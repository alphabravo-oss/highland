package benchmark

import (
	"sync"
	"testing"
	"time"
)

func TestExecutionRequestPreservesPreparedAttribution(t *testing.T) {
	source := &Benchmark{
		Name:         "bench-attribution",
		Profile:      "quick",
		StorageClass: "openebs-hostpath",
		CSIDriver:    "openebs.io/local",
		ProviderID:   "openebs",
		AccessMode:   "ReadWriteOnce",
		VolumeMode:   "Filesystem",
	}

	request := executionRequest(source)
	if request.CSIDriver != source.CSIDriver {
		t.Fatalf("CSI driver lost between prepare and execution: got %q want %q", request.CSIDriver, source.CSIDriver)
	}
	if request.ProviderID != source.ProviderID {
		t.Fatalf("provider ID lost between prepare and execution: got %q want %q", request.ProviderID, source.ProviderID)
	}
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []string
}

func (p *recordingPublisher) PublishHighlandChange(eventType string, _ []string, _, _ string, _ any) {
	p.mu.Lock()
	p.events = append(p.events, eventType)
	p.mu.Unlock()
}

func (p *recordingPublisher) has(event string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, current := range p.events {
		if current == event {
			return true
		}
	}
	return false
}

func TestStorePublishesBenchmarkLifecycle(t *testing.T) {
	store := NewStore(nil)
	publisher := &recordingPublisher{}
	store.SetPublisher(publisher)
	created, err := store.Create(Benchmark{Profile: "quick"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !publisher.has("benchmark.succeeded") {
		time.Sleep(10 * time.Millisecond)
	}
	for _, event := range []string{"benchmark.created", "benchmark.running", "benchmark.succeeded"} {
		if !publisher.has(event) {
			t.Fatalf("missing lifecycle event %q", event)
		}
	}
	if !store.Delete(created.Name) || !publisher.has("benchmark.deleted") {
		t.Fatal("missing benchmark.deleted event")
	}
}
