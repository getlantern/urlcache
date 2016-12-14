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
	lastModified := time.Now()
	lastRead := ""
	numUpdates := 0

	s := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		mx.RLock()
		lm := lastModified
		mx.RUnlock()
		resp.Header().Set(lastModifiedHeader, lm.Format(http.TimeFormat))
		resp.Write([]byte(lm.String()))
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

	for i := 0; i < 3; i++ {
		time.Sleep(150 * time.Millisecond)
		mx.Lock()
		assert.Equal(t, lastModified.String(), lastRead)
		lastModified = lastModified.Add(1 * time.Hour)
		numUpdates++
		mx.Unlock()
	}

	mx.RLock()
	assert.Equal(t, 3, numUpdates)
	mx.RUnlock()
}
