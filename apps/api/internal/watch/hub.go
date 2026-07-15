// Package watch turns Longhorn CRD change events into Server-Sent Events so the
// browser can refresh instantly instead of polling. A single dynamic shared
// informer set per BFF replica watches the CRDs; a coalescing broker fans out
// tiny "these query keys changed" frames to all connected SSE clients, which
// invalidate the matching TanStack Query caches. It AUGMENTS polling — if the
// stream drops or RBAC is missing, the UI keeps refreshing on its timers.
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const (
	lhGroup           = "longhorn.io"
	lhVersion         = "v1beta2"
	coalesceWindow    = 300 * time.Millisecond
	heartbeat         = 20 * time.Second
	clientBuffer      = 16
	maxStreamLifetime = 30 * time.Minute
)

func lhGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: lhGroup, Version: lhVersion, Resource: resource}
}

var coreEventsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}

type watchEntry struct {
	gvr  schema.GroupVersionResource
	keys []string
}

// watchedEntries maps each watched Longhorn CRD (+ core events) to the TanStack
// query keys it invalidates. Resource plurals are the exact longhorn.io GVR
// names (see `kubectl api-resources --api-group=longhorn.io`).
func watchedEntries() []watchEntry {
	return []watchEntry{
		// Volumes and everything that reflects volume state.
		{lhGVR("volumes"), []string{"volumes", "dashboard"}},
		{lhGVR("engines"), []string{"volumes", "dashboard"}},
		{lhGVR("replicas"), []string{"volumes", "dashboard"}},
		{lhGVR("volumeattachments"), []string{"volumes"}},
		// Nodes.
		{lhGVR("nodes"), []string{"nodes", "dashboard", "nodetags", "disktags"}},
		// Backups.
		{lhGVR("backupvolumes"), []string{"backupvolumes", "dashboard"}},
		{lhGVR("backups"), []string{"backupvolumes"}},
		{lhGVR("backuptargets"), []string{"backuptargets"}},
		// Images.
		{lhGVR("engineimages"), []string{"engineimages"}},
		{lhGVR("backingimages"), []string{"backingimages"}},
		{lhGVR("backupbackingimages"), []string{"backupbackingimages"}},
		// Misc list views.
		{lhGVR("settings"), []string{"settings"}},
		{lhGVR("recurringjobs"), []string{"recurringjobs"}},
		{lhGVR("instancemanagers"), []string{"instancemanagers"}},
		{lhGVR("orphans"), []string{"orphans"}},
		{lhGVR("systembackups"), []string{"systembackups"}},
		{lhGVR("systemrestores"), []string{"systemrestores"}},
		{lhGVR("supportbundles"), []string{"supportbundles"}},
		// Core Kubernetes events feed the Events view.
		{coreEventsGVR, []string{"events"}},
	}
}

type frame struct {
	keys []string
	name string
}

type sseClient struct {
	ch   chan frame
	done chan struct{}
	once sync.Once
}

// stop signals the handler to exit. The channel `ch` is NEVER closed, so the
// broker can send to it without racing a close (send-on-closed panic).
func (c *sseClient) stop() { c.once.Do(func() { close(c.done) }) }

// Hub is the change broker. Safe for concurrent use.
type Hub struct {
	dyn dynamic.Interface
	ns  string

	mu       sync.Mutex
	clients  map[*sseClient]struct{}
	pending  map[string]struct{}
	lastName string
	stopped  bool

	factory dynamicinformer.DynamicSharedInformerFactory
}

// NewHub builds a hub bound to the Longhorn namespace (default longhorn-system).
func NewHub(dyn dynamic.Interface, ns string) *Hub {
	if ns == "" {
		ns = "longhorn-system"
	}
	return &Hub{
		dyn:     dyn,
		ns:      ns,
		clients: map[*sseClient]struct{}{},
		pending: map[string]struct{}{},
	}
}

