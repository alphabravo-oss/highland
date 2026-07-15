package longhorn

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// Observer records proxy metrics (implemented by observability.Metrics; kept as
// a local interface so this package doesn't depend on it).
type Observer interface {
	ObserveManagerRequest(method string, status int, d time.Duration)
	IncManagerError(reason string)
}

// Proxy is an authenticated reverse proxy to the Longhorn manager /v1 API.
// Browser clients never talk to the manager directly; all traffic goes through Highland.
type Proxy struct {
	managerBase *url.URL
	proxy       *httputil.ReverseProxy
	obs         Observer
	// PublicAPIPrefix is the path prefix exposed by Highland (e.g. /api/v1/lh).
	PublicAPIPrefix string
	// ManagerAPIPrefix is the path on the manager (e.g. /v1).
	ManagerAPIPrefix string
}

// SetMetrics attaches an observer (nil disables proxy instrumentation).
func (p *Proxy) SetMetrics(o Observer) { p.obs = o }

// NewProxy creates a reverse proxy that rewrites /api/v1/lh/* → manager /v1/*.
func NewProxy(managerURL string) (*Proxy, error) {
	base, err := url.Parse(managerURL)
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		managerBase:      base,
		PublicAPIPrefix:  "/api/v1/lh",
		ManagerAPIPrefix: "/v1",
	}
	p.proxy = &httputil.ReverseProxy{
		Director:       p.director,
		ModifyResponse: p.modifyResponse,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if p.obs != nil {
				p.obs.IncManagerError("upstream_unavailable")
			}
			http.Error(w, "longhorn manager unavailable: "+err.Error(), http.StatusBadGateway)
		},
	}
	return p, nil
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.obs == nil {
		p.proxy.ServeHTTP(w, r)
		return
	}
	ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
	start := time.Now()
	p.proxy.ServeHTTP(ww, r)
	p.obs.ObserveManagerRequest(r.Method, ww.Status(), time.Since(start))
}

// RewritePath maps a Highland public path to the manager path.
// e.g. /api/v1/lh/volumes → /v1/volumes
func (p *Proxy) RewritePath(publicPath string) string {
	path := publicPath
	if strings.HasPrefix(path, p.PublicAPIPrefix) {
		path = p.ManagerAPIPrefix + strings.TrimPrefix(path, p.PublicAPIPrefix)
	}
	if path == "" {
		path = p.ManagerAPIPrefix
	}
	return path
}

// ManagerURL returns the configured manager base URL string.
func (p *Proxy) ManagerURL() string {
	return p.managerBase.String()
}

func (p *Proxy) director(req *http.Request) {
	targetPath := p.RewritePath(req.URL.Path)
	req.URL.Scheme = p.managerBase.Scheme
	req.URL.Host = p.managerBase.Host
	req.URL.Path = targetPath
	req.Host = p.managerBase.Host
	// Strip hop-by-hop / client identity; manager is cluster-internal.
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	// Force identity encoding so modifyResponse gets plain JSON it can rewrite.
	// If the client's Accept-Encoding leaks through, the manager may return gzip,
	// json.Unmarshal fails, and RewriteLinks silently returns links unrewritten.
	req.Header.Del("Accept-Encoding")
	// Force JSON. The Longhorn manager (Rancher API framework) content-negotiates
	// on Accept: a browser sends "text/html,..." and would get the HTML API-browser
	// page instead of JSON, which the SPA then parses to an empty collection.
	req.Header.Set("Accept", "application/json")
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "json") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	rewritten := RewriteLinks(body, p.managerBase.String(), p.PublicAPIPrefix, p.ManagerAPIPrefix)
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", itoa(len(rewritten)))
	return nil
}

// RewriteLinks rewrites manager absolute/relative action and link URLs so they
// point at the Highland proxy prefix instead of the manager.
func RewriteLinks(body []byte, managerBase, publicPrefix, managerPrefix string) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	rewriteValue(v, managerBase, publicPrefix, managerPrefix)
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}

func rewriteValue(v any, managerBase, publicPrefix, managerPrefix string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok && isLinkKey(k) {
				t[k] = rewriteURL(s, managerBase, publicPrefix, managerPrefix)
				continue
			}
			rewriteValue(child, managerBase, publicPrefix, managerPrefix)
		}
	case []any:
		for _, child := range t {
			rewriteValue(child, managerBase, publicPrefix, managerPrefix)
		}
	}
}

func isLinkKey(k string) bool {
	// Rancher-style collections put URLs in links.* and actions.* values.
	// We rewrite any string value under those containers when walking the tree;
	// keys themselves are "self", "attach", etc. — parents are "links"/"actions".
	// Here we only rewrite when the value looks like a manager URL/path.
	return true
}

func rewriteURL(s, managerBase, publicPrefix, managerPrefix string) string {
	if s == "" {
		return s
	}
	// Absolute manager URL
	if strings.HasPrefix(s, managerBase) {
		rest := strings.TrimPrefix(s, managerBase)
		if strings.HasPrefix(rest, managerPrefix) {
			return publicPrefix + strings.TrimPrefix(rest, managerPrefix)
		}
		return publicPrefix + rest
	}
	// Relative /v1/... path
	if strings.HasPrefix(s, managerPrefix+"/") || s == managerPrefix {
		return publicPrefix + strings.TrimPrefix(s, managerPrefix)
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
