// Package urlcache provides a facility for keeping data from a url cached on
// disk and periodically refreshing it.
package urlcache

import (
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
		err = c.onUpdate(file)
		file.Close()
		if err == nil {
			fileInfo, err := file.Stat()
			if err == nil {
				log.Debugf("Successfully initialized from %v", c.cacheFile)
				currentDate = fileInfo.ModTime()
				time.Sleep(c.checkInterval)
			}
		}
	}

	return currentDate
}

func (c *urlcache) keepCurrent(currentDate time.Time) {
	for {
		headResp, err := http.Head(c.url)
		if err != nil {
			log.Errorf("Unable to request modified of %v: %v", c.url, err)
		}
		lm, err := lastModified(headResp)
		if err != nil {
			log.Errorf("Unable to parse modified date for %v: %v", c.url, err)
		}
		if lm.After(currentDate) {
			log.Debug("Updating from web")
			err = c.updateFromWeb()
			if err != nil {
				log.Errorf("Unable to update from web: %v", err)
			} else {
				currentDate = lm
			}
		}
		time.Sleep(c.checkInterval)
	}
}

func (c *urlcache) updateFromWeb() error {
	resp, err := http.Get(c.url)
	if err != nil {
		return fmt.Errorf("Unable to update from web: %v", err)
	}
	defer resp.Body.Close()
	tmpFile, err := ioutil.TempFile("", "urlcache")
	if err != nil {
		return fmt.Errorf("Unable to create temp file: %v", err)
	}
	defer tmpFile.Close()
	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return fmt.Errorf("Unable to copy contents from web to temp file: %v", err)
	}
	err = tmpFile.Close()
	if err != nil {
		return fmt.Errorf("Unable to close temp file: %v", err)
	}
	tmpFile, err = os.Open(tmpFile.Name())
	if err != nil {
		return fmt.Errorf("Unable to reopen tmpFile for reading: %v", err)
	}
	defer tmpFile.Close()
	err = c.onUpdate(tmpFile)
	if err != nil {
		return fmt.Errorf("Unable to call onUpdate: %v", err)
	}
	err = os.Remove(c.cacheFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Unable to remove old cache file: %v", err)
	}
	err = os.Rename(tmpFile.Name(), c.cacheFile)
	if err != nil {
		return fmt.Errorf("Unable to move tmpFile to cacheFile: %v", err)
	}
	return nil
}

// lastModified parses the Last-Modified header from a response
func lastModified(resp *http.Response) (time.Time, error) {
	lastModified := resp.Header.Get(lastModifiedHeader)
	return http.ParseTime(lastModified)
}
