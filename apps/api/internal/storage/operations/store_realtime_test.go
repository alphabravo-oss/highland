package operations

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

type operationPublisher struct {
	mu     sync.Mutex
	events []string
}

func (p *operationPublisher) PublishHighlandChange(eventType string, _ []string, _, _ string, _ any) {
	p.mu.Lock()
	p.events = append(p.events, eventType)
	p.mu.Unlock()
}

func TestStorePublishesOperationLifecycle(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	store, err := NewStore(client, "highland-system")
	if err != nil {
		t.Fatal(err)
	}
	publisher := &operationPublisher{}
	store.SetPublisher(publisher)
	operation, err := store.Create(context.Background(), Spec{
		ActionID: "create-pvc", Target: ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"},
		ParameterHash: strings.Repeat("a", 64), PlanHash: strings.Repeat("b", 64), IdempotencyHash: strings.Repeat("c", 64),
		RequestedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = Status{Phase: "Running", Step: "PreflightComplete"}
	if _, err := store.UpdateStatus(context.Background(), operation.Name, operation.Status); err != nil {
		t.Fatal(err)
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.events) != 2 || publisher.events[0] != "storage.operation.created" || publisher.events[1] != "storage.operation.updated" {
		t.Fatalf("events=%v", publisher.events)
	}
}
