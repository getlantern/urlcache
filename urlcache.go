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

	"github.com/getlantern/golog"
)

const (
	lastModifiedHeader = "Last-Modified"
)

var (
	log = golog.LoggerFor("urlcache")

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
	}
	go c.keepCurrent(c.readInitial())

	return nil
}

type urlcache struct {
	url           string
	cacheFile     string
	checkInterval time.Duration
	onUpdate      func(io.Reader) error
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
				log.Debugf("Successfully initialized from %v", c.cacheFile)
				currentDate = fileInfo.ModTime()
			}
		}
	}

	return currentDate
}

func (c *urlcache) keepCurrent(currentDate time.Time) {
	for {
		currentDate = c.checkUpdates(currentDate)
		time.Sleep(c.checkInterval)
	}
}

func (c *urlcache) checkUpdates(prevDate time.Time) (newDate time.Time) {
	newDate = prevDate
	headResp, err := http.Head(c.url)
	if err != nil {
		log.Errorf("Unable to request modified of %v: %v", c.url, err)
		return
	}
	lm, err := lastModified(headResp)
	if err != nil {
		log.Errorf("Unable to parse modified date for %v: %v", c.url, err)
		return
	}
	if lm.After(prevDate) {
		log.Debug("Updating from web")
		err = c.updateFromWeb()
		if err != nil {
			log.Errorf("Unable to update from web: %v", err)
			return
		}
		newDate = lm
	}
	return
}

func (c *urlcache) updateFromWeb() error {
	resp, err := http.Get(c.url)
	if err != nil {
		return fmt.Errorf("Unable to update from web: %v", err)
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
		log.Debugf("Unable to save to temp file, will write directly to destination: %v", esave)
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
	f, err := ioutil.TempFile("", "urlcache")
	if err != nil {
		return "", fmt.Errorf("Unable to create temp file: %v", err)
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
	lastModified := resp.Header.Get(lastModifiedHeader)
	return http.ParseTime(lastModified)
}
