// Package longhorn adapts Highland's existing Longhorn proxy, stream, and
// metrics implementation to the provider-neutral storage contract.
package longhorn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	legacy "github.com/highland-io/highland/apps/api/internal/longhorn"
	longhornmetrics "github.com/highland-io/highland/apps/api/internal/metrics"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/storage"
)

const DriverName = "driver.longhorn.io"

type Config struct {
	ManagerURL     string
	Namespace      string
	Version        string
	Required       bool
	ScrapeInterval time.Duration
	MetricsSamples int
	HTTPClient     *http.Client
	Observer       *observability.Metrics
}

type Adapter struct {
	managerURL string
	namespace  string
	version    string
	required   bool
	client     *http.Client
	proxy      *legacy.Proxy
	stream     *legacy.StreamProxy
	scraper    *longhornmetrics.Scraper

	mu        sync.RWMutex
	lastFacts map[string]map[string]any
	lastRead  time.Time
}

func New(cfg Config) (*Adapter, error) {
	parsed, err := url.Parse(strings.TrimRight(cfg.ManagerURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid Longhorn manager URL")
	}
	proxy, err := legacy.NewProxy(parsed.String())
	if err != nil {
		return nil, err
	}
	proxy.SetMetrics(cfg.Observer)
	stream, err := legacy.NewStreamProxy(parsed.String())
	if err != nil {
		return nil, err
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "longhorn-system"
	}
	if cfg.ScrapeInterval <= 0 {
		cfg.ScrapeInterval = 10 * time.Second
	}
	if cfg.MetricsSamples <= 0 {
		cfg.MetricsSamples = 60
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConns: 20, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second}}
	}
	return &Adapter{
		managerURL: parsed.String(), namespace: cfg.Namespace, version: cfg.Version, required: cfg.Required,
		client: client, proxy: proxy, stream: stream,
		scraper:   longhornmetrics.NewScraper(parsed.String(), cfg.ScrapeInterval, cfg.MetricsSamples),
		lastFacts: map[string]map[string]any{},
	}, nil
}

func (a *Adapter) Start() {
	if a != nil && a.scraper != nil {
		a.scraper.Start()
	}
}
func (a *Adapter) Stop() {
	if a != nil && a.scraper != nil {
		a.scraper.Stop()
	}
}
func (a *Adapter) Proxy() *legacy.Proxy {
	if a == nil {
		return nil
	}
	return a.proxy
}
func (a *Adapter) Stream() *legacy.StreamProxy {
	if a == nil {
		return nil
	}
	return a.stream
}
func (a *Adapter) Scraper() *longhornmetrics.Scraper {
	if a == nil {
		return nil
	}
	return a.scraper
}

func (a *Adapter) ID() string        { return "longhorn" }
func (a *Adapter) Drivers() []string { return []string{DriverName} }

func (a *Adapter) Descriptor(ctx context.Context) (storage.ProviderDescriptor, error) {
	health := a.Health(ctx)
	version := a.detectVersion(ctx)
	supported := supportedVersion(version)
	if version == "" {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "VersionSupported", Status: "Unknown", Severity: storage.SeverityWarning, Reason: "VersionUnavailable", Message: "Longhorn version-sensitive common actions are withheld until the manager reports its version.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	} else if !supported {
		health.Conditions = append(health.Conditions, storage.Condition{Type: "VersionSupported", Status: "False", Severity: storage.SeverityWarning, Reason: "UntestedVersion", Message: "This Longhorn version is outside Highland's declared compatibility matrix; legacy routes remain available.", ObservedAt: time.Now().UTC()})
		if health.Status == storage.SeverityOK {
			health.Status = storage.SeverityWarning
		}
	}
	return storage.ProviderDescriptor{
		ID: a.ID(), Kind: "longhorn", DisplayName: "Longhorn", SupportLevel: storage.SupportManaged,
		Drivers: a.Drivers(), Version: version, Namespace: a.namespace,
		Capabilities: a.Capabilities(ctx), Health: health,
		Metadata: map[string]string{"managerReachable": fmt.Sprintf("%t", health.Status != storage.SeverityError), "required": fmt.Sprintf("%t", a.required), "versionSupported": fmt.Sprintf("%t", supported)},
	}, nil
}

