package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/store"
)

func TestReport_Add(t *testing.T) {
	var r Report
	for _, o := range []Outcome{
		OutcomeCreated, OutcomeLinked, OutcomeLinked, OutcomeMoved,
		OutcomeUnchanged, OutcomeRejected, OutcomeDuplicateInBatch,
	} {
		r.Add(Entry{Outcome: o})
	}
	want := store.Counters{Created: 1, Linked: 2, Moved: 1, Unchanged: 1, Rejected: 1, DuplicateInBatch: 1}
	if r.Counters != want || len(r.Entries) != 7 {
		t.Errorf("Counters = %+v with %d entries, want %+v with 7", r.Counters, len(r.Entries), want)
	}
}

func TestReport_SortByPosition_RestoresFileOrder(t *testing.T) {
	// Rejections and duplicates are added before the processed entries.
	var r Report
	for _, p := range []int{4, 1, 3, 2} {
		r.Add(Entry{Position: p})
	}
	r.SortByPosition()
	for i, e := range r.Entries {
		if e.Position != i+1 {
			t.Fatalf("positions after sort = %v, want 1 to 4", positions(r.Entries))
		}
	}
}

func positions(entries []Entry) []int {
	ps := make([]int, len(entries))
	for i, e := range entries {
		ps[i] = e.Position
	}
	return ps
}

func TestReport_WarningCounts_CountsEveryWarningByCode(t *testing.T) {
	var r Report
	r.Add(Entry{Warnings: []Warning{{Code: WarningPossibleDuplicate}, {Code: WarningSameProductMultipleIDs}}})
	r.Add(Entry{Warnings: []Warning{{Code: WarningSameProductMultipleIDs}}})
	got := r.WarningCounts()
	if got[WarningPossibleDuplicate] != 1 || got[WarningSameProductMultipleIDs] != 2 || len(got) != 2 {
		t.Errorf("WarningCounts() = %v, want 1 possible_duplicate and 2 same_product_multiple_ids", got)
	}
}

func TestReport_FileName(t *testing.T) {
	started := time.Date(2026, 10, 17, 20, 8, 40, 0, time.UTC)
	tests := []struct {
		name   string
		report Report
		want   string
	}{
		{"names_import_after_run_id", Report{RunID: 7, StartedAt: started}, "run-7.json"},
		{"names_dry_run_after_start_time", Report{RunID: 7, DryRun: true, StartedAt: started}, "dry-run-20261017T200840Z.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.FileName(); got != tt.want {
				t.Errorf("FileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The JSON report is part of the public API (see AGENTS.md, versioning):
// renaming a field is a breaking change, and this test makes it visible.
func TestReport_WriteFile_KeepsJSONFieldNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports") // does not exist yet
	r := Report{RunID: 1, Status: store.RunCompleted, SourceFile: "entries.json"}
	r.Add(Entry{
		Position: 1, SellerName: "GameStore", SellerProductID: "a1", Name: "Xbox Series X",
		Outcome: OutcomeLinked, ProductID: 1, MatchMethod: store.MatchNormalized,
		Warnings: []Warning{{Code: WarningSameProductMultipleIDs, Message: "m", ProductID: 1}},
	})
	path, err := r.WriteFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"run_id", "dry_run", "status", "source_file", "source_sha256", "started_at", "finished_at", "counters", "entries"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("report has no %q field", key)
		}
	}
	counters := doc["counters"].(map[string]any)
	for _, key := range []string{"created", "linked", "moved", "unchanged", "rejected", "duplicate_in_batch"} {
		if _, ok := counters[key]; !ok {
			t.Errorf("counters have no %q field", key)
		}
	}
	entry := doc["entries"].([]any)[0].(map[string]any)
	for _, key := range []string{"position", "seller_name", "seller_product_id", "name", "outcome", "product_id", "match_method", "warnings"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("entry has no %q field", key)
		}
	}
}

func TestReport_WriteSummary_ShowsCountersAndWarnings(t *testing.T) {
	r := Report{SourceFile: "entries.json", Status: store.RunCompleted, DryRun: true}
	r.Add(Entry{Outcome: OutcomeLinked, Warnings: []Warning{{Code: WarningPossibleDuplicate}}})
	var out bytes.Buffer
	r.WriteSummary(&out)
	for _, want := range []string{"dry run (nothing was saved)", "linked                1", "warning possible_duplicate"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary does not contain %q:\n%s", want, out.String())
		}
	}
}
