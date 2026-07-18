package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDurableRequiresWritableAppendOnlyFile(t *testing.T) {
	if NewStore(10, "").Durable() {
		t.Fatal("in-memory audit ring must not be treated as durable")
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if !NewStore(10, path).Durable() {
		t.Fatal("writable audit file was not recognized as durable")
	}
	if NewStore(10, filepath.Join(path, "child")).Durable() {
		t.Fatal("unwritable audit path was treated as durable")
	}
}

func TestDurableTerminalOperationIDsRequiresExactValidEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	store := NewStore(10, path)
	if err := store.Append(context.Background(), Event{Action: "storage_operation_succeeded", OperationID: "storage-complete", Result: "ok"}); err != nil { t.Fatal(err) }
	if err := store.Append(context.Background(), Event{Action: "storage_operation_execution_started", OperationID: "storage-running", Result: "ok"}); err != nil { t.Fatal(err) }
	ids, err := store.DurableTerminalOperationIDs()
	if err != nil {
		t.Fatal(err)
	}
	if !ids["storage-complete"] || ids["storage-running"] {
		t.Fatalf("terminal IDs=%v", ids)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("{malformed\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DurableTerminalOperationIDs(); err == nil {
		t.Fatal("malformed durable audit stream was accepted for operation GC")
	}
}
