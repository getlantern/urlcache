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

func TestCacheByLastModified(t *testing.T) {
	doTestCache(t, lastModifiedHeader, ifModifiedSinceHeader, time.Now().Format(http.TimeFormat), func(old string) string {
		lm, _ := time.Parse(http.TimeFormat, old)
		return lm.Add(1 * time.Hour).Format(http.TimeFormat)
	})
}

func TestCacheByETag(t *testing.T) {
	doTestCache(t, etagHeader, ifNoneMatchHeader, "a", func(old string) string {
		return old + "a"
	})
}

func TestCacheByNone(t *testing.T) {
	doTestCache(t, "X-Junk", "", "b", func(old string) string {
		return old + "b"
	})
}

func TestOpenBadURL(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "urlcache_test")
	if !assert.NoError(t, err) {
		return
	}
	defer os.RemoveAll(tmpDir)

	// Just to make sure it doesn't panic when error happens fetching URL
	openErr := Open("http://not-exist", filepath.Join(tmpDir, "inter", "cachefile"), 50*time.Millisecond, func(r io.Reader) error {
		return nil
	})
	if !assert.NoError(t, openErr) {
		return
	}
	time.Sleep(150 * time.Millisecond)
}

func doTestCache(t *testing.T, header string, modifiedHeader string, initialVal string, advance func(old string) string) {
	tmpDir, err := ioutil.TempDir("", "urlcache_test")
	if !assert.NoError(t, err) {
		return
	}
	defer os.RemoveAll(tmpDir)

	var mx sync.RWMutex
	val := initialVal
	lastRead := ""
	numUpdates := 0

	s := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		mx.RLock()
		v := val
		mx.RUnlock()
		resp.Header().Set(header, v)
		if resp.Header().Get(modifiedHeader) < v {
			resp.Write([]byte(v))
		} else {
			resp.WriteHeader(http.StatusNotModified)
		}
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
		assert.Equal(t, val, lastRead)
		val = advance(val)
		numUpdates++
		mx.Unlock()
	}

	mx.Lock()
	assert.Equal(t, 3, numUpdates)
	mx.Unlock()
}
