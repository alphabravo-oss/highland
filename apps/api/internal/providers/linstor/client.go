package linstor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 4 << 20

var endpointByKind = map[string]string{
	"nodes": "/v1/nodes", "storage-pools": "/v1/view/storage-pools",
	"resource-groups": "/v1/resource-groups", "resources": "/v1/view/resources",
	"resource-definitions": "/v1/resource-definitions", "snapshots": "/v1/view/snapshots",
	"remotes": "/v1/remotes", "schedules": "/v1/schedules", "error-reports": "/v1/error-reports",
}

type ClientConfig struct {
	URL, Token, CAFile string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

// Client is a bounded, read-only LINSTOR REST client. Callers select a fixed
// resource kind; request paths and HTTP methods cannot be supplied by users.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
	mu    sync.Mutex
	cache map[string]cachedResponse
}

type cachedResponse struct {
	data []byte
	at   time.Time
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil
	}
	base, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return nil, fmt.Errorf("invalid LINSTOR controller URL")
	}
	if base.Scheme != "https" && base.Scheme != "http" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid LINSTOR controller URL")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 -- explicit lab-only setting
	if cfg.CAFile != "" {
		pem, readErr := os.ReadFile(cfg.CAFile)
		if readErr != nil {
			return nil, fmt.Errorf("read LINSTOR CA: %w", readErr)
		}
		pool, cloneErr := x509.SystemCertPool()
		if cloneErr != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("LINSTOR CA contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Client{base: base, token: cfg.Token, cache: map[string]cachedResponse{}, http: &http.Client{Timeout: cfg.Timeout, Transport: &http.Transport{TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Available() bool { return c != nil }

func (c *Client) Version(ctx context.Context) (map[string]any, error) {
	var value map[string]any
	if err := c.get(ctx, "/v1/controller/version", &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c *Client) List(ctx context.Context, kind string) ([]map[string]any, error) {
	path, ok := endpointByKind[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported LINSTOR resource kind %q", kind)
	}
	var raw json.RawMessage
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, err
	}
	var values []map[string]any
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("decode LINSTOR list: %w", err)
		}
	} else {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return nil, fmt.Errorf("decode LINSTOR list: %w", err)
		}
		keys := make([]string, 0, len(wrapper))
		for key := range wrapper {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			var group []map[string]any
			if err := json.Unmarshal(wrapper[key], &group); err == nil {
				for _, item := range group {
					if kind == "remotes" {
						item["remote_type"] = strings.TrimSuffix(key, "_remotes")
					}
					values = append(values, item)
				}
			}
		}
	}
	if len(values) > 500 {
		values = values[:500]
	}
	return values, nil
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	if c == nil {
		return fmt.Errorf("LINSTOR controller endpoint is not configured")
	}
	c.mu.Lock()
	cached, ok := c.cache[path]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < 5*time.Second {
		return json.Unmarshal(cached.data, target)
	}
	reference, _ := url.Parse(path)
	endpoint := c.base.ResolveReference(reference)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("LINSTOR request failed: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read LINSTOR response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("LINSTOR response exceeded %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("LINSTOR returned HTTP %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "json") {
		return fmt.Errorf("LINSTOR returned a non-JSON response")
	}
	c.mu.Lock()
	c.cache[path] = cachedResponse{data: append([]byte(nil), data...), at: time.Now()}
	c.mu.Unlock()
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode LINSTOR response: %w", err)
	}
	return nil
}
