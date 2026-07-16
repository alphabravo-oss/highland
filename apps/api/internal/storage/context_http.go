package storage

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *HTTPAPI) ListRelationships(w http.ResponseWriter, r *http.Request) {
	query, err := a.relationshipQuery(r, strings.TrimSpace(r.URL.Query().Get("provider")), "", "")
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_RELATIONSHIP_QUERY", err.Error(), false, nil)
		return
	}
	a.writeRelationships(w, r, query)
}

func (a *HTTPAPI) GetProviderRelationships(w http.ResponseWriter, r *http.Request) {
	query, err := a.relationshipQuery(r, chi.URLParam(r, "providerId"), "", "")
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_RELATIONSHIP_QUERY", err.Error(), false, nil)
		return
	}
	a.writeRelationships(w, r, query)
}

func (a *HTTPAPI) GetResourceRelationships(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	kind, id := chi.URLParam(r, "kind"), chi.URLParam(r, "id")
	query, err := a.relationshipQuery(r, provider, kind, id)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_RELATIONSHIP_QUERY", err.Error(), false, nil)
		return
	}
	a.writeRelationships(w, r, query)
}

func (a *HTTPAPI) writeRelationships(w http.ResponseWriter, r *http.Request, query graphQuery) {
	if !a.ensureReady(w, r) {
		return
	}
	if _, ok := a.registry.Provider(query.provider); !ok {
		drivers, err := a.inventory.DiscoveredDriverNames()
		if err != nil {
			a.writeInventoryError(w, r, err)
			return
		}
		found := false
		for _, descriptor := range a.registry.Descriptors(r.Context(), drivers) {
			if descriptor.ID == query.provider {
				found = true
				break
			}
		}
		if !found {
			writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": query.provider})
			return
		}
	}
	started := time.Now()
	result, err := a.context.relationships(r.Context(), query)
	if observer, ok := a.observer.(ContextObserver); ok && err == nil {
		observer.ObserveStorageGraphBuild(query.provider, time.Since(started), unresolvedGraphNodes(result.Nodes))
	}
	if errors.Is(err, ErrNotFound) {
		writeAPIError(w, r, http.StatusNotFound, "GRAPH_RESOURCE_NOT_FOUND", "the exact graph resource ID was not found in the requested provider and kind", false, map[string]any{"providerId": query.provider, "kind": query.kind, "id": query.targetID})
		return
	}
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "RELATIONSHIP_BUILD_FAILED", err.Error(), true, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) relationshipQuery(r *http.Request, provider, pathKind, targetID string) (graphQuery, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return graphQuery{}, fmt.Errorf("provider is required")
	}
	if len(provider) > 253 {
		return graphQuery{}, fmt.Errorf("provider must not exceed 253 characters")
	}
	kind := strings.TrimSpace(pathKind)
	if kind == "" {
		kind = strings.TrimSpace(r.URL.Query().Get("kind"))
	}
	kind = normalizeGraphKind(kind)
	if kind == "" {
		return graphQuery{}, fmt.Errorf("kind is required")
	}
	if len(kind) > 64 {
		return graphQuery{}, fmt.Errorf("kind must not exceed 64 characters")
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if len(namespace) > 253 {
		return graphQuery{}, fmt.Errorf("namespace must not exceed 253 characters")
	}
	if isNamespacedGraphKind(kind) && namespace == "" && targetID == "" {
		return graphQuery{}, fmt.Errorf("namespace is required for kind %s", kind)
	}
	depth, err := boundedDepth(r.URL.Query().Get("depth"))
	if err != nil {
		return graphQuery{}, err
	}
	page, err := parsePageRequest(r)
	if err != nil {
		return graphQuery{}, err
	}
	if page.Limit > maxRelationshipNodes {
		return graphQuery{}, fmt.Errorf("limit must be between 1 and %d for relationship queries", maxRelationshipNodes)
	}
	if targetID != "" {
		if len(targetID) > 1024 {
			return graphQuery{}, fmt.Errorf("resource ID is too long")
		}
		expectedPrefix := "v1:" + kind + ":"
		if !strings.HasPrefix(targetID, expectedPrefix) {
			return graphQuery{}, fmt.Errorf("id must be the exact canonical graph ID for kind %s", kind)
		}
	}
	return graphQuery{provider: provider, namespace: namespace, kind: kind, targetID: targetID, depth: depth, page: page}, nil
}

