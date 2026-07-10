package metrics_test

import (
	"strings"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/metrics"
)

func TestParseProm(t *testing.T) {
	in := `# HELP x
longhorn_volume_read_throughput{volume="pvc-db"} 1.5e6
longhorn_disk_storage_maximum_bytes{node="n1"} 100
`
	pts, err := metrics.ParseProm(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d", len(pts))
	}
	if pts[0].Labels["volume"] != "pvc-db" {
		t.Fatalf("%v", pts[0].Labels)
	}
	if pts[0].Value != 1.5e6 {
		t.Fatalf("%v", pts[0].Value)
	}
}
