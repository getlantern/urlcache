package urlcache

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCache(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "urlcache_test")
	if !assert.NoError(t, err) {
		return
	}
	defer os.RemoveAll(tmpDir)

	var mx sync.RWMutex
	lastModified := time.Now().Format(http.TimeFormat)
	etag := ""
	lastRead := ""
	numUpdates := 0

	s := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		mx.RLock()
		lm := lastModified
		et := etag
		mx.RUnlock()
		if lm != "" {
			resp.Header().Set(lastModifiedHeader, lm)
		}
		if et != "" {
			resp.Header().Set(etagHeader, et)
		}
		resp.Write([]byte(lm))
	}))
	defer s.Close()

	openErr := Open(s.URL, filepath.Join(tmpDir, "inter", "cachefile"), 50*time.Millisecond, func(r io.Reader) error {
		b, err := ioutil.ReadAll(r)
		if err != nil {
			return err
		}
		mx.Lock()
		lastRead = string(b)
		mx.Unlock()
		return nil
	})
	if !assert.NoError(t, openErr) {
		return
	}

	// Fetch based on Last-Modified
	for i := 0; i < 3; i++ {
		time.Sleep(150 * time.Millisecond)
		mx.Lock()
		assert.Equal(t, lastModified, lastRead)
		lm, _ := time.Parse(http.TimeFormat, lastModified)
		lastModified = lm.Add(1 * time.Hour).Format(http.TimeFormat)
		numUpdates++
		mx.Unlock()
	}

	mx.Lock()
	assert.Equal(t, 3, numUpdates)
	numUpdates = 0
	lastModified = ""
	etag = "a"
	mx.Unlock()

	// Fetch based on ETag
	for i := 0; i < 3; i++ {
		time.Sleep(150 * time.Millisecond)
		mx.Lock()
		assert.Equal(t, lastModified, lastRead)
		etag = etag + etag
		numUpdates++
		mx.Unlock()
	}

	mx.Lock()
	assert.Equal(t, 3, numUpdates)
	numUpdates = 0
	lastModified = ""
	etag = ""
	mx.Unlock()

	// Fetch based on absence of headers
	for i := 0; i < 3; i++ {
		time.Sleep(150 * time.Millisecond)
		mx.Lock()
		assert.Equal(t, lastModified, lastRead)
		numUpdates++
		mx.Unlock()
	}

	mx.RLock()
	assert.Equal(t, 3, numUpdates)
	mx.RUnlock()

	// Just to make sure it doesn't panic when error happens fetching URL
	openErr = Open("http://not-exist", filepath.Join(tmpDir, "inter", "cachefile"), 50*time.Millisecond, func(r io.Reader) error {
		return nil
	})
	if !assert.NoError(t, openErr) {
		return
	}
	time.Sleep(150 * time.Millisecond)
}
