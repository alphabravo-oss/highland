package benchmark

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Persister durably stores benchmark records so they survive API restarts.
type Persister interface {
	Save(b *Benchmark)
	Remove(name string)
	LoadAll() []*Benchmark
}

const (
	cmPrefix    = "highland-bench-"
	cmDataKey   = "benchmark.json"
	cmKindLabel = "highland.io/kind"
	cmKindValue = "benchmark"
)

// ConfigMapPersister stores each benchmark as a labelled ConfigMap in the
// cluster (etcd) — no external datastore, kubectl-visible, RBAC-controlled, and
// durable across restarts.
type ConfigMapPersister struct {
	client    kubernetes.Interface
	namespace string
}

// NewConfigMapPersister builds a persister backed by ConfigMaps in namespace.
func NewConfigMapPersister(client kubernetes.Interface, namespace string) *ConfigMapPersister {
	return &ConfigMapPersister{client: client, namespace: namespace}
}

func cmName(name string) string {
	n := cmPrefix + name
	if len(n) > 253 {
		n = n[:253]
	}
	return n
}

// Save upserts the benchmark's ConfigMap.
func (p *ConfigMapPersister) Save(b *Benchmark) {
	data, err := json.Marshal(b)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cms := p.client.CoreV1().ConfigMaps(p.namespace)
	existing, err := cms.Get(ctx, cmName(b.Name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: cmName(b.Name),
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "highland",
					cmKindLabel:                    cmKindValue,
				},
			},
			Data: map[string]string{cmDataKey: string(data)},
		}
		if _, err := cms.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			slog.Warn("benchmark persist create failed", "name", b.Name, "err", err)
		}
		return
	}
	if err != nil {
		slog.Warn("benchmark persist get failed", "name", b.Name, "err", err)
		return
	}
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	existing.Data[cmDataKey] = string(data)
	if _, err := cms.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		slog.Warn("benchmark persist update failed", "name", b.Name, "err", err)
	}
}

// Remove deletes the benchmark's ConfigMap.
func (p *ConfigMapPersister) Remove(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.client.CoreV1().ConfigMaps(p.namespace).Delete(ctx, cmName(name), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Warn("benchmark persist delete failed", "name", name, "err", err)
	}
}

// LoadAll returns all persisted benchmarks.
func (p *ConfigMapPersister) LoadAll() []*Benchmark {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := p.client.CoreV1().ConfigMaps(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cmKindLabel + "=" + cmKindValue,
	})
	if err != nil {
		slog.Warn("benchmark load failed", "err", err)
		return nil
	}
	out := make([]*Benchmark, 0, len(list.Items))
	for i := range list.Items {
		raw, ok := list.Items[i].Data[cmDataKey]
		if !ok {
			continue
		}
		var b Benchmark
		if json.Unmarshal([]byte(raw), &b) == nil && b.Name != "" {
			out = append(out, &b)
		}
	}
	return out
}
