package rookceph

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	dashboardMediaType   = "application/vnd.ceph.api.v1.0+json"
	dashboardMediaTypeV2 = "application/vnd.ceph.api.v2.0+json"
	maxDashboardBody     = 16 << 20
	maxDashboardStale    = 15 * time.Minute
)

var dashboardReadEndpoints = map[string]string{
	"/api/health/minimal": dashboardMediaType,
	"/api/osd":            dashboardMediaType,
	"/api/block/image":    dashboardMediaTypeV2,
	"/api/pool":           dashboardMediaType,
}

type DashboardConfig struct {
	URL                string
	Username           string
	Password           string
	CAFile             string
	InsecureSkipVerify bool
	Timeout            time.Duration
	HTTPClient         *http.Client
}

type dashboardCacheEntry struct {
	value      []byte
	observedAt time.Time
}

type DashboardResult struct {
	Data       json.RawMessage
	ObservedAt time.Time
	Stale      bool
}

type dashboardCircuitState struct {
	failures  int
	openUntil time.Time
}

type DashboardClient struct {
	baseURL  *url.URL
	username string
	password string
	client   *http.Client
	insecure bool

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
	circuits    map[string]dashboardCircuitState
	cache       map[string]dashboardCacheEntry
}

func (c *DashboardClient) endpoint(path string) *url.URL {
	target := *c.baseURL
	target.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	return &target
}

func NewDashboardClient(cfg DashboardConfig) (*DashboardClient, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil
	}
	base, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, fmt.Errorf("invalid Ceph Dashboard URL")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("Ceph Dashboard credentials are required")
	}
	client := cfg.HTTPClient
	if client == nil {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 -- explicit development gate, surfaced as provider warning.
		if cfg.CAFile != "" {
			pem, readErr := os.ReadFile(cfg.CAFile)
			if readErr != nil {
				return nil, fmt.Errorf("read Ceph Dashboard CA: %w", readErr)
			}
			pool, poolErr := x509.SystemCertPool()
			if poolErr != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("Ceph Dashboard CA contains no certificates")
			}
			tlsConfig.RootCAs = pool
		}
		transport := &http.Transport{TLSClientConfig: tlsConfig, MaxIdleConns: 20, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &DashboardClient{baseURL: base, username: cfg.Username, password: cfg.Password, client: client, insecure: cfg.InsecureSkipVerify, circuits: map[string]dashboardCircuitState{}, cache: map[string]dashboardCacheEntry{}}, nil
}

func (c *DashboardClient) Insecure() bool { return c != nil && c.insecure }

