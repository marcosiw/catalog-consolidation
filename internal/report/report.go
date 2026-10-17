// Package report describes what an import did, entry by entry.
//
// The report is written as JSON next to the run (SPEC section 5) and is not
// stored in the database: to audit or re-process, the file is run again.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/store"
)

// Outcome is what happened to one entry of the input file.
type Outcome string

const (
	OutcomeCreated          Outcome = "created"            // new product and new link
	OutcomeLinked           Outcome = "linked"             // existing product, new link
	OutcomeMoved            Outcome = "moved"              // existing link now points to another product
	OutcomeUnchanged        Outcome = "unchanged"          // existing link, same product
	OutcomeRejected         Outcome = "rejected"           // not imported, see Reason
	OutcomeDuplicateInBatch Outcome = "duplicate_in_batch" // superseded by a later entry with the same seller and id
)

// Rejection reasons.
const (
	ReasonMissingID         = "missing_id"
	ReasonMissingSellerName = "missing_seller_name"
	ReasonMissingName       = "missing_name"
	ReasonExclusionList     = "exclusion_list"
)

// Warning codes. A warning never blocks an entry; it flags it for a human.
const (
	WarningPossibleDuplicate      = "possible_duplicate"
	WarningSameProductMultipleIDs = "same_product_multiple_ids"
	WarningOrphanedProduct        = "orphaned_product"
)

// Warning flags an entry for review.
type Warning struct {
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	ProductID int64    `json:"product_id,omitempty"`
	Score     *float64 `json:"score,omitempty"`
}

// Entry is the result of one entry, identified by its 1-based position in the
// input array.
type Entry struct {
	Position        int               `json:"position"`
	SellerName      string            `json:"seller_name"`
	SellerProductID string            `json:"seller_product_id"`
	Name            string            `json:"name"`
	Outcome         Outcome           `json:"outcome"`
	Reason          string            `json:"reason,omitempty"`
	Detail          string            `json:"detail,omitempty"`
	ProductID       int64             `json:"product_id,omitempty"`
	NewProduct      bool              `json:"new_product,omitempty"`
	MatchMethod     store.MatchMethod `json:"match_method,omitempty"`
	MatchScore      *float64          `json:"match_score,omitempty"`
	Warnings        []Warning         `json:"warnings,omitempty"`
}

// Report is the result of one import.
type Report struct {
	RunID        int64           `json:"run_id"`
	Status       store.RunStatus `json:"status"`
	SourceFile   string          `json:"source_file"`
	SourceSha256 string          `json:"source_sha256"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
	Counters     store.Counters  `json:"counters"`
	Entries      []Entry         `json:"entries"`
}

// Add records an entry result and updates the counters.
func (r *Report) Add(e Entry) {
	r.Counters = Tally(r.Counters, e)
	r.Entries = append(r.Entries, e)
}

// Tally returns c plus the outcomes of entries, without recording them.
func Tally(c store.Counters, entries ...Entry) store.Counters {
	for _, e := range entries {
		switch e.Outcome {
		case OutcomeCreated:
			c.Created++
		case OutcomeLinked:
			c.Linked++
		case OutcomeMoved:
			c.Moved++
		case OutcomeUnchanged:
			c.Unchanged++
		case OutcomeRejected:
			c.Rejected++
		case OutcomeDuplicateInBatch:
			c.DuplicateInBatch++
		}
	}
	return c
}

// SortByPosition orders entries as they appear in the input file.
func (r *Report) SortByPosition() {
	sort.SliceStable(r.Entries, func(i, j int) bool { return r.Entries[i].Position < r.Entries[j].Position })
}

// WarningCounts returns how many warnings of each code the report holds.
func (r *Report) WarningCounts() map[string]int {
	counts := make(map[string]int)
	for _, e := range r.Entries {
		for _, w := range e.Warnings {
			counts[w.Code]++
		}
	}
	return counts
}

// FileName is run-<id>.json, after the ImportRun it describes.
func (r *Report) FileName() string {
	return fmt.Sprintf("run-%d.json", r.RunID)
}

// WriteFile writes the report as indented JSON into dir and returns its path.
func (r *Report) WriteFile(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode report: %w", err)
	}
	path := filepath.Join(dir, r.FileName())
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}

// WriteSummary prints the counters and warning totals for a person.
func (r *Report) WriteSummary(w io.Writer) {
	c := r.Counters
	fmt.Fprintf(w, "import of %s: %s\n", r.SourceFile, r.Status)
	fmt.Fprintf(w, "  created            %4d  new product and link\n", c.Created)
	fmt.Fprintf(w, "  linked             %4d  existing product, new link\n", c.Linked)
	fmt.Fprintf(w, "  moved              %4d  link moved to another product\n", c.Moved)
	fmt.Fprintf(w, "  unchanged          %4d  already linked to the same product\n", c.Unchanged)
	fmt.Fprintf(w, "  rejected           %4d  not imported\n", c.Rejected)
	fmt.Fprintf(w, "  duplicate_in_batch %4d  superseded by a later entry in the file\n", c.DuplicateInBatch)
	counts := r.WarningCounts()
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		fmt.Fprintf(w, "  warning %-26s %4d\n", code, counts[code])
	}
}
