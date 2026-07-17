package storage

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testProvider struct{ descriptor ProviderDescriptor }

func (p testProvider) Descriptor(context.Context) (ProviderDescriptor, error) {
	return p.descriptor, nil
}
func (p testProvider) Health(context.Context) ProviderHealth {
	return ProviderHealth{Status: SeverityOK, ObservedAt: time.Now()}
}
func (p testProvider) Capabilities(context.Context) []Capability {
	return []Capability{CapabilityClaimsRead, CapabilityClaimsRead}
}

type resourceTestProvider struct{ testProvider }

func (resourceTestProvider) ResourceKinds(context.Context) []string { return []string{"zeta", "alpha"} }
func (resourceTestProvider) ListProviderResources(context.Context, string, PageRequest) (any, PageMeta, error) {
	return []any{}, PageMeta{}, nil
}
func (resourceTestProvider) GetProviderResource(context.Context, string, string) (any, error) {
	return nil, ErrNotFound
}

func TestRegistryPublishesProviderResourceContract(t *testing.T) {
	r := NewRegistry()
	provider := resourceTestProvider{testProvider{ProviderDescriptor{ID: "resources", Drivers: []string{"resources.example"}}}}
	if err := r.Register(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	descriptors := r.Descriptors(context.Background(), []string{"resources.example"})
	if len(descriptors) != 1 || len(descriptors[0].ResourceKinds) != 2 || descriptors[0].ResourceKinds[0] != "alpha" {
		t.Fatalf("resource contract not published in stable order: %#v", descriptors)
	}
}

func TestRegistryRejectsAmbiguousDriverOwnership(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	if err := r.Register(ctx, testProvider{ProviderDescriptor{ID: "one", Drivers: []string{"driver.example"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(ctx, testProvider{ProviderDescriptor{ID: "two", Drivers: []string{"driver.example"}}}); err == nil {
		t.Fatal("expected ambiguous driver error")
	}
}

func TestRegistrySynthesizesGenericProviders(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	if err := r.Register(ctx, testProvider{ProviderDescriptor{ID: "managed", Drivers: []string{"managed.example"}}}); err != nil {
		t.Fatal(err)
	}
	descriptors := r.Descriptors(ctx, []string{"managed.example", "unknown.example"})
	if len(descriptors) != 2 {
		t.Fatalf("expected managed + generic providers, got %#v", descriptors)
	}
	if got := r.ResolveDriver("unknown.example"); got != GenericProviderID("unknown.example") {
		t.Fatalf("unexpected generic id %q", got)
	}
}

func TestGenericProviderIDIsStableAndSafe(t *testing.T) {
	if got := GenericProviderID("rook-ceph.rbd.csi.ceph.com"); got != "csi-rook-ceph-rbd-csi-ceph-com-fbcda38c17" {
		t.Fatalf("unexpected id %q", got)
	}
	if GenericProviderID("a.b") == GenericProviderID("a-b") {
		t.Fatal("normalized driver names collided")
	}
	if len(GenericProviderID("very."+string(make([]byte, 300)))) > 128 {
		t.Fatal("generic provider id exceeded the API bound")
	}
}

type countingProvider struct {
	calls atomic.Int32
	delay time.Duration
}

func (p *countingProvider) Descriptor(ctx context.Context) (ProviderDescriptor, error) {
	p.calls.Add(1)
	select {
	case <-ctx.Done():
		return ProviderDescriptor{}, ctx.Err()
	case <-time.After(p.delay):
	}
	return ProviderDescriptor{
		ID: "counting", Drivers: []string{"counting.example"},
		Health: ProviderHealth{Status: SeverityOK, ObservedAt: time.Now()},
	}, nil
}
func (p *countingProvider) Health(context.Context) ProviderHealth {
	return ProviderHealth{Status: SeverityOK, ObservedAt: time.Now()}
}
func (p *countingProvider) Capabilities(context.Context) []Capability {
	return []Capability{CapabilityClaimsRead}
}

func TestDescriptorSnapshotDeduplicatesAndReturnsWarmCacheImmediately(t *testing.T) {
	registry := NewRegistry()
	provider := &countingProvider{delay: 40 * time.Millisecond}
	if err := registry.Register(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	provider.calls.Store(0)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, stale := registry.DescriptorSnapshot(context.Background(), []string{"counting.example"})
			if len(got) != 1 || stale {
				t.Errorf("snapshot=%#v stale=%t", got, stale)
			}
		}()
	}
	wg.Wait()
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("descriptor refreshes=%d, want 1", calls)
	}
	started := time.Now()
	got, _, stale := registry.DescriptorSnapshot(context.Background(), []string{"counting.example"})
	if len(got) != 1 || stale || time.Since(started) > 10*time.Millisecond {
		t.Fatalf("warm snapshot len=%d stale=%t duration=%s", len(got), stale, time.Since(started))
	}
}

func TestExpiredDescriptorSnapshotReturnsStaleWhileRefreshing(t *testing.T) {
	previous := providerDescriptorTTL
	providerDescriptorTTL = time.Millisecond
	t.Cleanup(func() { providerDescriptorTTL = previous })
	registry := NewRegistry()
	provider := &countingProvider{delay: 40 * time.Millisecond}
	if err := registry.Register(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := registry.DescriptorSnapshot(context.Background(), []string{"counting.example"}); len(got) != 1 {
		t.Fatal("missing initial snapshot")
	}
	time.Sleep(2 * time.Millisecond)
	started := time.Now()
	got, _, stale := registry.DescriptorSnapshot(context.Background(), []string{"counting.example"})
	if len(got) != 1 || !stale || !got[0].Health.Stale {
		t.Fatalf("expired snapshot=%#v stale=%t", got, stale)
	}
	if time.Since(started) > 10*time.Millisecond {
		t.Fatalf("stale cache blocked for %s", time.Since(started))
	}
}
