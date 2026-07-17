package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/highland-io/highland/apps/api/internal/kube"
	"github.com/highland-io/highland/apps/api/internal/storage"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// mountPath is where the selected CSI PVC is mounted inside the fio pod.
const mountPath = "/data"

// K8sRunner creates real fio Jobs when in-cluster (or kubeconfig) is available.
type K8sRunner struct {
	client       kubernetes.Interface
	restConfig   *rest.Config
	namespace    string
	fioImage     string
	storageClass string
	size         string
	providerFor  func(string) string
}

// NewK8sRunnerFromEnv returns a runner if cluster is reachable, else nil.
func NewK8sRunnerFromEnv() *K8sRunner {
	clients, err := kube.NewFromEnvironment()
	if err != nil {
		return nil
	}
	return NewK8sRunner(clients.Core, clients.RESTConfig)
}

// NewK8sRunner reuses Highland's shared Kubernetes clients and reads only the
// benchmark-specific execution settings from the environment.
func NewK8sRunner(client kubernetes.Interface, cfg *rest.Config) *K8sRunner {
	if client == nil || cfg == nil {
		return nil
	}
	ns := os.Getenv("HIGHLAND_NAMESPACE")
	if ns == "" {
		ns = "highland-system"
	}
	img := os.Getenv("HIGHLAND_FIO_IMAGE")
	if img == "" {
		// Public image with sh + fio (the Job runs `sh -c <fioCmd>`).
		img = "xridge/fio:latest"
	}
	sc := os.Getenv("HIGHLAND_FIO_STORAGECLASS")
	size := os.Getenv("HIGHLAND_FIO_SIZE")
	if size == "" {
		size = "10Gi"
	}
	return &K8sRunner{client: client, restConfig: cfg, namespace: ns, fioImage: img, storageClass: sc, size: size}
}

// Prepare validates the selected StorageClass and portable PVC mode before a
// benchmark is queued, and records its authoritative provisioner/provider.
func (k *K8sRunner) Prepare(b *Benchmark) error {
	if !k.Available() || b == nil {
		return fmt.Errorf("kubernetes not available")
	}
	var existing *corev1.PersistentVolumeClaim
	if b.PVCName != "" {
		claim, err := k.client.CoreV1().PersistentVolumeClaims(k.namespace).Get(context.Background(), b.PVCName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get existing PVC %q: %w", b.PVCName, err)
		}
		existing = claim
		if claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName == "" {
			return fmt.Errorf("existing PVC %q has no StorageClass", b.PVCName)
		}
		if b.StorageClass != "" && b.StorageClass != *claim.Spec.StorageClassName {
			return fmt.Errorf("existing PVC uses StorageClass %q, not %q", *claim.Spec.StorageClassName, b.StorageClass)
		}
		b.StorageClass = *claim.Spec.StorageClassName
	}
	if b.StorageClass == "" {
		b.StorageClass = k.storageClass
	}
	if b.StorageClass == "" {
		return fmt.Errorf("storageClass is required for a Kubernetes benchmark")
	}
	class, err := k.client.StorageV1().StorageClasses().Get(context.Background(), b.StorageClass, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get storage class %q: %w", b.StorageClass, err)
	}
	if b.AccessMode == "" {
		if existing != nil && len(existing.Spec.AccessModes) > 0 {
			b.AccessMode = string(existing.Spec.AccessModes[0])
		} else {
			b.AccessMode = string(corev1.ReadWriteOnce)
		}
	}
	validAccess := map[string]bool{string(corev1.ReadWriteOnce): true, string(corev1.ReadWriteMany): true, string(corev1.ReadOnlyMany): true, string(corev1.ReadWriteOncePod): true}
	if !validAccess[b.AccessMode] {
		return fmt.Errorf("unsupported accessMode %q", b.AccessMode)
	}
	if b.VolumeMode == "" {
		if existing != nil && existing.Spec.VolumeMode != nil {
			b.VolumeMode = string(*existing.Spec.VolumeMode)
		} else {
			b.VolumeMode = string(corev1.PersistentVolumeFilesystem)
		}
	}
	if b.VolumeMode != string(corev1.PersistentVolumeFilesystem) && b.VolumeMode != string(corev1.PersistentVolumeBlock) {
		return fmt.Errorf("unsupported volumeMode %q", b.VolumeMode)
	}
	if b.VolumeMode == string(corev1.PersistentVolumeBlock) {
		return fmt.Errorf("fio file profiles currently require Filesystem volumeMode")
	}
	b.CSIDriver = class.Provisioner
	b.ProviderID = k.resolveProvider(class.Provisioner)
	return nil
}

