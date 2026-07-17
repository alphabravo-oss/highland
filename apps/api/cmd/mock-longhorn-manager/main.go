// mock-longhorn-manager is a stateful fixture Longhorn manager /v1 API for local e2e/CI.
// It is not a reimplementation of Longhorn — only the collections/actions Highland Phase 1 uses.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type resource map[string]any

func main() {
	addr := envOr("MOCK_LH_ADDR", ":9500")
	s := newStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serve)
	log.Printf("mock-longhorn-manager listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

type store struct {
	mu       sync.Mutex
	volumes  map[string]resource
	nodes    []resource
	settings []resource
}

func newStore() *store {
	s := &store{
		volumes: map[string]resource{},
	}
	s.nodes = []resource{
		{
			"id": "node-1", "type": "node", "name": "node-1", "address": "10.0.0.1",
			"allowScheduling": true,
			"conditions":      []any{map[string]any{"type": "Ready", "status": "True"}},
			"disks": map[string]any{
				"default-disk": map[string]any{
					"path": "/var/lib/longhorn", "allowScheduling": true,
					"storageAvailable": int64(90 * 1024 * 1024 * 1024),
					"storageMaximum":   int64(100 * 1024 * 1024 * 1024),
					"storageScheduled": int64(0),
					"tags":             []string{"ssd"},
				},
			},
			"tags": []string{"worker"},
		},
	}
	s.settings = []resource{
		{
			"id": "default-replica-count", "type": "setting", "name": "default-replica-count",
			"value": "3",
			"definition": map[string]any{
				"displayName": "Default Replica Count",
				"category":    "general",
				"description": "Default number of replicas for a volume",
				"type":        "int",
			},
		},
		{
			"id": "guaranteed-instance-manager-cpu", "type": "setting", "name": "guaranteed-instance-manager-cpu",
			"value": "12",
			"definition": map[string]any{
				"displayName": "Guaranteed Instance Manager CPU",
				"category":    "danger zone",
				"description": "Danger zone CPU reservation",
				"type":        "int",
			},
		},
		{
			"id": "v2-data-engine", "type": "setting", "name": "v2-data-engine",
			"value": "true",
			"definition": map[string]any{
				"displayName": "V2 Data Engine",
				"category":    "v2 data engine",
				"description": "Enable the experimental SPDK-based v2 data engine",
				"type":        "bool",
			},
		},
	}
	// Seed one volume so empty clusters still show data
	s.upsertVolume("pvc-db", "10737418240", 3)
	return s
}

func (s *store) upsertVolume(name, size string, replicas int) resource {
	id := name
	v := resource{
		"id": id, "type": "volume", "name": name,
		"size": size, "state": "detached", "robustness": "healthy",
		"numberOfReplicas": replicas, "dataEngine": "v1", "frontend": "blockdev",
		"accessMode": "rwo", "dataLocality": "disabled",
		"replicas": []any{
			map[string]any{"name": name + "-r-a", "hostId": "node-1", "running": true, "mode": "RW"},
		},
		"conditions": []any{
			map[string]any{"type": "Scheduled", "status": "True", "message": "ok"},
		},
		"snapshots": []any{},
	}
	s.volumes[name] = v
	return v
}

func (s *store) withLinks(r resource, self string) resource {
	out := resource{}
	for k, v := range r {
		out[k] = v
	}
	out["links"] = map[string]string{"self": self}
	name, _ := r["name"].(string)
	if name == "" {
		name, _ = r["id"].(string)
	}
	if r["type"] == "volume" {
		out["actions"] = map[string]string{
			"attach":                      self + "?action=attach",
			"detach":                      self + "?action=detach",
			"snapshotCreate":              self + "?action=snapshotCreate",
			"snapshotList":                self + "?action=snapshotList",
			"snapshotDelete":              self + "?action=snapshotDelete",
			"snapshotRevert":              self + "?action=snapshotRevert",
			"snapshotBackup":              self + "?action=snapshotBackup",
			"salvage":                     self + "?action=salvage",
			"expand":                      self + "?action=expand",
			"cancelExpansion":             self + "?action=cancelExpansion",
			"cloneVolume":                 self + "?action=cloneVolume",
			"pvCreate":                    self + "?action=pvCreate",
			"pvcCreate":                   self + "?action=pvcCreate",
			"activate":                    self + "?action=activate",
			"trimFilesystem":              self + "?action=trimFilesystem",
			"engineUpgrade":               self + "?action=engineUpgrade",
			"recurringJobAdd":             self + "?action=recurringJobAdd",
			"recurringJobDelete":          self + "?action=recurringJobDelete",
			"updateReplicaCount":          self + "?action=updateReplicaCount",
			"updateDataLocality":          self + "?action=updateDataLocality",
			"updateAccessMode":            self + "?action=updateAccessMode",
			"updateBackupTargetName":      self + "?action=updateBackupTargetName",
			"updateReplicaAutoBalance":    self + "?action=updateReplicaAutoBalance",
			"updateSnapshotMaxCount":      self + "?action=updateSnapshotMaxCount",
			"updateSnapshotMaxSize":       self + "?action=updateSnapshotMaxSize",
			"updateSnapshotDataIntegrity": self + "?action=updateSnapshotDataIntegrity",
			"offlineReplicaRebuilding":    self + "?action=offlineReplicaRebuilding",
		}
	}
	return out
}

func (s *store) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	base := "http://" + r.Host
	path := r.URL.Path
	action := r.URL.Query().Get("action")

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case path == "/metrics" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		// Prometheus-style samples for offline sparkline demos
		_, _ = w.Write([]byte(`# HELP longhorn_volume_read_throughput Read throughput
# TYPE longhorn_volume_read_throughput gauge
longhorn_volume_read_throughput{volume="pvc-db"} 12.5e6
longhorn_volume_write_throughput{volume="pvc-db"} 8.2e6
longhorn_volume_read_iops{volume="pvc-db"} 4200
longhorn_volume_write_iops{volume="pvc-db"} 3100
longhorn_volume_actual_size_bytes{volume="pvc-db"} 2147483648
longhorn_disk_storage_maximum_bytes{node="node-1",disk="default-disk"} 107374182400
longhorn_disk_storage_available_bytes{node="node-1",disk="default-disk"} 96636764160
`))
		return

	case path == "/v1/dashboard" && r.Method == http.MethodGet:
		healthy, degraded, faulted, detached, attached := 0, 0, 0, 0, 0
		for _, v := range s.volumes {
			switch strings.ToLower(str(v["robustness"])) {
			case "healthy":
				healthy++
			case "degraded":
				degraded++
			case "faulted":
				faulted++
			}
			switch strings.ToLower(str(v["state"])) {
			case "attached":
				attached++
			default:
				detached++
			}
		}
		writeJSON(w, map[string]any{
			"type": "dashboard",
			"volumes": map[string]int{
				"total": len(s.volumes), "healthy": healthy, "degraded": degraded,
				"faulted": faulted, "detached": detached, "attached": attached,
			},
			"nodes":   map[string]int{"total": len(s.nodes), "ready": len(s.nodes)},
			"storage": map[string]int64{"total": 100 * 1024 * 1024 * 1024, "used": 10 * 1024 * 1024 * 1024, "available": 90 * 1024 * 1024 * 1024},
		})

	case path == "/v1/volumes" && r.Method == http.MethodGet:
		data := make([]any, 0, len(s.volumes))
		for name, v := range s.volumes {
			self := base + "/v1/volumes/" + name
			data = append(data, s.withLinks(v, self))
		}
		writeCollection(w, "volume", base+path, data)

	case path == "/v1/volumes" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := str(body["name"])
		if name == "" {
			http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
			return
		}
		size := str(body["size"])
		if size == "" {
			size = "10737418240"
		}
		replicas := 3
		if n, ok := body["numberOfReplicas"].(float64); ok {
			replicas = int(n)
		}
		v := s.upsertVolume(name, size, replicas)
		// Echo back engine/frontend the client requested so the UI reflects them.
		if de := str(body["dataEngine"]); de != "" {
			v["dataEngine"] = de
		}
		if fe := str(body["frontend"]); fe != "" {
			v["frontend"] = fe
		}
		self := base + "/v1/volumes/" + name
		writeJSON(w, s.withLinks(v, self))

	case strings.HasPrefix(path, "/v1/volumes/") && r.Method == http.MethodGet:
		name := strings.TrimPrefix(path, "/v1/volumes/")
		v, ok := s.volumes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, s.withLinks(v, base+path))

	case strings.HasPrefix(path, "/v1/volumes/") && r.Method == http.MethodDelete:
		name := strings.TrimPrefix(path, "/v1/volumes/")
		delete(s.volumes, name)
		w.WriteHeader(http.StatusNoContent)

	case strings.HasPrefix(path, "/v1/volumes/") && r.Method == http.MethodPost && action != "":
		name := strings.TrimPrefix(path, "/v1/volumes/")
		v, ok := s.volumes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch action {
		case "attach":
			v["state"] = "attached"
		case "detach":
			v["state"] = "detached"
		case "snapshotCreate":
			snaps, _ := v["snapshots"].([]any)
			snapName := str(body["name"])
			if snapName == "" {
				snapName = "snap-" + strconv.FormatInt(time.Now().Unix(), 10)
			}
			snaps = append(snaps, map[string]any{
				"name": snapName, "created": time.Now().UTC().Format(time.RFC3339),
			})
			v["snapshots"] = snaps
			v["snapshotCount"] = len(snaps)
		case "snapshotList":
			writeJSON(w, map[string]any{
				"data": v["snapshots"],
				"type": "collection",
			})
			return
		case "snapshotDelete":
			snaps, _ := v["snapshots"].([]any)
			target := str(body["name"])
			kept := make([]any, 0, len(snaps))
			for _, s := range snaps {
				m, _ := s.(map[string]any)
				if str(m["name"]) != target {
					kept = append(kept, s)
				}
			}
			v["snapshots"] = kept
			v["snapshotCount"] = len(kept)
		case "snapshotRevert", "snapshotBackup", "recurringJobAdd", "updateReplicaCount", "salvage":
			// accept and return volume
		case "expand":
			if sz := str(body["size"]); sz != "" {
				v["size"] = sz
			}
		}
		s.volumes[name] = v
		writeJSON(w, s.withLinks(v, base+path))

	case path == "/v1/nodes" && r.Method == http.MethodGet:
		data := make([]any, 0, len(s.nodes))
		for _, n := range s.nodes {
			name := str(n["name"])
			self := base + "/v1/nodes/" + name
			data = append(data, s.withLinks(n, self))
		}
		writeCollection(w, "node", base+path, data)

	case strings.HasPrefix(path, "/v1/nodes/") && r.Method == http.MethodPut:
		name := strings.TrimPrefix(path, "/v1/nodes/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for i, n := range s.nodes {
			if str(n["name"]) == name {
				for k, v := range body {
					if k == "links" || k == "actions" {
						continue
					}
					n[k] = v
				}
				s.nodes[i] = n
				writeJSON(w, s.withLinks(n, base+path))
				return
			}
		}
		http.NotFound(w, r)

	case path == "/v1/settings" && r.Method == http.MethodGet:
		data := make([]any, 0, len(s.settings))
		for _, st := range s.settings {
			name := str(st["name"])
			data = append(data, s.withLinks(st, base+"/v1/settings/"+name))
		}
		writeCollection(w, "setting", base+path, data)

	case strings.HasPrefix(path, "/v1/settings/") && r.Method == http.MethodPut:
		name := strings.TrimPrefix(path, "/v1/settings/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for i, st := range s.settings {
			if str(st["name"]) == name {
				if v, ok := body["value"]; ok {
					st["value"] = v
				}
				s.settings[i] = st
				writeJSON(w, s.withLinks(st, base+path))
				return
			}
		}
		http.NotFound(w, r)

	case path == "/v1/backupvolumes", path == "/v1/backuptargets", path == "/v1/recurringjobs",
		path == "/v1/engineimages", path == "/v1/backingimages", path == "/v1/instancemanagers",
		path == "/v1/orphans", path == "/v1/systembackups", path == "/v1/systemrestores",
		path == "/v1/supportbundles", path == "/v1/events", path == "/v1/volumeattachments":
		if r.Method == http.MethodGet {
			writeCollection(w, strings.TrimPrefix(path, "/v1/"), base+path, []any{})
			return
		}
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := str(body["name"])
			if name == "" {
				name = "created-" + strconv.FormatInt(time.Now().Unix(), 10)
			}
			writeJSON(w, map[string]any{
				"id": name, "type": strings.TrimSuffix(strings.TrimPrefix(path, "/v1/"), "s"),
				"name": name, "state": "ready",
				"links": map[string]string{"self": base + path + "/" + name},
			})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

	default:
		http.NotFound(w, r)
	}
}

func writeCollection(w http.ResponseWriter, resourceType, self string, data []any) {
	writeJSON(w, map[string]any{
		"type":         "collection",
		"resourceType": resourceType,
		"data":         data,
		"links":        map[string]string{"self": self},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
