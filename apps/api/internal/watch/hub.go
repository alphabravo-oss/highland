// Package watch turns Kubernetes and provider resource changes into scoped
// Server-Sent Events. Version 2 frames carry cluster/provider/namespace/resource
// identity while retaining legacy query keys during the Longhorn migration.
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
	gvr        schema.GroupVersionResource
	namespace  string
	providerID string
	kind       string
	keys       []string
}

// watchedEntries maps each watched Longhorn CRD (+ core events) to the TanStack
// query keys it invalidates. Resource plurals are the exact longhorn.io GVR
// names (see `kubectl api-resources --api-group=longhorn.io`).
func watchedEntries() []watchEntry {
	return []watchEntry{
		// Volumes and everything that reflects volume state.
		{gvr: lhGVR("volumes"), providerID: "longhorn", kind: "volumes", keys: []string{"volumes", "dashboard"}},
		{gvr: lhGVR("engines"), providerID: "longhorn", kind: "volumes", keys: []string{"volumes", "dashboard"}},
		{gvr: lhGVR("replicas"), providerID: "longhorn", kind: "volumes", keys: []string{"volumes", "dashboard"}},
		{gvr: lhGVR("volumeattachments"), providerID: "longhorn", kind: "attachments", keys: []string{"volumes"}},
		// Nodes.
		{gvr: lhGVR("nodes"), providerID: "longhorn", kind: "nodes", keys: []string{"nodes", "dashboard", "nodetags", "disktags"}},
		// Backups.
		{gvr: lhGVR("backupvolumes"), providerID: "longhorn", kind: "backups", keys: []string{"backupvolumes", "dashboard"}},
		{gvr: lhGVR("backups"), providerID: "longhorn", kind: "backups", keys: []string{"backupvolumes"}},
		{gvr: lhGVR("backuptargets"), providerID: "longhorn", kind: "backup-targets", keys: []string{"backuptargets"}},
		// Images.
		{gvr: lhGVR("engineimages"), providerID: "longhorn", kind: "engine-images", keys: []string{"engineimages"}},
		{gvr: lhGVR("backingimages"), providerID: "longhorn", kind: "backing-images", keys: []string{"backingimages"}},
		{gvr: lhGVR("backupbackingimages"), providerID: "longhorn", kind: "backing-images", keys: []string{"backupbackingimages"}},
		// Misc list views.
		{gvr: lhGVR("settings"), providerID: "longhorn", kind: "settings", keys: []string{"settings"}},
		{gvr: lhGVR("recurringjobs"), providerID: "longhorn", kind: "recurring-jobs", keys: []string{"recurringjobs"}},
		{gvr: lhGVR("instancemanagers"), providerID: "longhorn", kind: "instance-managers", keys: []string{"instancemanagers"}},
		{gvr: lhGVR("orphans"), providerID: "longhorn", kind: "orphans", keys: []string{"orphans"}},
		{gvr: lhGVR("systembackups"), providerID: "longhorn", kind: "system-backups", keys: []string{"systembackups"}},
		{gvr: lhGVR("systemrestores"), providerID: "longhorn", kind: "system-restores", keys: []string{"systemrestores"}},
		{gvr: lhGVR("supportbundles"), providerID: "longhorn", kind: "support-bundles", keys: []string{"supportbundles"}},
		// Core Kubernetes events feed the Events view.
		{gvr: coreEventsGVR, kind: "events", keys: []string{"events"}},
	}
}

type frame struct {
	version    int
	cluster    string
	eventType  string
	providerID string
	namespace  string
	kind       string
	keys       []string
	name       string
	entity     json.RawMessage
}

// Registration describes a dynamic informer and the cache scopes it invalidates.
// Providers may register entries before Start; duplicate GVR/namespace entries
// are rejected to avoid accidental double watches.
type Registration struct {
	GVR        schema.GroupVersionResource
	Namespace  string
	ProviderID string
	Kind       string
	QueryKeys  []string
}

type sseClient struct {
	ch   chan frame
	done chan struct{}
	once sync.Once
}

// stop signals the handler to exit. The channel `ch` is NEVER closed, so the
// broker can send to it without racing a close (send-on-closed panic).
func (c *sseClient) stop() { c.once.Do(func() { close(c.done) }) }

// ErrObserver counts informer watch/handler errors (implemented by
// observability.Metrics; a local interface keeps this package dependency-free).
type ErrObserver interface{ IncWatchError() }

// Hub is the change broker. Safe for concurrent use.
type Hub struct {
	dyn             dynamic.Interface
	ns              string
	clusterID       string
	includeLonghorn bool

	mu            sync.Mutex
	clients       map[*sseClient]struct{}
	pending       map[string]frame
	registrations []Registration
	stopped       bool

	errObs  ErrObserver
	factory dynamicinformer.DynamicSharedInformerFactory
}

// SetMetrics attaches a watch-error observer (nil disables it).
func (h *Hub) SetMetrics(o ErrObserver) { h.errObs = o }

// ClientCount returns the number of connected SSE clients (for a Prometheus
// GaugeFunc). Race-free: every mutation of clients holds h.mu.
func (h *Hub) ClientCount() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// NewHub builds a hub bound to the Longhorn namespace (default longhorn-system).
func NewHub(dyn dynamic.Interface, ns string) *Hub {
	if ns == "" {
		ns = "longhorn-system"
	}
	return &Hub{
		dyn:             dyn,
		ns:              ns,
		clusterID:       "local",
		includeLonghorn: true,
		clients:         map[*sseClient]struct{}{},
		pending:         map[string]frame{},
	}
}

// NewStorageHub creates a provider-neutral broker without implicit Longhorn
// CRD registrations. Providers may add their own registrations before Start.
func NewStorageHub(dyn dynamic.Interface) *Hub {
	h := NewHub(dyn, "default")
	h.includeLonghorn = false
	return h
}

