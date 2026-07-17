package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Provider supplies backend-specific identity, health, and capabilities.
type Provider interface {
	Descriptor(context.Context) (ProviderDescriptor, error)
	Health(context.Context) ProviderHealth
	Capabilities(context.Context) []Capability
}

// InventoryEnricher optionally attaches authoritative provider details to
// common Kubernetes storage summaries.
type InventoryEnricher interface {
	EnrichClaims(context.Context, []ClaimSummary) error
	EnrichVolumes(context.Context, []PersistentVolumeSummary) error
}

// ProviderResourceReader exposes typed provider-specific read models.
type ProviderResourceReader interface {
	ResourceKinds(context.Context) []string
	ListProviderResources(context.Context, string, PageRequest) (any, PageMeta, error)
	GetProviderResource(context.Context, string, string) (any, error)
}

type ProviderSummaryReader interface {
	ProviderSummary(context.Context) (any, error)
}

type PageRequest struct {
	Limit    int
	Continue string
	Search   string
	Offset   int
}

// Registry resolves CSI driver ownership. A driver may be claimed by at most
// one configured managed provider; ambiguity is surfaced as an error.
type Registry struct {
	mu          sync.RWMutex
	providers   map[string]Provider
	driverOwner map[string]string
	cache       []ProviderDescriptor
	cacheKey    string
	cacheAt     time.Time
	refreshing  bool
	refreshDone chan struct{}
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}, driverOwner: map[string]string{}}
}

var providerDescriptorTTL = 10 * time.Second

func (r *Registry) Register(ctx context.Context, p Provider) error {
	if p == nil {
		return fmt.Errorf("provider is nil")
	}
	d, err := p.Descriptor(ctx)
	if err != nil {
		return fmt.Errorf("describe provider: %w", err)
	}
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("provider id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[d.ID]; exists {
		return fmt.Errorf("provider %q already registered", d.ID)
	}
	for _, driver := range d.Drivers {
		if owner, exists := r.driverOwner[driver]; exists {
			return fmt.Errorf("CSI driver %q claimed by providers %q and %q", driver, owner, d.ID)
		}
	}
	r.providers[d.ID] = p
	for _, driver := range d.Drivers {
		r.driverOwner[driver] = d.ID
	}
	r.cache = nil
	r.cacheAt = time.Time{}
	return nil
}

func (r *Registry) Provider(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) InvalidateDescriptors() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = nil
	r.cacheAt = time.Time{}
	r.cacheKey = ""
}

func (r *Registry) ResolveDriver(driver string) string {
	r.mu.RLock()
	owner := r.driverOwner[driver]
	r.mu.RUnlock()
	if owner != "" {
		return owner
	}
	return GenericProviderID(driver)
}

func (r *Registry) Descriptors(ctx context.Context, discoveredDrivers []string) []ProviderDescriptor {
	descriptors, _, _ := r.DescriptorSnapshot(ctx, discoveredDrivers)
	return descriptors
}

// DescriptorSnapshot makes navigation reads cheap and stable. The first caller
// performs one bounded refresh; warm callers receive the cached snapshot
// immediately. An expired snapshot is returned as stale while one asynchronous
// refresh runs, preventing slow provider probes from blocking the application
// shell or multiplying under concurrent requests.
func (r *Registry) DescriptorSnapshot(ctx context.Context, discoveredDrivers []string) ([]ProviderDescriptor, time.Time, bool) {
	drivers := append([]string(nil), discoveredDrivers...)
	sort.Strings(drivers)
	key := strings.Join(drivers, "\x00")

	r.mu.Lock()
	if len(r.cache) > 0 {
		stale := time.Since(r.cacheAt) >= providerDescriptorTTL || r.cacheKey != key
		snapshot, observedAt := cloneDescriptors(r.cache, stale), r.cacheAt
		if stale && !r.refreshing {
			r.refreshing = true
			r.refreshDone = make(chan struct{})
			go r.refreshDescriptors(drivers, key)
		}
		r.mu.Unlock()
		return snapshot, observedAt, stale
	}
	if r.refreshing {
		done := r.refreshDone
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, time.Time{}, true
		case <-done:
			r.mu.RLock()
			snapshot, observedAt := cloneDescriptors(r.cache, false), r.cacheAt
			r.mu.RUnlock()
			return snapshot, observedAt, len(snapshot) == 0
		}
	}
	r.refreshing = true
	r.refreshDone = make(chan struct{})
	r.mu.Unlock()

	refreshContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	snapshot := r.computeDescriptors(refreshContext, drivers)
	cancel()
	r.finishRefresh(snapshot, key)
	r.mu.RLock()
	result, observedAt := cloneDescriptors(r.cache, false), r.cacheAt
	r.mu.RUnlock()
	return result, observedAt, len(result) == 0
}

