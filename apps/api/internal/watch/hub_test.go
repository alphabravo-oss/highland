package watch

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeRec is a concurrency-safe ResponseWriter+Flusher so the test can read the
// streamed body while the handler goroutine writes it (httptest.ResponseRecorder
// is not safe for that).
type safeRec struct {
	mu  sync.Mutex
	buf bytes.Buffer
	hdr http.Header
}

func newSafeRec() *safeRec             { return &safeRec{hdr: http.Header{}} }
func (s *safeRec) Header() http.Header { return s.hdr }
func (s *safeRec) WriteHeader(int)     {}
func (s *safeRec) Flush()              {}
func (s *safeRec) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}
func (s *safeRec) body() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func waitForClients(t *testing.T, h *Hub, n int) {
	t.Helper()
	waitUntil(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.clients) >= n
	})
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func newTestHub() *Hub {
	return &Hub{
		clients: map[*sseClient]struct{}{},
		pending: map[string]struct{}{},
		ns:      "longhorn-system",
	}
}

func TestCoalescesPendingIntoOneFrame(t *testing.T) {
	h := newTestHub()
	c, _ := h.subscribe()

	// Many marks within a window collapse to a single frame with the key union.
	h.mark([]string{"volumes", "dashboard"}, "pvc-a")
	h.mark([]string{"volumes", "dashboard"}, "pvc-b")
	h.mark([]string{"nodes", "dashboard", "nodetags", "disktags"}, "node-1")
	h.flushOnce()

	select {
	case fr := <-c.ch:
		got := append([]string(nil), fr.keys...)
		sort.Strings(got)
		want := []string{"dashboard", "disktags", "nodes", "nodetags", "volumes"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("coalesced keys = %v, want %v", got, want)
		}
	default:
		t.Fatal("expected one coalesced frame")
	}
	// Nothing left pending → no second frame.
	h.flushOnce()
	select {
	case <-c.ch:
		t.Fatal("expected no frame when nothing pending")
	default:
	}
}

func TestBackpressureDropsToResync(t *testing.T) {
	h := newTestHub()
	c, _ := h.subscribe() // buffer = clientBuffer (16)

	// Fill the buffer exactly, then one more send overflows: the whole queue is
	// dropped and replaced by a single __all__ resync frame.
	for i := 0; i < clientBuffer+1; i++ {
		h.send(c, frame{keys: []string{"volumes"}})
	}
	var got []frame
	for {
		select {
		case f := <-c.ch:
			got = append(got, f)
			continue
		default:
		}
		break
	}
	if len(got) != 1 || len(got[0].keys) != 1 || got[0].keys[0] != "__all__" {
		t.Fatalf("expected a single __all__ resync frame after overflow, got %v", got)
	}
}

func TestUnsubscribeIsIdempotentAndSignalsDone(t *testing.T) {
	h := newTestHub()
	c, _ := h.subscribe()
	h.unsubscribe(c)
	h.unsubscribe(c) // must not panic (double stop guarded by sync.Once)
	select {
	case <-c.done:
	default:
		t.Fatal("done should be signaled after unsubscribe")
	}
	// send() to a stopped client must not panic and must not enqueue.
	h.send(c, frame{keys: []string{"volumes"}})
}

func TestSendToStoppedClientAfterSnapshotDoesNotPanic(t *testing.T) {
	// Simulates the blocker race: broker holds a client pointer, client stops,
	// broker sends. Must be a no-op, never a send-on-closed panic.
	h := newTestHub()
	c, _ := h.subscribe()
	h.mark([]string{"volumes"}, "x")
	h.unsubscribe(c)                            // stops c (does NOT close c.ch)
	h.flushOnce()                               // broadcasts to the (now empty) client set — safe
	h.send(c, frame{keys: []string{"volumes"}}) // direct send to stopped client — safe
}

func TestSubscribeAfterStopFails(t *testing.T) {
	h := newTestHub()
	h.Stop()
	if _, ok := h.subscribe(); ok {
		t.Fatal("subscribe after Stop should fail")
	}
}

func TestServeSSENilHub503(t *testing.T) {
	var h *Hub
	rec := httptest.NewRecorder()
	h.ServeSSE(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil hub should 503, got %d", rec.Code)
	}
}

func TestServeSSEWritesReadyThenChange(t *testing.T) {
	h := newTestHub()
	rec := newSafeRec()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	ctx, cancel := contextWithCancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.ServeSSE(rec, req)
		close(done)
	}()

	// Wait for the client to register, push a change, then let the handler flush.
	waitForClients(t, h, 1)
	h.mark([]string{"volumes"}, "pvc-x")
	h.flushOnce()
	// Give the handler a moment to write, then stop it.
	waitUntil(t, func() bool { return strings.Contains(rec.body(), "event: change") })
	cancel()
	<-done

	body := rec.body()
	if !strings.HasPrefix(body, "retry: 5000\nevent: ready\ndata: {}\n\n") {
		t.Fatalf("stream should open with retry+ready, got:\n%q", body)
	}
	if !strings.Contains(body, `event: change`) || !strings.Contains(body, `"keys":["volumes"]`) {
		t.Fatalf("expected a change frame with the volumes key, got:\n%q", body)
	}
}

func TestWatchedEntriesMappingIsSane(t *testing.T) {
	for _, e := range watchedEntries() {
		if e.gvr.Resource == "" || len(e.keys) == 0 {
			t.Fatalf("entry %v has empty resource or keys", e.gvr)
		}
		if e.gvr.Resource != "events" && e.gvr.Group != "longhorn.io" {
			t.Fatalf("unexpected group for %s: %q", e.gvr.Resource, e.gvr.Group)
		}
	}
}

func TestClientCountTracksSubscriptions(t *testing.T) {
	var nilHub *Hub
	if nilHub.ClientCount() != 0 {
		t.Fatal("nil hub must report 0 clients")
	}

	h := newTestHub()
	if h.ClientCount() != 0 {
		t.Fatalf("fresh hub should have 0 clients, got %d", h.ClientCount())
	}
	c1, _ := h.subscribe()
	c2, _ := h.subscribe()
	if h.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", h.ClientCount())
	}
	h.unsubscribe(c1)
	if h.ClientCount() != 1 {
		t.Fatalf("expected 1 client after unsubscribe, got %d", h.ClientCount())
	}
	h.unsubscribe(c2)
	if h.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", h.ClientCount())
	}
}
