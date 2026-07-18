// Command aggregate validates and aggregates Highland qualification result JSON
// documents. It is pure Go (stdlib only) and never treats skipped/not-run as
// passing evidence for a release gate.
//
// Usage:
//
//	aggregate -dir DIR [-gate production] [-require id1,id2] [-out summary.json]
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Result states from docs/adr/0007 and Workstream C.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusFlaky   = "flaky"
	StatusSkipped = "skipped"
	StatusBlocked = "blocked"
	StatusNotRun  = "not-run"
)

var validStatuses = map[string]bool{
	StatusPassed:  true,
	StatusFailed:  true,
	StatusFlaky:   true,
	StatusSkipped: true,
	StatusBlocked: true,
	StatusNotRun:  true,
}

var validGates = map[string]bool{
	"pr": true, "nightly": true, "preview": true, "production": true,
}

// Fields that must never appear in published summaries.
var redactFieldNames = map[string]bool{
	"password":   true,
	"token":      true,
	"kubeconfig": true,
	"secret":     true,
}

// Result is a normalized qualification result document.
type Result struct {
	SchemaVersion   int                    `json:"schemaVersion"`
	RunID           string                 `json:"runId,omitempty"`
	ProfileID       string                 `json:"profileId"`
	Provider        string                 `json:"provider"`
	Gate            string                 `json:"gate"`
	Status          string                 `json:"status"`
	SourceCommit    string                 `json:"sourceCommit,omitempty"`
	ArtifactDigests map[string]string      `json:"artifactDigests,omitempty"`
	StartedAt       string                 `json:"startedAt"`
	EndedAt         string                 `json:"endedAt"`
	Environment     map[string]any         `json:"environment,omitempty"`
	Assertions      []Assertion            `json:"assertions,omitempty"`
	Cleanup         *Cleanup               `json:"cleanup,omitempty"`
	Evidence        map[string]any         `json:"evidence,omitempty"`
	Message         string                 `json:"message,omitempty"`
	SourceFile string `json:"-"`
}

// Assertion is a single QA-C4 assertion outcome.
type Assertion struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Cleanup records finalization status.
type Cleanup struct {
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// Summary is the aggregator output.
type Summary struct {
	Gate            string            `json:"gate"`
	Required        []string          `json:"required,omitempty"`
	Results         []ResultSummary   `json:"results"`
	Missing         []string          `json:"missing,omitempty"`
	NotPassed       []string          `json:"notPassed,omitempty"`
	Invalid         []string          `json:"invalid,omitempty"`
	Passed          bool              `json:"passed"`
	RejectedNonPass []string          `json:"rejectedNonPassEvidence,omitempty"`
	Message         string            `json:"message"`
	Redacted        map[string]any    `json:"redactedSample,omitempty"`
}

// ResultSummary is a concise per-profile line in the aggregate summary.
type ResultSummary struct {
	ProfileID  string `json:"profileId"`
	Provider   string `json:"provider"`
	Gate       string `json:"gate"`
	Status     string `json:"status"`
	SourceFile string `json:"sourceFile,omitempty"`
	Satisfies  bool   `json:"satisfiesGate"`
}

func main() {
	dir := flag.String("dir", "", "directory containing result JSON files")
	gate := flag.String("gate", "production", "gate to enforce (pr|nightly|preview|production)")
	require := flag.String("require", "", "comma-separated required profile IDs for the selected gate")
	out := flag.String("out", "", "optional path to write redacted summary JSON")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: aggregate -dir DIR [-gate production] [-require id1,id2] [-out summary.json]")
		os.Exit(2)
	}
	if !validGates[*gate] {
		fmt.Fprintf(os.Stderr, "invalid gate %q\n", *gate)
		os.Exit(2)
	}

	var required []string
	if strings.TrimSpace(*require) != "" {
		for _, p := range strings.Split(*require, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				required = append(required, p)
			}
		}
	}

	summary, err := Aggregate(*dir, *gate, required)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aggregate error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(summary)

	if *out != "" {
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal summary: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write summary: %v\n", err)
			os.Exit(1)
		}
	}

	if !summary.Passed {
		os.Exit(1)
	}
}

