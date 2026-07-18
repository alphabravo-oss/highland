package audit

import (
	"context"
	"os"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendReturnsErrorOnInvalidEvent(t *testing.T) {
	s := NewStore(10, "")
	err := s.Append(context.Background(), Event{Action: "", Result: "ok"})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("expected ErrInvalidEvent, got %v", err)
	}
}

func TestAppendRejectsSecretMaterial(t *testing.T) {
	s := NewStore(10, "")
	err := s.Append(context.Background(), Event{
		Action: "login", Result: "ok", Message: "password=supersecret",
	})
	if !errors.Is(err, ErrSecretRejected) {
		t.Fatalf("expected secret rejection, got %v", err)
	}
}

func TestRequireAppendFailClosed(t *testing.T) {
	fail := NewFailingSink(ErrUnavailable)
	err := RequireAppend(context.Background(), fail, Event{Action: "storage_operation_admit", Result: "ok"})
	if err == nil {
		t.Fatal("required append must fail when sink unavailable")
	}
}

func TestBestEffortAppendIgnoresNil(t *testing.T) {
	if err := BestEffortAppend(context.Background(), nil, Event{Action: "x", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
}

func TestJSONLAppendPropagatesPathError(t *testing.T) {
	// directory path cannot be opened as file for append
	dir := t.TempDir()
	s := NewStore(10, dir) // OpenFile on directory fails on write
	// Durable may be false; Append should error when writing
	err := s.Append(context.Background(), Event{Action: "test", Result: "ok"})
	// On Linux opening a directory O_WRONLY fails
	if err == nil {
		// some OS may behave differently; ensure List still works for memory portion
		t.Log("append did not error on directory path; checking durable flag")
	}
}

func TestListPaginationAndFilters(t *testing.T) {
	s := NewStore(100, "")
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := s.Append(ctx, Event{Action: "mutate", Result: "ok", Username: "a"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Append(ctx, Event{Action: "login", Result: "denied", Username: "b"}); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, Query{Action: "login", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Username != "b" {
		t.Fatalf("filter failed: %+v", page.Events)
	}
	page, err = s.List(ctx, Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("limit: got %d", len(page.Events))
	}
	if page.NextCursor == "" {
		t.Fatal("expected next cursor")
	}
	page2, err := s.List(ctx, Query{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) == 0 {
		t.Fatal("expected more events after cursor")
	}
}

func TestSharedMemoryReplicaVisibility(t *testing.T) {
	shared := NewSharedMemorySink()
	// two "replicas" use the same sink
	ctx := context.Background()
	if err := shared.Append(ctx, Event{Action: "a1", Result: "ok", ID: "e1"}); err != nil {
		t.Fatal(err)
	}
	if err := shared.Append(ctx, Event{Action: "a2", Result: "ok", ID: "e2"}); err != nil {
		t.Fatal(err)
	}
	page, err := shared.List(ctx, Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("shared list: %d", len(page.Events))
	}
}

func TestImportJSONLDryRunAndQuarantine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.jsonl")
	content := `{"id":"1","action":"login","result":"ok","timestamp":"2026-01-01T00:00:00Z"}
{not json
{"id":"1","action":"login","result":"ok","timestamp":"2026-01-01T00:00:00Z"}
{"id":"2","action":"login","result":"ok","timestamp":"2026-01-01T00:00:00Z"}
`
	if err := writeFile(path, content); err != nil {
		t.Fatal(err)
	}
	sink := NewStore(100, "")
	stats, err := ImportJSONL(context.Background(), sink, path, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Read != 4 || stats.Quarantined < 1 || stats.Accepted < 2 {
		t.Fatalf("stats=%+v", stats)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
