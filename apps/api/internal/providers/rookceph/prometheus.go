package rookceph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/highland-io/highland/apps/api/internal/storage"
)

var cephQueries = map[string]string{
	"readThroughput":      `sum(rate(ceph_pool_rd_bytes[5m]))`,
	"writeThroughput":     `sum(rate(ceph_pool_wr_bytes[5m]))`,
	"readIOPS":            `sum(rate(ceph_pool_rd[5m]))`,
	"writeIOPS":           `sum(rate(ceph_pool_wr[5m]))`,
	"readLatencySeconds":  `sum(rate(ceph_osd_op_r_latency_sum[5m])) / clamp_min(sum(rate(ceph_osd_op_r_latency_count[5m])), 1)`,
	"writeLatencySeconds": `sum(rate(ceph_osd_op_w_latency_sum[5m])) / clamp_min(sum(rate(ceph_osd_op_w_latency_count[5m])), 1)`,
	"usedBytes":           `sum(ceph_cluster_total_used_bytes)`,
	"totalBytes":          `sum(ceph_cluster_total_bytes)`,
	"recoveringBytes":     `sum(rate(ceph_osd_recovery_bytes[5m]))`,
	"healthStatus":        `max(ceph_health_status)`,
	"degradedObjects":     `sum(ceph_pg_degraded)`,
	"misplacedObjects":    `sum(ceph_pg_misplaced)`,
}

type PrometheusSnapshot struct {
	Values               map[string]string  `json:"values"`
	ObservedAt           time.Time          `json:"observedAt"`
	LastSuccess          time.Time          `json:"lastSuccess"`
	Stale                bool               `json:"stale,omitempty"`
	Unavailable          []string           `json:"unavailable,omitempty"`
	Failures             int                `json:"failures,omitempty"`
	QueryDurationSeconds map[string]float64 `json:"queryDurationSeconds,omitempty"`
}

type PrometheusClient struct {
	base   *url.URL
	client *http.Client
	mu     sync.RWMutex
	last   PrometheusSnapshot
}

func NewPrometheusClient(rawURL string, client *http.Client) (*PrometheusClient, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, nil
	}
	base, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("invalid Prometheus URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	} else if client.CheckRedirect == nil {
		copyClient := *client
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copyClient
	}
	return &PrometheusClient{base: base, client: client}, nil
}

func (c *PrometheusClient) Snapshot(ctx context.Context) (PrometheusSnapshot, error) {
	if c == nil {
		return PrometheusSnapshot{}, fmt.Errorf("Prometheus is not configured")
	}
	values := make(map[string]string, len(cephQueries))
	durations := make(map[string]float64, len(cephQueries))
	unavailable := []string{}
	var lastErr error
	for name, query := range cephQueries {
		started := time.Now()
		value, err := c.query(ctx, query)
		durations[name] = time.Since(started).Seconds()
		if err != nil {
			lastErr = err
			unavailable = append(unavailable, name)
			continue
		}
		values[name] = value
	}
	if len(values) == 0 {
		c.mu.RLock()
		last := c.last
		c.mu.RUnlock()
		if !last.ObservedAt.IsZero() {
			last.Stale = true
			last.Failures = len(unavailable)
			last.Unavailable = unavailable
			last.QueryDurationSeconds = durations
			return last, nil
		}
		return PrometheusSnapshot{}, lastErr
	}
	sort.Strings(unavailable)
	now := time.Now().UTC()
	snapshot := PrometheusSnapshot{Values: values, ObservedAt: now, LastSuccess: now, Unavailable: unavailable, Failures: len(unavailable), QueryDurationSeconds: durations}
	c.mu.Lock()
	c.last = snapshot
	c.mu.Unlock()
	return snapshot, nil
}

func (c *PrometheusClient) query(ctx context.Context, expression string) (string, error) {
	target := c.base.ResolveReference(&url.URL{Path: "/api/v1/query"})
	query := target.Query()
	query.Set("query", expression)
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Prometheus query failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Prometheus returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode Prometheus response: %w", err)
	}
	if envelope.Status != "success" || len(envelope.Data.Result) == 0 || len(envelope.Data.Result[0].Value) != 2 {
		return "", fmt.Errorf("Prometheus query returned no scalar result")
	}
	var raw string
	if err := json.Unmarshal(envelope.Data.Result[0].Value[1], &raw); err != nil {
		return "", fmt.Errorf("Prometheus value is invalid")
	}
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		return "", fmt.Errorf("Prometheus value is not numeric")
	}
	return raw, nil
}

func (c *PrometheusClient) Range(ctx context.Context, key string, start, end time.Time, step time.Duration) ([]storage.CapacityHistorySample, error) {
	if c == nil {
		return nil, fmt.Errorf("Prometheus is not configured")
	}
	expression, ok := cephQueries[key]
	if !ok || (key != "usedBytes" && key != "totalBytes") {
		return nil, fmt.Errorf("Prometheus history key is not allowlisted")
	}
	if start.IsZero() || end.IsZero() || !end.After(start) || end.Sub(start) > 90*24*time.Hour {
		return nil, fmt.Errorf("Prometheus history range must be positive and no greater than 90 days")
	}
	if step < 5*time.Minute || step > time.Hour {
		return nil, fmt.Errorf("Prometheus history step must be between 5 minutes and 1 hour")
	}
	target := c.base.ResolveReference(&url.URL{Path: "/api/v1/query_range"})
	query := target.Query()
	query.Set("query", expression)
	query.Set("start", strconv.FormatInt(start.Unix(), 10))
	query.Set("end", strconv.FormatInt(end.Unix(), 10))
	query.Set("step", strconv.FormatInt(int64(step/time.Second), 10))
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Prometheus range query failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Prometheus returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode Prometheus range response: %w", err)
	}
	if envelope.Status != "success" || len(envelope.Data.Result) != 1 {
		return nil, fmt.Errorf("Prometheus range query returned no single aggregate series")
	}
	if len(envelope.Data.Result[0].Values) > 10_000 {
		return nil, fmt.Errorf("Prometheus range query exceeded 10000 samples")
	}
	result := make([]storage.CapacityHistorySample, 0, len(envelope.Data.Result[0].Values))
	for _, pair := range envelope.Data.Result[0].Values {
		if len(pair) != 2 {
			return nil, fmt.Errorf("Prometheus range sample is malformed")
		}
		var timestamp float64
		var raw string
		if json.Unmarshal(pair[0], &timestamp) != nil || json.Unmarshal(pair[1], &raw) != nil {
			return nil, fmt.Errorf("Prometheus range sample is invalid")
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > float64(^uint64(0)) {
			return nil, fmt.Errorf("Prometheus range value is outside uint64")
		}
		result = append(result, storage.CapacityHistorySample{
			Timestamp: time.Unix(0, int64(timestamp*float64(time.Second))).UTC(),
			Bytes:     uint64(value),
		})
	}
	return result, nil
}
