package longhorn_test

import (
	"encoding/json"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/longhorn"
)

func TestRewriteRelativeManagerPaths(t *testing.T) {
	in := []byte(`{"actions":{"attach":"/v1/volumes/vol-1?action=attach"},"links":{"self":"/v1/volumes/vol-1"}}`)
	out := longhorn.RewriteLinks(in, "http://lh:9500", "/api/v1/lh", "/v1")
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	attach := m["actions"].(map[string]any)["attach"].(string)
	if attach != "/api/v1/lh/volumes/vol-1?action=attach" {
		t.Fatalf("attach = %q", attach)
	}
	self := m["links"].(map[string]any)["self"].(string)
	if self != "/api/v1/lh/volumes/vol-1" {
		t.Fatalf("self = %q", self)
	}
}