// Aggregate loads result JSON files from dir and evaluates gate coverage.
func Aggregate(dir, gate string, required []string) (*Summary, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		// Still fail closed when production requires profiles.
		sum := &Summary{
			Gate:     gate,
			Required: append([]string(nil), required...),
			Passed:   len(required) == 0,
			Message:  "no result files found",
		}
		if len(required) > 0 {
			sum.Missing = append([]string(nil), required...)
			sum.Passed = false
			sum.Message = "no result files found; required production profiles missing"
		}
		return sum, nil
	}
	sort.Strings(files)

	var (
		results   []Result
		invalid   []string
		byProfile = map[string]Result{}
	)

	for _, f := range files {
		r, err := LoadResultFile(f)
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("%s: %v", filepath.Base(f), err))
			continue
		}
		results = append(results, *r)
		// Last file wins if duplicates; record both in summary list.
		byProfile[r.ProfileID] = *r
	}

	sum := &Summary{
		Gate:     gate,
		Required: append([]string(nil), required...),
		Invalid:  invalid,
	}

	for _, r := range results {
		satisfies := SatisfiesGate(r.Status, gate, r.Gate)
		sum.Results = append(sum.Results, ResultSummary{
			ProfileID:  r.ProfileID,
			Provider:   r.Provider,
			Gate:       r.Gate,
			Status:     r.Status,
			SourceFile: r.SourceFile,
			Satisfies:  satisfies,
		})
		if !satisfies && (r.Status == StatusSkipped || r.Status == StatusNotRun) {
			sum.RejectedNonPass = append(sum.RejectedNonPass,
				fmt.Sprintf("%s: status %q cannot satisfy gate %q", r.ProfileID, r.Status, gate))
		}
	}

	// Enforce required profiles (typical for production).
	for _, id := range required {
		r, ok := byProfile[id]
		if !ok {
			sum.Missing = append(sum.Missing, id)
			continue
		}
		if !SatisfiesGate(r.Status, gate, r.Gate) {
			sum.NotPassed = append(sum.NotPassed, fmt.Sprintf("%s:%s", id, r.Status))
		}
	}

	// Any invalid document fails the aggregate.
	sum.Passed = len(invalid) == 0 && len(sum.Missing) == 0 && len(sum.NotPassed) == 0

	// When requiring production coverage, also reject using skipped/not-run as pass.
	if gate == "production" || len(required) > 0 {
		for _, r := range results {
			if isRequired(required, r.ProfileID) && (r.Status == StatusSkipped || r.Status == StatusNotRun || r.Status == StatusBlocked || r.Status == StatusFlaky || r.Status == StatusFailed) {
				// already in NotPassed if required; ensure fail closed
				sum.Passed = false
			}
		}
	}

	switch {
	case !sum.Passed && len(sum.Missing) > 0:
		sum.Message = "required profiles missing or not present in result set"
	case !sum.Passed && len(sum.NotPassed) > 0:
		sum.Message = "required profiles did not pass (skipped/not-run/failed/flaky/blocked are not passing evidence)"
	case !sum.Passed && len(invalid) > 0:
		sum.Message = "one or more result documents failed schema field validation"
	case len(results) == 0:
		sum.Message = "no valid results"
		if len(required) > 0 {
			sum.Passed = false
		}
	default:
		if sum.Passed {
			sum.Message = "qualification aggregate passed"
		} else {
			sum.Message = "qualification aggregate failed"
		}
	}

	// Include a redacted sample of the first result for audit (no secrets).
	if len(results) > 0 {
		raw, err := os.ReadFile(filepath.Join(dir, results[0].SourceFile))
		if err == nil {
			var anyDoc any
			if json.Unmarshal(raw, &anyDoc) == nil {
				if m, ok := RedactValue(anyDoc).(map[string]any); ok {
					sum.Redacted = m
				}
			}
		}
	}

	return sum, nil
}

func isRequired(required []string, id string) bool {
	for _, r := range required {
		if r == id {
			return true
		}
	}
	return false
}

// SatisfiesGate reports whether a result status may satisfy the enforcement gate.
// skipped and not-run never satisfy any gate. flaky/failed/blocked never pass.
// Only status=passed can satisfy, and the result's declared gate must be at least
// as strict as the enforcement gate when ranking is applied.
func SatisfiesGate(status, enforceGate, resultGate string) bool {
	if status != StatusPassed {
		return false
	}
	// Result documents tagged for a weaker gate cannot satisfy a stronger enforce gate.
	return gateRank(resultGate) >= gateRank(enforceGate)
}

func gateRank(g string) int {
	switch g {
	case "pr":
		return 1
	case "nightly":
		return 2
	case "preview":
		return 3
	case "production":
		return 4
	default:
		return 0
	}
}

// LoadResultFile reads and validates a result JSON document.
func LoadResultFile(path string) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Redact before parsing into structured form so sensitive keys never linger
	// in intermediate maps used by callers that re-marshal raw docs.
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	redacted := RedactValue(raw)
	clean, err := json.Marshal(redacted)
	if err != nil {
		return nil, err
	}

	var r Result
	if err := json.Unmarshal(clean, &r); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	r.SourceFile = filepath.Base(path)
	if err := ValidateResult(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ValidateResult checks required schema fields without external JSON Schema deps.
func ValidateResult(r *Result) error {
	var errs []string
	if r.SchemaVersion != 1 {
		errs = append(errs, fmt.Sprintf("schemaVersion must be 1, got %d", r.SchemaVersion))
	}
	if strings.TrimSpace(r.ProfileID) == "" {
		errs = append(errs, "profileId is required")
	}
	if strings.TrimSpace(r.Provider) == "" {
		errs = append(errs, "provider is required")
	}
	if !validGates[r.Gate] {
		errs = append(errs, fmt.Sprintf("invalid gate %q", r.Gate))
	}
	if !validStatuses[r.Status] {
		errs = append(errs, fmt.Sprintf("invalid status %q", r.Status))
	}
	if strings.TrimSpace(r.StartedAt) == "" {
		errs = append(errs, "startedAt is required")
	} else if _, err := time.Parse(time.RFC3339, r.StartedAt); err != nil {
		// also accept RFC3339Nano
		if _, err2 := time.Parse(time.RFC3339Nano, r.StartedAt); err2 != nil {
			errs = append(errs, "startedAt must be RFC3339")
		}
	}
	if strings.TrimSpace(r.EndedAt) == "" {
		errs = append(errs, "endedAt is required")
	} else if _, err := time.Parse(time.RFC3339, r.EndedAt); err != nil {
		if _, err2 := time.Parse(time.RFC3339Nano, r.EndedAt); err2 != nil {
			errs = append(errs, "endedAt must be RFC3339")
		}
	}
	for i, a := range r.Assertions {
		if a.ID == "" {
			errs = append(errs, fmt.Sprintf("assertions[%d].id is required", i))
		}
		if a.Status == "" {
			errs = append(errs, fmt.Sprintf("assertions[%d].status is required", i))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// RedactValue recursively replaces values of sensitive field names.
func RedactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if redactFieldNames[strings.ToLower(k)] {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = RedactValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = RedactValue(val)
		}
		return out
	default:
		return v
	}
}

// RedactJSON reads JSON from r and writes a redacted document to w.
func RedactJSON(r io.Reader, w io.Writer) error {
	var doc any
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(RedactValue(doc))
}