func (a *Adapter) Health(ctx context.Context) storage.ProviderHealth {
	now := time.Now().UTC()
	health := storage.ProviderHealth{Status: storage.SeverityOK, ObservedAt: now}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.managerURL+"/v1", nil)
	if err == nil {
		response, requestErr := a.client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				health.Conditions = []storage.Condition{{Type: "ManagerReachable", Status: "True", Severity: storage.SeverityOK, Reason: "ManagerResponded", ObservedAt: now}}
				return health
			}
			err = fmt.Errorf("manager returned HTTP %d", response.StatusCode)
		} else {
			err = requestErr
		}
	}
	health.Status = storage.SeverityError
	health.Conditions = []storage.Condition{{Type: "ManagerReachable", Status: "False", Severity: storage.SeverityError, Reason: "ManagerUnavailable", Message: sanitizeError(err), ObservedAt: now}}
	return health
}

func (a *Adapter) Capabilities(ctx context.Context) []storage.Capability {
	capabilities := []storage.Capability{
		storage.CapabilityClaimsRead, storage.CapabilityVolumesRead, storage.CapabilityAttachmentsRead,
		storage.CapabilitySnapshotsRead, storage.CapabilityCapacityRead, storage.CapabilityEventsRead,
		storage.CapabilityProviderHealth,
	}
	if supportedVersion(a.detectVersion(ctx)) {
		capabilities = append(capabilities, storage.CapabilityVolumeCreate, storage.CapabilityVolumeExpand,
			storage.CapabilityVolumeDelete, storage.CapabilitySnapshotCreate, storage.CapabilitySnapshotDelete,
			storage.CapabilitySnapshotRestore, storage.CapabilityVolumeClone)
	}
	return capabilities
}

func (a *Adapter) detectVersion(ctx context.Context) string {
	a.mu.RLock()
	version := a.version
	a.mu.RUnlock()
	if version != "" {
		return version
	}
	items, err := a.list(ctx, "settings")
	if err != nil {
		return ""
	}
	for _, item := range items {
		if objectID(item) == "current-longhorn-version" || objectID(item) == "longhorn-version" {
			if value := strings.TrimSpace(fmt.Sprint(item["value"])); value != "" && value != "<nil>" {
				a.mu.Lock()
				a.version = value
				a.mu.Unlock()
				return value
			}
		}
	}
	return ""
}

func supportedVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	return strings.HasPrefix(version, "1.12.") || strings.HasPrefix(version, "1.11.")
}

func (a *Adapter) EnrichClaims(ctx context.Context, claims []storage.ClaimSummary) error {
	facts, err := a.volumeFacts(ctx)
	if err != nil {
		return err
	}
	for index := range claims {
		if claims[index].Driver != DriverName || claims[index].VolumeHandle == "" {
			continue
		}
		if _, ok := facts[claims[index].VolumeHandle]; ok {
			claims[index].ProviderRef = &storage.ProviderReference{Kind: "longhorn-volume", ID: claims[index].VolumeHandle}
		}
	}
	return nil
}

func (a *Adapter) EnrichVolumes(ctx context.Context, volumes []storage.PersistentVolumeSummary) error {
	facts, err := a.volumeFacts(ctx)
	if err != nil {
		return err
	}
	for index := range volumes {
		if volumes[index].Driver != DriverName || volumes[index].VolumeHandle == "" {
			continue
		}
		fact, ok := facts[volumes[index].VolumeHandle]
		if !ok {
			volumes[index].Conditions = append(volumes[index].Conditions, storage.Condition{Type: "BackendCorrelation", Status: "False", Severity: storage.SeverityWarning, Reason: "LonghornVolumeNotObserved", Message: "No Longhorn volume matched the authoritative CSI volume handle."})
			continue
		}
		volumes[index].ProviderRef = &storage.ProviderReference{Kind: "longhorn-volume", ID: volumes[index].VolumeHandle}
		volumes[index].Backend = fact
		if value, ok := fact["backendAllocatedCapacity"].(string); ok {
			volumes[index].BackendAllocated = value
		}
	}
	return nil
}

