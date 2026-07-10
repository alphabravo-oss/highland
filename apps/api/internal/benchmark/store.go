package benchmark

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Phase of a benchmark.
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
)

// Benchmark is a Highland benchmark record.
type Benchmark struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	NodeName string `json:"nodeName,omitempty"`
	Profile  string `json:"profile"`
	// StorageClass targets a Longhorn StorageClass for a freshly-provisioned PVC
	// (defaults to the runner's HIGHLAND_FIO_STORAGECLASS, commonly "longhorn").
	StorageClass string `json:"storageClass,omitempty"`
	// Size is the requested PVC size (defaults to HIGHLAND_FIO_SIZE, e.g. 10Gi).
	Size string `json:"size,omitempty"`
	// PVCName references an existing PVC to benchmark instead of creating one.
	PVCName   string             `json:"pvcName,omitempty"`
	Phase     Phase              `json:"phase"`
	Message   string             `json:"message,omitempty"`
	CreatedAt time.Time          `json:"createdAt"`
	Completed *time.Time         `json:"completedAt,omitempty"`
	Results   map[string]float64 `json:"results,omitempty"`
	FioCmd    string             `json:"fioCmd,omitempty"`
	Mode      string             `json:"mode,omitempty"` // synthetic | kubernetes-job
}

// Store manages benchmarks (synthetic and/or k8s Job).
type Store struct {
	mu     sync.RWMutex
	items  map[string]*Benchmark
	runner *K8sRunner
}

// NewStore creates a store; runner may be nil (offline synthetic only).
func NewStore(runner *K8sRunner) *Store {
	return &Store{items: map[string]*Benchmark{}, runner: runner}
}

func (s *Store) List() []Benchmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Benchmark, 0, len(s.items))
	for _, b := range s.items {
		out = append(out, *b)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (s *Store) Get(name string) (*Benchmark, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[name]
	if !ok {
		return nil, false
	}
	cp := *b
	return &cp, true
}

func (s *Store) Create(b Benchmark) (*Benchmark, error) {
	if b.Name == "" {
		id, _ := randomHex(4)
		b.Name = "bench-" + id
	}
	if b.Profile == "" {
		b.Profile = "quick"
	}
	if b.Type == "" {
		b.Type = "Disk"
	}
	b.Phase = PhasePending
	b.CreatedAt = time.Now().UTC()
	b.FioCmd = fioCmdFor(b.Profile)
	b.Results = map[string]float64{}
	if s.runner != nil && s.runner.Available() {
		b.Mode = "kubernetes-job"
		b.Message = "queued fio Job"
	} else {
		b.Mode = "synthetic"
		b.Message = "offline synthetic (no cluster) — same API used for real Jobs on k3s"
	}

	s.mu.Lock()
	s.items[b.Name] = &b
	s.mu.Unlock()

	go s.run(b.Name)
	cp := b
	return &cp, nil
}

func (s *Store) Delete(name string) bool {
	s.mu.Lock()
	_, ok := s.items[name]
	delete(s.items, name)
	s.mu.Unlock()
	if ok && s.runner != nil && s.runner.Available() {
		// Tear down any cluster resources (Job + PVC we created) for this run.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			s.runner.Cleanup(ctx, name)
		}()
	}
	return ok
}

func (s *Store) run(name string) {
	s.mu.Lock()
	b, ok := s.items[name]
	if !ok {
		s.mu.Unlock()
		return
	}
	b.Phase = PhaseRunning
	mode := b.Mode
	req := Benchmark{
		Name:         b.Name,
		NodeName:     b.NodeName,
		Profile:      b.Profile,
		StorageClass: b.StorageClass,
		Size:         b.Size,
		PVCName:      b.PVCName,
		FioCmd:       b.FioCmd,
	}
	profile := b.Profile
	s.mu.Unlock()

	if mode == "kubernetes-job" && s.runner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		res, msg, err := s.runner.RunJob(ctx, &req)
		s.mu.Lock()
		defer s.mu.Unlock()
		bb, ok := s.items[name]
		if !ok {
			return
		}
		now := time.Now().UTC()
		bb.Completed = &now
		if err != nil {
			bb.Phase = PhaseFailed
			bb.Message = err.Error()
			return
		}
		bb.Phase = PhaseSucceeded
		bb.Message = msg
		bb.Results = res
		return
	}

	// Synthetic path
	delay := 800 * time.Millisecond
	switch profile {
	case "standard":
		delay = 1500 * time.Millisecond
	case "thorough":
		delay = 2500 * time.Millisecond
	}
	time.Sleep(delay)

	s.mu.Lock()
	defer s.mu.Unlock()
	bb, ok := s.items[name]
	if !ok {
		return
	}
	now := time.Now().UTC()
	bb.Completed = &now
	bb.Phase = PhaseSucceeded
	bb.Message = "SYNTHETIC (fabricated) results — offline fallback, NOT measured; cluster Job path activates automatically when kube API is reachable"
	mult := 1.0
	if profile == "standard" {
		mult = 1.2
	}
	if profile == "thorough" {
		mult = 1.4
	}
	bb.Results = map[string]float64{
		"seqReadMBps":   420 * mult,
		"seqWriteMBps":  380 * mult,
		"randReadIOPS":  28000 * mult,
		"randWriteIOPS": 22000 * mult,
		"latReadUs":     280 / mult,
		"latWriteUs":    320 / mult,
	}
}

// fioCmdFor builds an fio command that runs four sequential (stonewalled) jobs —
// seqread, seqwrite, randread, randwrite — against a file on the mounted Longhorn
// volume (mountPath) and emits a JSON report to stdout for parsing. Runtime scales
// with the profile; four jobs stay comfortably under the Job wait deadline.
func fioCmdFor(profile string) string {
	runtime := 10
	size := "512M"
	switch profile {
	case "standard":
		runtime = 20
		size = "1G"
	case "thorough":
		runtime = 45
		size = "2G"
	}
	global := fmt.Sprintf(
		"fio --output-format=json --directory=%s --filename=highland.fio --size=%s "+
			"--ioengine=libaio --direct=1 --time_based --runtime=%d --group_reporting",
		mountPath, size, runtime,
	)
	jobs := strings.Join([]string{
		"--name=seqread --rw=read --bs=1M --iodepth=16",
		"--name=seqwrite --stonewall --rw=write --bs=1M --iodepth=16",
		"--name=randread --stonewall --rw=randread --bs=4k --iodepth=32",
		"--name=randwrite --stonewall --rw=randwrite --bs=4k --iodepth=32",
	}, " ")
	return global + " " + jobs
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
