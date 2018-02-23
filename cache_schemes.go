package urlcache

import (
	"net/http"
)

const (
	lastModifiedHeader    = "Last-Modified"
	ifModifiedSinceHeader = "If-Modified-Since"

	etagHeader        = "ETag"
	ifNoneMatchHeader = "If-None-Match"
)

type cacheScheme interface {
	prepareRequest(req *http.Request)

	onResponse(resp *http.Response)
}

type lastModifiedScheme struct {
	lastModifiedDate string
}

func (s *lastModifiedScheme) prepareRequest(req *http.Request) {
	req.Header.Set(ifModifiedSinceHeader, s.lastModifiedDate)
}

func (s *lastModifiedScheme) onResponse(resp *http.Response) {
	s.lastModifiedDate = resp.Header.Get(lastModifiedHeader)
}

type etagScheme struct {
	etag string
}

func (s *etagScheme) prepareRequest(req *http.Request) {
	req.Header.Set(ifNoneMatchHeader, s.etag)
}

func (s *etagScheme) onResponse(resp *http.Response) {
	s.etag = resp.Header.Get(etagHeader)
}

type noopScheme struct {
}

func (s *noopScheme) prepareRequest(req *http.Request) {
}

func (s *noopScheme) onResponse(resp *http.Response) {
}
