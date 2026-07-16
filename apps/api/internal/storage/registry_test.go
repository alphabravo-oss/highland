package storage

import (
	"context"
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
