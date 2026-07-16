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
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}, driverOwner: map[string]string{}}
}

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
	return nil
}

func (r *Registry) Provider(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
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

	out := make([]ProviderDescriptor, 0, len(providers)+len(discoveredDrivers))
	for _, p := range providers {
		d, err := p.Descriptor(ctx)
		if err != nil {
			continue
		}
		d.Capabilities = dedupeCapabilities(p.Capabilities(ctx))
		// Descriptor may add version/support conditions to live provider health.
		// Fall back to Health only for minimal provider implementations.
		if d.Health.ObservedAt.IsZero() {
			d.Health = p.Health(ctx)
		}
		out = append(out, d)
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