func (a *Adapter) ResourceKinds(context.Context) []string {
	return []string{"volumes", "nodes", "backups"}
}

func (a *Adapter) ListProviderResources(ctx context.Context, kind string, page storage.PageRequest) (any, storage.PageMeta, error) {
	resource := map[string]string{"volumes": "volumes", "nodes": "nodes", "backups": "backupvolumes"}[kind]
	if resource == "" {
		return nil, storage.PageMeta{}, storage.ErrNotFound
	}
	items, err := a.list(ctx, resource)
	if err != nil {
		return nil, storage.PageMeta{}, err
	}
	start := page.Offset
	if start > len(items) {
		start = len(items)
	}
	end := start + page.Limit
	if end > len(items) {
		end = len(items)
	}
	result := make([]any, 0, end-start)
	for _, item := range items[start:end] {
		result = append(result, bounded(item))
	}
	meta := storage.PageMeta{Limit: page.Limit, Total: len(items)}
	if end < len(items) {
		meta.Continue = storage.EncodePageOffset(end)
	}
	return result, meta, nil
}

func (a *Adapter) GetProviderResource(ctx context.Context, kind, id string) (any, error) {
	resource := map[string]string{"volumes": "volumes", "nodes": "nodes", "backups": "backupvolumes"}[kind]
	items, err := a.list(ctx, resource)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if objectID(item) == id {
			return bounded(item), nil
		}
	}
	return nil, storage.ErrNotFound
}

func (a *Adapter) volumeFacts(ctx context.Context) (map[string]map[string]any, error) {
	a.mu.RLock()
	if time.Since(a.lastRead) < 5*time.Second && len(a.lastFacts) > 0 {
		facts := a.lastFacts
		a.mu.RUnlock()
		return facts, nil
	}
	a.mu.RUnlock()
	items, err := a.list(ctx, "volumes")
	if err != nil {
		return nil, err
	}
	facts := make(map[string]map[string]any, len(items))
	for _, item := range items {
		id := objectID(item)
		if id == "" {
			continue
		}
		fact := map[string]any{}
		for _, key := range []string{"state", "robustness", "frontend", "numberOfReplicas", "actualSize", "lastBackup"} {
			if value, ok := item[key]; ok {
				fact[key] = value
			}
		}
		if size, ok := item["actualSize"]; ok {
			fact["backendAllocatedCapacity"] = fmt.Sprint(size)
		}
		facts[id] = fact
	}
	a.mu.Lock()
	a.lastFacts, a.lastRead = facts, time.Now()
	a.mu.Unlock()
	return facts, nil
}

func (a *Adapter) list(ctx context.Context, resource string) ([]map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.managerURL+"/v1/"+resource, nil)
	if err != nil {
		return nil, err
	}
	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Longhorn %s read failed: %w", resource, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Longhorn %s returned HTTP %d", resource, response.StatusCode)
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode Longhorn %s: %w", resource, err)
	}
	return envelope.Data, nil
}

func objectID(item map[string]any) string {
	for _, key := range []string{"id", "name"} {
		if value, ok := item[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func bounded(item map[string]any) map[string]any {
	allowed := []string{"id", "name", "state", "robustness", "frontend", "actualSize", "size", "nodeId", "region", "zone", "allowScheduling", "conditions", "disks", "snapshotName", "created", "lastBackupName"}
	result := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if value, ok := item[key]; ok {
			result[key] = value
		}
	}
	return result
}

func sanitizeError(err error) string {
	if err == nil {
		return "manager unavailable"
	}
	message := err.Error()
	if len(message) > 300 {
		message = message[:300]
	}
	return strings.ReplaceAll(message, "\n", " ")
}

var _ storage.Provider = (*Adapter)(nil)
var _ storage.InventoryEnricher = (*Adapter)(nil)
var _ storage.ProviderResourceReader = (*Adapter)(nil)