// SetProviderResolver connects benchmark attribution to the provider registry.
func (k *K8sRunner) SetProviderResolver(resolve func(string) string) { k.providerFor = resolve }

func (k *K8sRunner) resolveProvider(driver string) string {
	if k.providerFor != nil {
		return k.providerFor(driver)
	}
	return storage.GenericProviderID(driver)
}

// Clientset exposes the k8s client so other components (e.g. the ConfigMap
// persister) can reuse it.
func (k *K8sRunner) Clientset() kubernetes.Interface { return k.client }

// RESTConfig exposes the resolved REST config so other components (e.g. the
// dynamic watch hub) can build additional clients.
func (k *K8sRunner) RESTConfig() *rest.Config { return k.restConfig }

// Namespace is where Highland's cluster resources live.
func (k *K8sRunner) Namespace() string { return k.namespace }

// Available reports whether Jobs can be created.
func (k *K8sRunner) Available() bool {
	return k != nil && k.client != nil
}

func jobName(bench string) string {
	name := fmt.Sprintf("highland-bench-%s", bench)
	if len(name) > 58 {
		name = name[:58]
	}
	return name
}

func pvcName(bench string) string {
	return jobName(bench) + "-pvc"
}

// RunJob provisions a PVC from the selected StorageClass (unless an existing PVC is referenced),
// launches an fio Job that benchmarks a file on that volume, waits for
// completion (bounded), then parses the fio JSON logs into real results.
// Any PVC it creates is cleaned up before returning.
func (k *K8sRunner) RunJob(ctx context.Context, b *Benchmark) (map[string]float64, string, error) {
	if !k.Available() {
		return nil, "", fmt.Errorf("kubernetes not available")
	}

	name := jobName(b.Name)

	// Resolve storage class / size (per-benchmark override, else runner default).
	sc := b.StorageClass
	if sc == "" {
		sc = k.storageClass
	}
	size := b.Size
	if size == "" {
		size = k.size
	}

	// Determine which PVC to mount. If the request references an existing PVC we
	// use it as-is and never delete it; otherwise we create one for this run.
	claimName := b.PVCName
	createdPVC := false
	runSucceeded := false
	if claimName == "" {
		claimName = pvcName(b.Name)
		qty, err := resource.ParseQuantity(size)
		if err != nil {
			return nil, "", fmt.Errorf("invalid benchmark size %q: %w", size, err)
		}
		scName := sc
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   claimName,
				Labels: benchLabels(b.Name),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.PersistentVolumeAccessMode(b.AccessMode)},
				StorageClassName: &scName,
				VolumeMode:       ptrVolumeMode(corev1.PersistentVolumeMode(b.VolumeMode)),
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: qty,
					},
				},
			},
		}
		// Clean any leftover from a previous run of the same name.
		_ = k.client.CoreV1().PersistentVolumeClaims(k.namespace).Delete(ctx, claimName, metav1.DeleteOptions{})
		if _, err := k.client.CoreV1().PersistentVolumeClaims(k.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
			return nil, "", fmt.Errorf("create pvc: %w", err)
		}
		createdPVC = true
		// Ensure we do not leak the PVC we created, whatever the outcome.
		defer func() {
			if b.RetainFailedPVC && !runSucceeded {
				return
			}
			delCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			bg := metav1.DeletePropagationBackground
			_ = k.client.CoreV1().PersistentVolumeClaims(k.namespace).Delete(delCtx, claimName, metav1.DeleteOptions{PropagationPolicy: &bg})
		}()
	}

	ttl := int32(300)
	backoff := int32(0)
	// fio writes its JSON report to stdout; we read it back from the pod logs.
	cmd := []string{"sh", "-c", b.FioCmd}

	var nodeSelector map[string]string
	if b.NodeName != "" {
		nodeSelector = map[string]string{corev1.LabelHostname: b.NodeName}
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: benchLabels(b.Name),
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: benchLabels(b.Name)},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					// Do not hardcode nodeName: let the scheduler place the pod,
					// constrained by nodeSelector only when a node is requested.
					NodeSelector: nodeSelector,
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: claimName,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "fio",
							Image:   k.fioImage,
							Command: cmd,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: mountPath},
							},
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
	// Delete leftover job of the same name.
	_ = k.client.BatchV1().Jobs(k.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if _, err := k.client.BatchV1().Jobs(k.namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, "", fmt.Errorf("create job: %w", err)
	}
	// Results are captured from pod logs before RunJob returns, so the scratch
	// Job does not need to remain for its full TTL. Removing it first lets
	// Kubernetes release the pod's PVC reference and complete PVC/PV cleanup.
	defer func() {
		delCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		bg := metav1.DeletePropagationBackground
		_ = k.client.BatchV1().Jobs(k.namespace).Delete(delCtx, name, metav1.DeleteOptions{PropagationPolicy: &bg})
	}()

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		j, err := k.client.BatchV1().Jobs(k.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		if j.Status.Succeeded > 0 {
			logs, err := k.podLogs(ctx, b.Name)
			if err != nil {
				return nil, "", fmt.Errorf("read fio logs: %w", err)
			}
			results, err := parseFioJSON(logs)
			if err != nil {
				return nil, "", fmt.Errorf("parse fio output: %w", err)
			}
			k.populateMetadata(ctx, b, claimName)
			runSucceeded = true
			pvcNote := "existing PVC " + claimName
			if createdPVC {
				pvcNote = fmt.Sprintf("provisioned PVC (%s, %s; provisioner %s)", sc, size, b.CSIDriver)
			}
			return results, fmt.Sprintf("fio Job completed on %s", pvcNote), nil
		}
		if j.Status.Failed > 0 {
			logs, _ := k.podLogs(ctx, b.Name)
			return nil, "", fmt.Errorf("fio job failed: %s", tail(logs, 500))
		}
		time.Sleep(2 * time.Second)
	}
	return nil, "", fmt.Errorf("fio job timeout")
}