func (a *HTTPAPI) GetImpact(w http.ResponseWriter, r *http.Request) {
	if !a.ensureReady(w, r) {
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	kind := normalizeGraphKind(r.URL.Query().Get("kind"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if provider == "" || kind == "" || id == "" {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_IMPACT_QUERY", "provider, kind, and exact canonical id are required", false, nil)
		return
	}
	if !strings.HasPrefix(id, "v1:"+kind+":") {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_IMPACT_QUERY", "id must be the exact canonical graph ID for the requested kind", false, nil)
		return
	}
	depth, err := boundedDepthDefault(r.URL.Query().Get("depth"), 5)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_IMPACT_QUERY", err.Error(), false, nil)
		return
	}
	result, err := a.context.impact(r.Context(), provider, kind, id, depth)
	if errors.Is(err, ErrNotFound) {
		writeAPIError(w, r, http.StatusNotFound, "IMPACT_TARGET_NOT_FOUND", "impact target was not found using the exact graph identity", false, map[string]any{"providerId": provider, "kind": kind, "id": id})
		return
	}
	if err != nil {
		if observer, ok := a.observer.(ContextObserver); ok {
			observer.IncStorageImpactFailure(provider, "build_failed")
		}
		writeAPIError(w, r, http.StatusServiceUnavailable, "IMPACT_ANALYSIS_FAILED", err.Error(), true, nil)
		return
	}
	if result.Incomplete {
		if observer, ok := a.observer.(ContextObserver); ok {
			observer.IncStorageImpactFailure(provider, "incomplete")
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *HTTPAPI) GetProviderDrift(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	page, err := parsePageRequest(r)
	if err != nil || page.Limit > maxRelationshipNodes {
		if err == nil {
			err = fmt.Errorf("limit must be between 1 and %d for drift queries", maxRelationshipNodes)
		}
		writeAPIError(w, r, http.StatusBadRequest, "INVALID_DRIFT_QUERY", err.Error(), false, nil)
		return
	}
	report, err := a.context.driftReport(r.Context(), providerID)
	if errors.Is(err, ErrNotFound) {
		writeAPIError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", "storage provider not found", false, map[string]any{"providerId": providerID})
		return
	}
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "DRIFT_READ_FAILED", err.Error(), true, nil)
		return
	}
	if observer, ok := a.observer.(ContextObserver); ok {
		observer.SetStorageDriftRecords(providerID, "error", report.Summary.Error)
		observer.SetStorageDriftRecords(providerID, "warning", report.Summary.Warning)
		observer.SetStorageDriftRecords(providerID, "info", report.Summary.Info)
	}
	report.Data, report.Page = paginate(report.Data, page)
	writeJSON(w, http.StatusOK, report)
}

func unresolvedGraphNodes(nodes []GraphNode) int {
	count := 0
	for _, node := range nodes {
		if node.Freshness == FreshnessUnknown {
			count++
			continue
		}
		for _, condition := range node.Conditions {
			if condition.Reason == "NoAuthoritativeBackendMatch" || condition.Reason == "ProviderResourceNotObserved" {
				count++
				break
			}
		}
	}
	return count
}

func boundedDepth(raw string) (int, error) {
	return boundedDepthDefault(raw, 2)
}

func boundedDepthDefault(raw string, defaultDepth int) (int, error) {
	if raw == "" {
		return defaultDepth, nil
	}
	depth, err := strconv.Atoi(raw)
	if err != nil || depth < 1 || depth > maxRelationshipDepth {
		return 0, fmt.Errorf("depth must be between 1 and %d", maxRelationshipDepth)
	}
	return depth, nil
}

func isNamespacedGraphKind(kind string) bool {
	switch kind {
	case "pvc", "pod", "workload", "volumesnapshot", "storage-operation":
		return true
	default:
		return false
	}
}
