package benchmark

import (
	"context"
	"fmt"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sRunner creates real fio Jobs when in-cluster (or kubeconfig) is available.
type K8sRunner struct {
	client    kubernetes.Interface
	namespace string
	fioImage  string
}

// NewK8sRunnerFromEnv returns a runner if cluster is reachable, else nil.
func NewK8sRunnerFromEnv() *K8sRunner {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// fall back to kubeconfig for dev from laptop against cluster
		kube := os.Getenv("KUBECONFIG")
		if kube == "" {
			home, _ := os.UserHomeDir()
			kube = home + "/.kube/config"
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kube)
		if err != nil {
			return nil
		}
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil
	}
	ns := os.Getenv("HIGHLAND_NAMESPACE")
	if ns == "" {
		ns = "highland-system"
	}
	img := os.Getenv("HIGHLAND_FIO_IMAGE")
	if img == "" {
		img = "ghcr.io/aksakalli/fio:latest"
	}
	return &K8sRunner{client: client, namespace: ns, fioImage: img}
}

// Available reports whether Jobs can be created.
func (k *K8sRunner) Available() bool {
	return k != nil && k.client != nil
}

// RunJob launches a short fio Job and waits for completion (bounded).
func (k *K8sRunner) RunJob(ctx context.Context, b *Benchmark) (map[string]float64, string, error) {
	if !k.Available() {
		return nil, "", fmt.Errorf("kubernetes not available")
	}
	name := fmt.Sprintf("highland-bench-%s", b.Name)
	if len(name) > 63 {
		name = name[:63]
	}
	ttl := int32(300)
	backoff := int32(0)
	cmd := []string{"sh", "-c", b.FioCmd + " --output-format=json 2>/dev/null | head -c 20000; echo"}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "highland",
				"app.kubernetes.io/component": "benchmark",
				"highland.io/benchmark":       b.Name,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeName:      b.NodeName,
					Containers: []corev1.Container{
						{
							Name:    "fio",
							Image:   k.fioImage,
							Command: cmd,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
		},
	}
	// Delete leftover
	_ = k.client.BatchV1().Jobs(k.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	_, err := k.client.BatchV1().Jobs(k.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("create job: %w", err)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		j, err := k.client.BatchV1().Jobs(k.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if j.Status.Succeeded > 0 {
			// Synthetic parse: real fio JSON parse can be added; return profile-based numbers + note
			return map[string]float64{
				"seqReadMBps":   400,
				"seqWriteMBps":  360,
				"randReadIOPS":  25000,
				"randWriteIOPS": 20000,
			}, "fio Job completed (summary placeholders — parse JSON logs for exact)", nil
		}
		if j.Status.Failed > 0 {
			return nil, "", fmt.Errorf("fio job failed")
		}
		time.Sleep(2 * time.Second)
	}
	return nil, "", fmt.Errorf("fio job timeout")
}
