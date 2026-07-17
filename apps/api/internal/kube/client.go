// Package kube owns Highland's connection to the Kubernetes API. Storage
// inventory, benchmarks, watches, and operation reconciliation all reuse one
// REST configuration instead of independently guessing in-cluster/kubeconfig
// settings.
package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const defaultUserAgent = "highland-storage-control-plane"

const (
	defaultQPS     = float32(20)
	defaultBurst   = 40
	defaultTimeout = 30 * time.Second
)

// Clients is the shared Kubernetes client bundle used by Highland subsystems.
type Clients struct {
	RESTConfig *rest.Config
	Core       kubernetes.Interface
	Dynamic    dynamic.Interface
	Discovery  discovery.DiscoveryInterface
}

// ConfigFromEnvironment resolves in-cluster configuration first, then falls
// back to KUBECONFIG (or ~/.kube/config) for local development.
func ConfigFromEnvironment() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		path := os.Getenv("KUBECONFIG")
		if path == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, fmt.Errorf("resolve home directory: %w", homeErr)
			}
			path = filepath.Join(home, ".kube", "config")
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("build kubernetes config: %w", err)
		}
	}

	copy := rest.CopyConfig(cfg)
	copy.UserAgent = boundedUserAgent(os.Getenv("HIGHLAND_KUBE_USER_AGENT"))
	copy.QPS, err = envFloat32("HIGHLAND_KUBE_QPS", copy.QPS, defaultQPS, 1, 1000)
	if err != nil {
		return nil, err
	}
	copy.Burst, err = envInt("HIGHLAND_KUBE_BURST", copy.Burst, defaultBurst, 1, 5000)
	if err != nil {
		return nil, err
	}
	copy.Timeout, err = envDuration("HIGHLAND_KUBE_TIMEOUT", copy.Timeout, defaultTimeout, time.Second, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	return copy, nil
}

func boundedUserAgent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultUserAgent
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func envFloat32(key string, current, fallback, min, max float32) (float32, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if current > 0 {
			return current, nil
		}
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil || parsed < float64(min) || parsed > float64(max) {
		return 0, fmt.Errorf("%s must be between %g and %g", key, min, max)
	}
	return float32(parsed), nil
}

func envInt(key string, current, fallback, min, max int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if current > 0 {
			return current, nil
		}
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return parsed, nil
}

func envDuration(key string, current, fallback, min, max time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if current > 0 {
			return current, nil
		}
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %s and %s", key, min, max)
	}
	return parsed, nil
}

// New creates typed, dynamic, and discovery clients from a single config.
func New(cfg *rest.Config) (*Clients, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kubernetes REST config is nil")
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic kubernetes client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	return &Clients{RESTConfig: cfg, Core: core, Dynamic: dyn, Discovery: disco}, nil
}

// NewFromEnvironment resolves configuration and constructs all clients.
func NewFromEnvironment() (*Clients, error) {
	cfg, err := ConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}