func ptrVolumeMode(mode corev1.PersistentVolumeMode) *corev1.PersistentVolumeMode { return &mode }

func (k *K8sRunner) populateMetadata(ctx context.Context, b *Benchmark, claimName string) {
	claim, err := k.client.CoreV1().PersistentVolumeClaims(k.namespace).Get(ctx, claimName, metav1.GetOptions{})
	if err != nil {
		return
	}
	b.PVCName, b.PVName = claim.Name, claim.Spec.VolumeName
	if claim.Spec.VolumeName != "" {
		if pv, getErr := k.client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{}); getErr == nil && pv.Spec.CSI != nil {
			b.CSIDriver = pv.Spec.CSI.Driver
			b.ProviderID = k.resolveProvider(pv.Spec.CSI.Driver)
		}
	}
	pods, err := k.client.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{LabelSelector: "highland.io/benchmark=" + b.Name})
	if err != nil || len(pods.Items) == 0 {
		return
	}
	b.NodeName = pods.Items[0].Spec.NodeName
	if b.NodeName != "" {
		if node, getErr := k.client.CoreV1().Nodes().Get(ctx, b.NodeName, metav1.GetOptions{}); getErr == nil {
			b.Topology = map[string]string{}
			for key, value := range node.Labels {
				if strings.HasPrefix(key, "topology.kubernetes.io/") || strings.HasPrefix(key, "topology.") {
					b.Topology[key] = value
				}
			}
		}
	}
}