func (r *Registry) refreshDescriptors(drivers []string, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r.finishRefresh(r.computeDescriptors(ctx, drivers), key)
}

func (r *Registry) finishRefresh(snapshot []ProviderDescriptor, key string) {
	r.mu.Lock()
	if len(snapshot) > 0 {
		r.cache = cloneDescriptors(snapshot, false)
		r.cacheKey = key
		r.cacheAt = time.Now().UTC()
	}
	r.refreshing = false
	if r.refreshDone != nil {
		close(r.refreshDone)
		r.refreshDone = nil
	}
	r.mu.Unlock()
}

func (r *Registry) computeDescriptors(ctx context.Context, discoveredDrivers []string) []ProviderDescriptor {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	claimed := make(map[string]struct{}, len(r.driverOwner))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	for driver := range r.driverOwner {
		claimed[driver] = struct{}{}
	}
	r.mu.RUnlock()

	type result struct {
		descriptor ProviderDescriptor
		ok         bool
	}
	results := make(chan result, len(providers))
	var wg sync.WaitGroup
	for _, provider := range providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			d, err := p.Descriptor(ctx)
			if err != nil {
				results <- result{}
				return
			}
			if len(d.Capabilities) == 0 {
				d.Capabilities = dedupeCapabilities(p.Capabilities(ctx))
			}
			if d.Health.ObservedAt.IsZero() {
				d.Health = p.Health(ctx)
			}
			if reader, ok := p.(ProviderResourceReader); ok {
				d.ResourceKinds = append([]string(nil), reader.ResourceKinds(ctx)...)
				sort.Strings(d.ResourceKinds)
			}
			results <- result{descriptor: d, ok: true}
		}(provider)
	}
	wg.Wait()
	close(results)
	out := make([]ProviderDescriptor, 0, len(providers)+len(discoveredDrivers))
	for item := range results {
		if item.ok {
			out = append(out, item.descriptor)
		}
	}
	for _, driver := range discoveredDrivers {
		if _, ok := claimed[driver]; ok || driver == "" {
			continue
		}
		out = append(out, GenericDescriptor(driver))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneDescriptors(source []ProviderDescriptor, stale bool) []ProviderDescriptor {
	result := make([]ProviderDescriptor, len(source))
	for i := range source {
		result[i] = source[i]
		result[i].Drivers = append([]string(nil), source[i].Drivers...)
		result[i].Capabilities = append([]Capability(nil), source[i].Capabilities...)
		result[i].ResourceKinds = append([]string(nil), source[i].ResourceKinds...)
		result[i].Health.Conditions = append([]Condition(nil), source[i].Health.Conditions...)
		result[i].Health.Stale = source[i].Health.Stale || stale
		if source[i].Metadata != nil {
			result[i].Metadata = make(map[string]string, len(source[i].Metadata))
			for key, value := range source[i].Metadata {
				result[i].Metadata[key] = value
			}
		}
	}
	return result
}

func GenericProviderID(driver string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(driver) {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "driver"
	}
	if len(base) > 110 {
		base = strings.TrimRight(base[:110], "-")
	}
	sum := sha256.Sum256([]byte(driver))
	return "csi-" + base + "-" + hex.EncodeToString(sum[:5])
}

func GenericDescriptor(driver string) ProviderDescriptor {
	now := time.Now().UTC()
	return ProviderDescriptor{
		ID:           GenericProviderID(driver),
		Kind:         "csi",
		DisplayName:  driver,
		SupportLevel: SupportDetected,
		Drivers:      []string{driver},
		Capabilities: []Capability{
			CapabilityClaimsRead,
			CapabilityVolumesRead,
			CapabilityAttachmentsRead,
			CapabilityCapacityRead,
			CapabilityEventsRead,
		},
		Health: ProviderHealth{
			Status: SeverityUnknown,
			Conditions: []Condition{{
				Type: "BackendHealth", Status: "Unknown", Severity: SeverityUnknown,
				Reason: "GenericCSIDriver", Message: "CSI does not expose backend health through the Kubernetes API",
				ObservedAt: now,
			}},
			ObservedAt: now,
		},
	}
}

func dedupeCapabilities(in []Capability) []Capability {
	seen := map[Capability]struct{}{}
	out := make([]Capability, 0, len(in))
	for _, cap := range in {
		if _, ok := seen[cap]; ok {
			continue
		}
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
