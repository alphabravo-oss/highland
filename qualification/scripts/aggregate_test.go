package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeResult(t *testing.T, dir, name string, doc map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseDoc(profileID, gate, status string) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"profileId":     profileID,
		"provider":      "longhorn",
		"gate":          gate,
		"status":        status,
		"startedAt":     "2026-07-18T00:00:00Z",
		"endedAt":       "2026-07-18T00:10:00Z",
		"assertions": []any{
			map[string]any{"id": "QA-C4.1", "status": status},
		},
	}
}

func TestSatisfiesGate_SkippedAndNotRunNeverPass(t *testing.T) {
	for _, st := range []string{StatusSkipped, StatusNotRun, StatusFailed, StatusFlaky, StatusBlocked} {
		if SatisfiesGate(st, "production", "production") {
			t.Fatalf("status %q must not satisfy production", st)
		}
	}
	if !SatisfiesGate(StatusPassed, "production", "production") {
		t.Fatal("passed production result must satisfy production")
	}
	if SatisfiesGate(StatusPassed, "production", "pr") {
		t.Fatal("pr-gated pass must not satisfy production enforcement")
	}
}

func TestAggregate_SkippedCannotPassProduction(t *testing.T) {
	dir := t.TempDir()
	writeResult(t, dir, "skipped.json", baseDoc("linstor-drbd", "production", StatusSkipped))

	sum, err := Aggregate(dir, "production", []string{"linstor-drbd"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Passed {
		t.Fatal("expected aggregate failure when required profile is skipped")
	}
	if len(sum.NotPassed) == 0 && len(sum.RejectedNonPass) == 0 {
		t.Fatal("expected notPassed or rejectedNonPass evidence")
	}
}

func TestAggregate_NotRunCannotPassProduction(t *testing.T) {
	dir := t.TempDir()
	writeResult(t, dir, "not-run.json", baseDoc("longhorn-current", "production", StatusNotRun))

	sum, err := Aggregate(dir, "production", []string{"longhorn-current"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Passed {
		t.Fatal("expected aggregate failure when required profile is not-run")
	}
}

func TestAggregate_MissingRequiredFails(t *testing.T) {
	dir := t.TempDir()
	writeResult(t, dir, "other.json", baseDoc("generic-csi-kind", "pr", StatusPassed))

	sum, err := Aggregate(dir, "production", []string{"linstor-drbd", "ha-multi-replica"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Passed {
		t.Fatal("expected failure for missing required production profiles")
	}
	if len(sum.Missing) != 2 {
		t.Fatalf("expected 2 missing, got %v", sum.Missing)
	}
}

func TestAggregate_AllPassed(t *testing.T) {
	dir := t.TempDir()
	writeResult(t, dir, "a.json", baseDoc("linstor-drbd", "production", StatusPassed))
	writeResult(t, dir, "b.json", baseDoc("ha-multi-replica", "production", StatusPassed))

	sum, err := Aggregate(dir, "production", []string{"linstor-drbd", "ha-multi-replica"})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Passed {
		t.Fatalf("expected pass, summary=%+v", sum)
	}
}

func TestRedactValue(t *testing.T) {
	in := map[string]any{
		"profileId":  "x",
		"password":   "hunter2",
		"token":      "abc",
		"kubeconfig": "apiVersion: v1",
		"secret":     "s3cr3t",
		"nested": map[string]any{
			"token": "nested-token",
			"ok":    "visible",
		},
		"list": []any{
			map[string]any{"password": "p"},
		},
	}
	out := RedactValue(in).(map[string]any)
	if out["password"] != "[REDACTED]" || out["token"] != "[REDACTED]" ||
		out["kubeconfig"] != "[REDACTED]" || out["secret"] != "[REDACTED]" {
		t.Fatalf("top-level secrets not redacted: %#v", out)
	}
	nested := out["nested"].(map[string]any)
	if nested["token"] != "[REDACTED]" || nested["ok"] != "visible" {
		t.Fatalf("nested redaction failed: %#v", nested)
	}
	list0 := out["list"].([]any)[0].(map[string]any)
	if list0["password"] != "[REDACTED]" {
		t.Fatalf("list redaction failed: %#v", list0)
	}
}

func TestValidateResult_RejectsBadStatus(t *testing.T) {
	r := &Result{
		SchemaVersion: 1,
		ProfileID:     "x",
		Provider:      "longhorn",
		Gate:          "production",
		Status:        "success", // invalid
		StartedAt:     "2026-07-18T00:00:00Z",
		EndedAt:       "2026-07-18T00:01:00Z",
	}
	err := ValidateResult(r)
	if err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestLoadResultFile_RedactsAndValidates(t *testing.T) {
	dir := t.TempDir()
	doc := baseDoc("longhorn-current", "production", StatusPassed)
	doc["password"] = "should-not-leak"
	writeResult(t, dir, "ok.json", doc)

	r, err := LoadResultFile(filepath.Join(dir, "ok.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r.ProfileID != "longhorn-current" {
		t.Fatalf("profileId=%s", r.ProfileID)
	}
}

func TestAggregate_InvalidDocumentFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := Aggregate(dir, "production", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Passed {
		t.Fatal("invalid documents must fail aggregate")
	}
	if len(sum.Invalid) == 0 {
		t.Fatal("expected invalid entry")
	}
}