// Logout invalidates the current Dashboard session on a best-effort basis and
// always clears the in-memory JWT before returning. It is intended for clean
// Highland shutdown; normal token rotation continues to use the bounded auth
// refresh path in Get.
func (c *DashboardClient) Logout(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	token := c.token
	c.token, c.tokenExpiry = "", time.Time{}
	c.mu.Unlock()
	if token == "" {
		return nil
	}
	target := c.endpoint("/api/auth/logout")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", dashboardMediaType)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("Ceph Dashboard logout failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Ceph Dashboard logout returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *DashboardClient) Get(ctx context.Context, endpoint string) (DashboardResult, error) {
	if c == nil {
		return DashboardResult{}, errors.New("Ceph Dashboard is not configured")
	}
	if _, ok := dashboardReadEndpoints[endpoint]; !ok {
		return DashboardResult{}, errors.New("unsupported Ceph Dashboard endpoint")
	}
	c.mu.Lock()
	circuit := c.circuits[endpoint]
	if time.Now().Before(circuit.openUntil) {
		cached, ok := c.cache[endpoint]
		c.mu.Unlock()
		if ok && time.Since(cached.observedAt) <= maxDashboardStale {
			return DashboardResult{Data: append([]byte(nil), cached.value...), ObservedAt: cached.observedAt, Stale: true}, nil
		}
		return DashboardResult{}, fmt.Errorf("Ceph Dashboard circuit is open")
	}
	c.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, status, err := c.getOnce(ctx, endpoint, attempt > 0)
		if err == nil {
			c.recordSuccess(endpoint, result.Data, result.ObservedAt)
			return result, nil
		}
		lastErr = err
		if status == http.StatusUnauthorized && attempt == 0 {
			c.clearToken()
			continue
		}
		if status != http.StatusBadGateway && status != http.StatusServiceUnavailable && status != http.StatusGatewayTimeout {
			break
		}
		select {
		case <-ctx.Done():
			return DashboardResult{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	c.recordFailure(endpoint)
	c.mu.Lock()
	cached, ok := c.cache[endpoint]
	c.mu.Unlock()
	if ok && time.Since(cached.observedAt) <= maxDashboardStale {
		return DashboardResult{Data: append([]byte(nil), cached.value...), ObservedAt: cached.observedAt, Stale: true}, nil
	}
	return DashboardResult{}, lastErr
}

func (c *DashboardClient) getOnce(ctx context.Context, endpoint string, forceAuth bool) (DashboardResult, int, error) {
	token, err := c.authToken(ctx, forceAuth)
	if err != nil {
		return DashboardResult{}, 0, err
	}
	target := c.endpoint(endpoint)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return DashboardResult{}, 0, err
	}
	request.Header.Set("Accept", dashboardReadEndpoints[endpoint])
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.client.Do(request)
	if err != nil {
		return DashboardResult{}, 0, fmt.Errorf("Ceph Dashboard request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DashboardResult{}, response.StatusCode, fmt.Errorf("Ceph Dashboard returned HTTP %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" && !strings.Contains(contentType, "json") {
		return DashboardResult{}, response.StatusCode, fmt.Errorf("Ceph Dashboard returned unsupported content type")
	}
	data, err := readBounded(response.Body, maxDashboardBody)
	if err != nil {
		return DashboardResult{}, response.StatusCode, err
	}
	if !json.Valid(data) {
		return DashboardResult{}, response.StatusCode, errors.New("Ceph Dashboard returned malformed JSON")
	}
	return DashboardResult{Data: data, ObservedAt: time.Now().UTC()}, response.StatusCode, nil
}

func (c *DashboardClient) authToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	if !force && c.token != "" && time.Until(c.tokenExpiry) > time.Minute {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()
	payload, _ := json.Marshal(map[string]string{"username": c.username, "password": c.password})
	target := c.endpoint("/api/auth")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", dashboardMediaType)
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Ceph Dashboard authentication failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Ceph Dashboard authentication returned HTTP %d", response.StatusCode)
	}
	data, err := readBounded(response.Body, 1<<20)
	if err != nil {
		return "", err
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &auth); err != nil || auth.Token == "" {
		return "", errors.New("Ceph Dashboard authentication response is invalid")
	}
	expires := jwtExpiry(auth.Token)
	if expires.IsZero() {
		expires = time.Now().Add(15 * time.Minute)
	}
	c.mu.Lock()
	c.token, c.tokenExpiry = auth.Token, expires
	c.mu.Unlock()
	return auth.Token, nil
}

func (c *DashboardClient) clearToken() {
	c.mu.Lock()
	c.token, c.tokenExpiry = "", time.Time{}
	c.mu.Unlock()
}
func (c *DashboardClient) recordSuccess(endpoint string, value []byte, observedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.circuits, endpoint)
	c.cache[endpoint] = dashboardCacheEntry{value: append([]byte(nil), value...), observedAt: observedAt}
}
func (c *DashboardClient) recordFailure(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.circuits == nil {
		c.circuits = map[string]dashboardCircuitState{}
	}
	state := c.circuits[endpoint]
	state.failures++
	if state.failures >= 3 {
		state.openUntil = time.Now().Add(30 * time.Second)
	}
	c.circuits[endpoint] = state
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read Ceph Dashboard response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Ceph Dashboard response exceeded size limit")
	}
	return data, nil
}

func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Expires json.Number `json:"exp"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if decoder.Decode(&claims) != nil {
		return time.Time{}
	}
	seconds, err := claims.Expires.Int64()
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}
