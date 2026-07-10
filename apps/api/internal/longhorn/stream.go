package longhorn

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StreamProxy reverse-proxies large uploads/downloads (backing image) without buffering entire body.
// Mount under authenticated routes; rewrites /api/v1/lh/* → manager /v1/*.
type StreamProxy struct {
	managerBase *url.URL
	client      *http.Client
	publicPref  string
	managerPref string
}

// NewStreamProxy builds a streaming client for manager.
func NewStreamProxy(managerURL string) (*StreamProxy, error) {
	base, err := url.Parse(managerURL)
	if err != nil {
		return nil, err
	}
	return &StreamProxy{
		managerBase: base,
		publicPref:  "/api/v1/lh",
		managerPref: "/v1",
		client: &http.Client{
			Timeout: 0, // no overall timeout for large uploads; rely on server
			Transport: &http.Transport{
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}, nil
}

// ServeHTTP streams the request/response body bidirectionally.
func (s *StreamProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, s.publicPref) {
		path = s.managerPref + strings.TrimPrefix(path, s.publicPref)
	}
	target := *s.managerBase
	target.Path = path
	target.RawQuery = r.URL.RawQuery

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Copy headers except hop-by-hop / cookies
	for k, vv := range r.Header {
		lk := strings.ToLower(k)
		if lk == "cookie" || lk == "authorization" || lk == "host" {
			continue
		}
		for _, v := range vv {
			outReq.Header.Add(k, v)
		}
	}
	outReq.ContentLength = r.ContentLength
	outReq.Host = s.managerBase.Host

	resp, err := s.client.Do(outReq)
	if err != nil {
		http.Error(w, "manager stream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