// Start registers the informers and the coalescing flush loop. It never blocks
// on cache sync, so a missing RBAC grant degrades to log spam + heartbeats-only
// rather than hanging.
func (h *Hub) Start(ctx context.Context) {
	h.factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(h.dyn, 10*time.Minute, h.ns, nil)
	for _, e := range watchedEntries() {
		keys := e.keys
		inf := h.factory.ForResource(e.gvr).Informer()
		handler := func(obj any) { h.mark(keys, nameOf(obj)) }
		if _, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { handler(obj) },
			UpdateFunc: func(_, obj any) { handler(obj) },
			DeleteFunc: func(obj any) { handler(obj) },
		}); err != nil {
			slog.Warn("watch: add handler failed", "gvr", e.gvr.String(), "err", err)
		}
	}
	h.factory.Start(ctx.Done())
	go h.flushLoop(ctx)
	slog.Info("watch hub started", "namespace", h.ns, "resources", len(watchedEntries()))
}

func nameOf(obj any) string {
	if m, err := meta.Accessor(obj); err == nil {
		return m.GetName()
	}
	return ""
}

func (h *Hub) mark(keys []string, name string) {
	h.mu.Lock()
	for _, k := range keys {
		h.pending[k] = struct{}{}
	}
	if name != "" {
		h.lastName = name
	}
	h.mu.Unlock()
}

func (h *Hub) flushLoop(ctx context.Context) {
	t := time.NewTicker(coalesceWindow)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.flushOnce()
		}
	}
}

// flushOnce coalesces all pending keys accumulated since the last tick into one
// frame and broadcasts it to every client. No-op when nothing changed.
func (h *Hub) flushOnce() {
	h.mu.Lock()
	if len(h.pending) == 0 {
		h.mu.Unlock()
		return
	}
	keys := make([]string, 0, len(h.pending))
	for k := range h.pending {
		keys = append(keys, k)
		delete(h.pending, k)
	}
	name := h.lastName
	h.lastName = ""
	clients := make([]*sseClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	fr := frame{keys: keys, name: name}
	for _, c := range clients {
		h.send(c, fr)
	}
}

// send is non-blocking: a slow (or gone) client never blocks the broker. It
// bails if the client has stopped, and on a full buffer drops the queue and
// enqueues a single "invalidate everything" frame so it self-heals. `ch` is
// never closed, so this can never panic with send-on-closed.
func (h *Hub) send(c *sseClient, fr frame) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.ch <- fr:
		return
	case <-c.done:
		return
	default:
	}
drain:
	for {
		select {
		case <-c.ch:
		default:
			break drain
		}
	}
	select {
	case c.ch <- frame{keys: []string{"__all__"}}:
	case <-c.done:
	default:
	}
}

func (h *Hub) subscribe() (*sseClient, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return nil, false
	}
	c := &sseClient{ch: make(chan frame, clientBuffer), done: make(chan struct{})}
	h.clients[c] = struct{}{}
	return c, true
}

func (h *Hub) unsubscribe(c *sseClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.stop()
}

// Stop signals every client to disconnect (unblocking their SSE handlers) so the
// HTTP server can shut down without waiting on long-lived streams. Idempotent.
func (h *Hub) Stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	clients := make([]*sseClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
		delete(h.clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.stop()
	}
}

// ServeSSE is the GET /api/v1/events/stream handler. Mount it inside the
// authenticated group; auth rides the session cookie and CSRF passes GETs.
func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "realtime unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	c, ok := h.subscribe()
	if !ok {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	defer h.unsubscribe(c)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 5000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()

	hb := time.NewTicker(heartbeat)
	defer hb.Stop()
	// Bound the stream lifetime so a long-open tab periodically reconnects and
	// re-authenticates (stateless tokens aren't revalidated mid-stream). A clean
	// return ends the response; EventSource auto-reconnects.
	life := time.NewTimer(maxStreamLifetime)
	defer life.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-life.C:
			return
		case <-hb.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case fr := <-c.ch:
			payload, _ := json.Marshal(map[string]any{"keys": fr.keys, "name": fr.name})
			var b strings.Builder
			b.WriteString("event: change\n")
			fmt.Fprintf(&b, "data: %s\n\n", payload)
			if _, err := io.WriteString(w, b.String()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
