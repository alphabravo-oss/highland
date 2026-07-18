package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ImportStats reports JSONL import outcomes.
type ImportStats struct {
	Read        int `json:"read"`
	Accepted    int `json:"accepted"`
	Duplicate   int `json:"duplicate"`
	Quarantined int `json:"quarantined"`
	Failed      int `json:"failed"`
}

// ImportJSONL streams events from path into sink. dryRun validates only.
// checkpointPath when non-empty stores the last successfully processed byte offset.
func ImportJSONL(ctx context.Context, sink Sink, path string, dryRun bool, checkpointPath string) (ImportStats, error) {
	var stats ImportStats
	f, err := os.Open(path)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	var start int64
	if checkpointPath != "" {
		if raw, err := os.ReadFile(checkpointPath); err == nil {
			var off int64
			if _, scanErr := fmt.Sscan(string(raw), &off); scanErr == nil && off > 0 {
				start = off
			}
		}
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return stats, err
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	var offset = start
	seen := map[string]bool{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		line := scanner.Bytes()
		offset += int64(len(line)) + 1
		stats.Read++
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			stats.Quarantined++
			continue
		}
		if err := ValidateEvent(e); err != nil {
			stats.Quarantined++
			continue
		}
		if e.ID != "" && seen[e.ID] {
			stats.Duplicate++
			continue
		}
		if e.ID != "" {
			seen[e.ID] = true
		}
		if dryRun {
			stats.Accepted++
			continue
		}
		if err := sink.Append(ctx, e); err != nil {
			if err == ErrDuplicateEvent {
				stats.Duplicate++
				continue
			}
			stats.Failed++
			return stats, err
		}
		stats.Accepted++
		if checkpointPath != "" {
			_ = os.WriteFile(checkpointPath, []byte(fmt.Sprintf("%d\n", offset)), 0o600)
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}
	return stats, nil
}
