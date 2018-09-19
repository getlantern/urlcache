// Package urlcache provides a facility for keeping data from a url cached on
// disk and periodically refreshing it.
package urlcache

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/zaplog"
)

var (
	log = zaplog.LoggerFor("urlcache")

	defaultCheckInterval = 1 * time.Minute
)

// Open opens the url and starts caching in cacheFile. Whenever initial or
// updated data is available, onupdate is called. If data already existed in
// cacheFile, onUpdate will be immediately called with that.
func Open(url string, cacheFile string, checkInterval time.Duration, onUpdate func(io.Reader) error) error {
	if checkInterval <= 0 {
		checkInterval = defaultCheckInterval
	}
	dir, _ := filepath.Split(cacheFile)
	if dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("Unable to create cache dir %v: %v", dir, err)
		}
	}

	c := &urlcache{
		url:           url,
		cacheFile:     cacheFile,
		checkInterval: checkInterval,
		onUpdate:      onUpdate,
		client:        &http.Client{},
	}
	go c.keepCurrent(c.readInitial())

	return nil
}

type urlcache struct {
	url           string
	cacheFile     string
	checkInterval time.Duration
	onUpdate      func(io.Reader) error
	client        *http.Client
}

func (c *urlcache) readInitial() time.Time {
	var currentDate time.Time
	file, err := os.Open(c.cacheFile)
	if err == nil {
		err = c.onUpdate(bufio.NewReader(file))
		file.Close()
		if err == nil {
			fileInfo, err := file.Stat()
			if err == nil {
				log.Infof("Successfully initialized from %v", c.cacheFile)
				currentDate = fileInfo.ModTime()
			}
		}
	}

	return currentDate
}

func (c *urlcache) keepCurrent(initialDate time.Time) {
	var scheme cacheScheme
	for {
		scheme = c.checkUpdates(initialDate, scheme)
		time.Sleep(c.checkInterval)
	}
}

func (c *urlcache) checkUpdates(initialDate time.Time, scheme cacheScheme) cacheScheme {
	if scheme == nil {
		log.Infof("Cache scheme unknown, issue HEAD request to determine scheme")
		headResp, err := http.Head(c.url)
		if err != nil {
			log.Errorf("Unable to request modified of %v: %v", c.url, err)
			return scheme
		}

		if headResp.Header.Get(lastModifiedHeader) != "" {
			log.Infof("Will use %v to determine when file changes", lastModifiedHeader)
			scheme = &lastModifiedScheme{initialDate.Format(http.TimeFormat)}
		} else if headResp.Header.Get(etagHeader) != "" {
			log.Infof("Will use %v to determine when file changes", etagHeader)
			scheme = &etagScheme{}
		} else {
			log.Info("Will always assume file changed")
			scheme = &noopScheme{}
		}
	}

	err := c.updateFromWeb(scheme)
	if err != nil {
		log.Errorf("Unable to update from web: %v", err)
	}
	return scheme
}

func (c *urlcache) updateFromWeb(scheme cacheScheme) error {
	req, _ := http.NewRequest(http.MethodGet, c.url, nil)
	scheme.prepareRequest(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Unable to update from web: %v", err)
	}
	scheme.onResponse(resp)

	if resp.StatusCode == http.StatusNotModified {
		return nil
	}

	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Unable to read data from web: %v", err)
	}
	err = c.onUpdate(bytes.NewReader(data))
	if err != nil {
		return err
	}

	tmpName, esave := c.saveToTmpFile(data)
	if esave != nil {
		log.Infof("Unable to save to temp file, will write directly to destination: %v", esave)
		f, openErr := os.OpenFile(c.cacheFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if openErr != nil {
			return fmt.Errorf("Unable to open cache file: %v", openErr)
		}
		return c.saveToFile(f, data)
	}

	err = os.Remove(c.cacheFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Unable to remove old cache file: %v", err)
	}
	err = os.Rename(tmpName, c.cacheFile)
	if err != nil {
		return fmt.Errorf("Unable to move tmpFile to cacheFile: %v", err)
	}
	return nil
}

func (c *urlcache) saveToTmpFile(data []byte) (string, error) {
	tmpFileName := fmt.Sprintf("%v_temp", c.cacheFile)
	f, err := os.OpenFile(tmpFileName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("Unable to create temp file %v: %v", tmpFileName, err)
	}
	return f.Name(), c.saveToFile(f, data)
}

func (c *urlcache) saveToFile(f *os.File, data []byte) error {
	defer f.Close()
	_, err := f.Write(data)
	if err != nil {
		return fmt.Errorf("Unable to copy contents from web to temp file: %v", err)
	}
	return nil
}

// lastModified parses the Last-Modified header from a response
func lastModified(resp *http.Response) (time.Time, error) {
	return http.ParseTime(resp.Header.Get(lastModifiedHeader))
}

func etag(resp *http.Response) string {
	return resp.Header.Get(etagHeader)
}