// SetClusterID changes the bounded cluster identity included in v2 frames.
func (h *Hub) SetClusterID(id string) {
	if h == nil || strings.TrimSpace(id) == "" {
		return
	}
	h.mu.Lock()
	h.clusterID = strings.TrimSpace(id)
	h.mu.Unlock()
}

// Register adds a provider watch before Start.
func (h *Hub) Register(reg Registration) error {
	if h == nil || reg.GVR.Resource == "" || strings.TrimSpace(reg.Kind) == "" {
		return fmt.Errorf("watch registration requires GVR and kind")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.factory != nil {
		return fmt.Errorf("watch registration after start")
	}
	for _, existing := range h.registrations {
		if existing.GVR == reg.GVR && existing.Namespace == reg.Namespace {
			return fmt.Errorf("duplicate watch registration for %s namespace %q", reg.GVR, reg.Namespace)
		}
	}
	reg.QueryKeys = append([]string(nil), reg.QueryKeys...)
	h.registrations = append(h.registrations, reg)
	return nil
}

// Start registers the informers and the coalescing flush loop. It never blocks
// on cache sync, so a missing RBAC grant degrades to log spam + heartbeats-only
// rather than hanging.
func (h *Hub) Start(ctx context.Context) {
	h.factory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(h.dyn, 10*time.Minute, h.ns, nil)
	var entries []watchEntry
	if h.includeLonghorn {
		entries = watchedEntries()
	}
	h.mu.Lock()
	for _, reg := range h.registrations {
		entries = append(entries, watchEntry{gvr: reg.GVR, namespace: reg.Namespace, providerID: reg.ProviderID, kind: reg.Kind, keys: reg.QueryKeys})
	}
	h.mu.Unlock()
	for _, e := range entries {
		keys := e.keys
		providerID, kind := e.providerID, e.kind
		inf := h.factory.ForResource(e.gvr).Informer()
		// Count runtime watch/list failures (e.g. RBAC or connection loss).
		_ = inf.SetWatchErrorHandler(func(_ *cache.Reflector, _ error) {
			if h.errObs != nil {
				h.errObs.IncWatchError()
			}
		})
		handler := func(obj any) {
			accessor, _ := meta.Accessor(obj)
			namespace := ""
			if accessor != nil {
				namespace = accessor.GetNamespace()
			}
			h.markScoped(providerID, namespace, kind, nameOf(obj), keys)
		}
		if _, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { handler(obj) },
			UpdateFunc: func(_, obj any) { handler(obj) },
			DeleteFunc: func(obj any) { handler(obj) },
		}); err != nil {
			if h.errObs != nil {
				h.errObs.IncWatchError()
			}
			slog.Warn("watch: add handler failed", "gvr", e.gvr.String(), "err", err)
		}
	}
	h.factory.Start(ctx.Done())
	go h.flushLoop(ctx)
	slog.Info("watch hub started", "namespace", h.ns, "resources", len(entries))
}

func nameOf(obj any) string {
	if m, err := meta.Accessor(obj); err == nil {
		return m.GetName()
	}
	return ""
}

func (h *Hub) mark(keys []string, name string) {
	h.markScoped("", "", "", name, keys)
}

// PublishStorageChange implements storage.ChangePublisher. Inventory changes
// are already informer-backed, so they publish directly without a second watch.
func (h *Hub) PublishStorageChange(providerID, namespace, kind, name string) {
	h.markScoped(providerID, namespace, kind, name, nil)
}

// PublishHighlandChange sends application-native lifecycle events through the
// same authenticated stream as Kubernetes inventory changes.
func (h *Hub) PublishHighlandChange(eventType string, keys []string, resource, name string, entity any) {
	if h == nil {
		return
	}
	var encoded json.RawMessage
	if entity != nil {
		if payload, err := json.Marshal(entity); err == nil {
			encoded = payload
		}
	}
	h.mu.Lock()
	scopeKey := strings.Join([]string{"highland", eventType, resource, name}, "\x00")
	h.pending[scopeKey] = frame{
		version: 2, cluster: h.clusterID, eventType: eventType,
		kind: resource, name: name, keys: append([]string(nil), keys...), entity: encoded,
	}
	h.mu.Unlock()
}

func (h *Hub) markScoped(providerID, namespace, kind, name string, keys []string) {
	h.mu.Lock()
	scopeKey := strings.Join([]string{providerID, namespace, kind}, "\x00")
	fr := h.pending[scopeKey]
	fr.version = 2
	fr.cluster = h.clusterID
	fr.providerID = providerID
	fr.namespace = namespace
	fr.kind = kind
	fr.name = name
	seen := make(map[string]struct{}, len(fr.keys))
	for _, key := range fr.keys {
		seen[key] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := seen[key]; !ok && key != "" {
			fr.keys = append(fr.keys, key)
			seen[key] = struct{}{}
		}
	}
	h.pending[scopeKey] = fr
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
	frames := make([]frame, 0, len(h.pending))
	for key, fr := range h.pending {
		frames = append(frames, fr)
		delete(h.pending, key)
	}
	clients := make([]*sseClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, fr := range frames {
		for _, c := range clients {
			h.send(c, fr)
		}
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
	case c.ch <- frame{version: 2, cluster: h.clusterID, keys: []string{"__all__"}}:
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
			payload, _ := json.Marshal(map[string]any{
				"version": fr.version, "cluster": fr.cluster, "eventType": fr.eventType,
				"providerId": fr.providerID, "namespace": fr.namespace, "resource": fr.kind,
				"keys": fr.keys, "name": fr.name, "entity": fr.entity,
			})
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
