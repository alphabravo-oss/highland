package metrics

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is a single numeric series point.
type Sample struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// Series is a named metric series.
type Series struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Points []Sample          `json:"points"`
}

// Scraper polls manager /metrics and keeps a short ring buffer per series key.
type Scraper struct {
	baseURL    string
	client     *http.Client
	interval   time.Duration
	window     int           // points per series
	staleAfter time.Duration // drop a series after it goes this long without a sample

	mu      sync.RWMutex
	series  map[string]*Series
	lastErr string
	stop    chan struct{}
}

// NewScraper creates a metrics scraper. baseURL is manager root (no trailing slash).
func NewScraper(baseURL string, interval time.Duration, window int) *Scraper {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if window <= 0 {
		window = 60
	}
	// Prune a series after ~3 missed scrapes (min 30s) so deleted volumes/nodes
	// don't linger as ghost series after the manager stops reporting them.
	staleAfter := 3 * interval
	if staleAfter < 30*time.Second {
		staleAfter = 30 * time.Second
	}
	return &Scraper{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: 8 * time.Second},
		interval:   interval,
		window:     window,
		staleAfter: staleAfter,
		series:     map[string]*Series{},
		stop:       make(chan struct{}),
	}
}

// Start begins background polling.
func (s *Scraper) Start() {
	go func() {
		t := time.NewTicker(s.interval)
		defer t.Stop()
		s.poll()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.poll()
			}
		}
	}()
}

// Stop ends polling.
func (s *Scraper) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

func (s *Scraper) poll() {
	resp, err := s.client.Get(s.baseURL + "/metrics")
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		s.mu.Lock()
		s.lastErr = fmt.Sprintf("metrics status %d", resp.StatusCode)
		s.mu.Unlock()
		return
	}
	parsed, err := ParseProm(resp.Body)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = ""
	for _, p := range parsed {
		key := p.Name
		if vol := p.Labels["volume"]; vol != "" {
			key = p.Name + "|volume=" + vol
		} else if node := p.Labels["node"]; node != "" {
			key = p.Name + "|node=" + node
		}
		ser, ok := s.series[key]
		if !ok {
			ser = &Series{Name: p.Name, Labels: p.Labels, Points: nil}
			s.series[key] = ser
		}
		ser.Points = append(ser.Points, Sample{T: now, V: p.Value})
		if len(ser.Points) > s.window {
			ser.Points = ser.Points[len(ser.Points)-s.window:]
		}
	}
	// Drop series the manager has stopped reporting (e.g. deleted volumes).
	staleBefore := now.Add(-s.staleAfter)
	for key, ser := range s.series {
		if len(ser.Points) == 0 || ser.Points[len(ser.Points)-1].T.Before(staleBefore) {
			delete(s.series, key)
		}
	}
}

// Snapshot returns copies of series, optionally filtered by volume label.
func (s *Scraper) Snapshot(volume string) []Series {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Series, 0, len(s.series))
	for _, ser := range s.series {
		if volume != "" && ser.Labels["volume"] != volume {
			continue
		}
		cp := Series{Name: ser.Name, Labels: ser.Labels, Points: append([]Sample(nil), ser.Points...)}
		out = append(out, cp)
	}
	return out
}

// LastError returns the last scrape error.
func (s *Scraper) LastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

type promPoint struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// ParseProm parses a subset of Prometheus text exposition format.
func ParseProm(r io.Reader) ([]promPoint, error) {
	sc := bufio.NewScanner(r)
	var out []promPoint
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// metric{labels} value  OR metric value
		name := line
		labels := map[string]string{}
		rest := ""
		if i := strings.Index(line, "{"); i >= 0 {
			name = line[:i]
			end := strings.Index(line, "}")
			if end < 0 {
				continue
			}
			labelPart := line[i+1 : end]
			rest = strings.TrimSpace(line[end+1:])
			for _, pair := range strings.Split(labelPart, ",") {
				pair = strings.TrimSpace(pair)
				if pair == "" {
					continue
				}
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) != 2 {
					continue
				}
				labels[kv[0]] = strings.Trim(kv[1], `"`)
			}
		} else {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			name = parts[0]
			rest = parts[1]
		}
		fields := strings.Fields(rest)
		if len(fields) < 1 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		out = append(out, promPoint{Name: name, Labels: labels, Value: v})
	}
	return out, sc.Err()
}
