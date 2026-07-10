package benchmark

import (
	"math"
	"testing"
)

func TestParseFioJSON(t *testing.T) {
	// Minimal but realistic fio --output-format=json payload with the four jobs
	// the runner emits. Some fio noise is prepended to exercise the JSON-start scan.
	logs := `fio-3.28
starting jobs
{
  "fio version": "fio-3.28",
  "jobs": [
    {"jobname": "seqread",  "read":  {"bw": 409600, "bw_bytes": 419430400, "iops": 400.0, "lat_ns": {"mean": 250000}}, "write": {}},
    {"jobname": "seqwrite", "read":  {}, "write": {"bw": 358400, "bw_bytes": 367001600, "iops": 350.0, "lat_ns": {"mean": 300000}}},
    {"jobname": "randread", "read":  {"iops": 25000.0, "clat_ns": {"mean": 128000}}, "write": {}},
    {"jobname": "randwrite","read":  {}, "write": {"iops": 20000.0, "lat_ns": {"mean": 160000}}}
  ]
}`

	res, err := parseFioJSON(logs)
	if err != nil {
		t.Fatalf("parseFioJSON error: %v", err)
	}

	// 419430400 bytes/s / 1MiB = 400 MiB/s
	if got := res["seqReadMBps"]; math.Abs(got-400) > 0.5 {
		t.Errorf("seqReadMBps = %v, want ~400", got)
	}
	if got := res["seqWriteMBps"]; math.Abs(got-350) > 0.5 {
		t.Errorf("seqWriteMBps = %v, want ~350", got)
	}
	if got := res["randReadIOPS"]; got != 25000 {
		t.Errorf("randReadIOPS = %v, want 25000", got)
	}
	if got := res["randWriteIOPS"]; got != 20000 {
		t.Errorf("randWriteIOPS = %v, want 20000", got)
	}
	// clat_ns.mean 128000 ns -> 128 us
	if got := res["latReadUs"]; math.Abs(got-128) > 0.01 {
		t.Errorf("latReadUs = %v, want 128", got)
	}
	// lat_ns.mean 160000 ns -> 160 us
	if got := res["latWriteUs"]; math.Abs(got-160) > 0.01 {
		t.Errorf("latWriteUs = %v, want 160", got)
	}
}

func TestParseFioJSONBwFallback(t *testing.T) {
	// No bw_bytes: falls back to bw (KiB/s). 1024 KiB/s = 1 MiB/s.
	logs := `{"jobs": [{"jobname": "seqread", "read": {"bw": 1024, "iops": 1.0}}]}`
	res, err := parseFioJSON(logs)
	if err != nil {
		t.Fatalf("parseFioJSON error: %v", err)
	}
	if got := res["seqReadMBps"]; math.Abs(got-1) > 0.001 {
		t.Errorf("seqReadMBps = %v, want 1", got)
	}
}

func TestParseFioJSONErrors(t *testing.T) {
	if _, err := parseFioJSON("no json here"); err == nil {
		t.Error("expected error for input without JSON object")
	}
	if _, err := parseFioJSON(`{"jobs": []}`); err == nil {
		t.Error("expected error for empty jobs array")
	}
	if _, err := parseFioJSON(`{"jobs": [{"jobname": "other", "read": {}}]}`); err == nil {
		t.Error("expected error when expected jobs are absent")
	}
}

func TestFioCmdForTargetsMountAndJobs(t *testing.T) {
	for _, p := range []string{"quick", "standard", "thorough", ""} {
		cmd := fioCmdFor(p)
		for _, want := range []string{
			"--output-format=json",
			"--directory=" + mountPath,
			"--name=seqread",
			"--name=seqwrite",
			"--name=randread",
			"--name=randwrite",
		} {
			if !contains(cmd, want) {
				t.Errorf("profile %q: fio cmd missing %q: %s", p, want, cmd)
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