// Cleanup removes the Job (and any created PVC) for a benchmark. Safe to call
// even if the resources are already gone.
func (k *K8sRunner) Cleanup(ctx context.Context, bench string) {
	if !k.Available() {
		return
	}
	bg := metav1.DeletePropagationBackground
	_ = k.client.BatchV1().Jobs(k.namespace).Delete(ctx, jobName(bench), metav1.DeleteOptions{PropagationPolicy: &bg})
	_ = k.client.CoreV1().PersistentVolumeClaims(k.namespace).Delete(ctx, pvcName(bench), metav1.DeleteOptions{PropagationPolicy: &bg})
}

func benchLabels(bench string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "highland",
		"app.kubernetes.io/component": "benchmark",
		"highland.io/benchmark":       bench,
	}
}

// podLogs returns the combined logs of the pod(s) belonging to a benchmark Job.
func (k *K8sRunner) podLogs(ctx context.Context, bench string) (string, error) {
	pods, err := k.client.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "highland.io/benchmark=" + bench,
	})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for benchmark %s", bench)
	}
	var sb strings.Builder
	for _, p := range pods.Items {
		req := k.client.CoreV1().Pods(k.namespace).GetLogs(p.Name, &corev1.PodLogOptions{})
		rc, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		sb.Write(data)
	}
	return sb.String(), nil
}

// fioReport is the subset of fio's JSON output we consume.
type fioReport struct {
	Jobs []fioJob `json:"jobs"`
}

type fioJob struct {
	JobName string    `json:"jobname"`
	Read    fioStream `json:"read"`
	Write   fioStream `json:"write"`
}

type fioStream struct {
	// bw is in KiB/s; bw_bytes (when present) is bytes/s.
	Bw      float64 `json:"bw"`
	BwBytes float64 `json:"bw_bytes"`
	IOPS    float64 `json:"iops"`
	LatNs   fioLat  `json:"lat_ns"`
	ClatNs  fioLat  `json:"clat_ns"`
}

type fioLat struct {
	Mean float64 `json:"mean"`
}

func (s fioStream) mbps() float64 {
	bytesPerSec := s.BwBytes
	if bytesPerSec <= 0 {
		bytesPerSec = s.Bw * 1024 // KiB/s -> bytes/s
	}
	return bytesPerSec / (1024 * 1024) // bytes/s -> MiB/s
}

func (s fioStream) latUs() float64 {
	mean := s.LatNs.Mean
	if mean <= 0 {
		mean = s.ClatNs.Mean
	}
	return mean / 1000.0 // ns -> us
}

// parseFioJSON extracts real throughput/IOPS/latency numbers from fio JSON.
// It expects jobs named seqread, seqwrite, randread, randwrite.
func parseFioJSON(logs string) (map[string]float64, error) {
	start := strings.IndexByte(logs, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object in fio output")
	}
	var rep fioReport
	if err := json.Unmarshal([]byte(logs[start:]), &rep); err != nil {
		return nil, fmt.Errorf("unmarshal fio json: %w", err)
	}
	if len(rep.Jobs) == 0 {
		return nil, fmt.Errorf("fio output contained no jobs")
	}
	byName := map[string]fioJob{}
	for _, j := range rep.Jobs {
		byName[j.JobName] = j
	}
	res := map[string]float64{}
	if j, ok := byName["seqread"]; ok {
		res["seqReadMBps"] = j.Read.mbps()
	}
	if j, ok := byName["seqwrite"]; ok {
		res["seqWriteMBps"] = j.Write.mbps()
	}
	if j, ok := byName["randread"]; ok {
		res["randReadIOPS"] = j.Read.IOPS
		res["latReadUs"] = j.Read.latUs()
	}
	if j, ok := byName["randwrite"]; ok {
		res["randWriteIOPS"] = j.Write.IOPS
		res["latWriteUs"] = j.Write.latUs()
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("fio output missing expected jobs (seqread/seqwrite/randread/randwrite)")
	}
	return res, nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
