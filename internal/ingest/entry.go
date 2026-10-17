package ingest

import (
	"fmt"
	"strings"

	"github.com/marcosiw/catalog-consolidation/internal/report"
)

// Entry is one item sent by a seller, as found in the input file.
type Entry struct {
	Position   int // 1-based position in the input array
	ID         string
	SellerName string
	Name       string
	Brand      *string
	Category   *string
}

type rawEntry struct {
	ID         *string `json:"Id"`
	SellerName *string `json:"SellerName"`
	Name       *string `json:"Name"`
	Brand      *string `json:"Brand"`
	Category   *string `json:"Category"`
}

func (r rawEntry) entry(position int) Entry {
	return Entry{
		Position:   position,
		ID:         value(r.ID),
		SellerName: value(r.SellerName),
		Name:       value(r.Name),
		Brand:      r.Brand,
		Category:   r.Category,
	}
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// validate returns the rejection reason of an entry that cannot be imported,
// or "". Only what the link needs is required: the seller id is an opaque
// string and its format is not checked (SPEC decision 3); the brand may be
// missing.
func validate(e Entry) string {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return report.ReasonMissingID
	case strings.TrimSpace(e.SellerName) == "":
		return report.ReasonMissingSellerName
	case strings.TrimSpace(e.Name) == "":
		return report.ReasonMissingName
	}
	return ""
}

// triage reports an entry that is not processed: invalid, or superseded by a
// later occurrence of the same seller item (SPEC decision 2). It returns
// true when the entry must be processed.
func (im *Importer) triage(rep *report.Report, idx index, e Entry) bool {
	if reason := validate(e); reason != "" {
		rep.Add(reportEntry(e, report.OutcomeRejected, reason, ""))
		return false
	}
	if last := idx.last[sellerItem{e.SellerName, e.ID}]; last != e.Position {
		rep.Add(reportEntry(e, report.OutcomeDuplicateInBatch, "",
			fmt.Sprintf("superseded by the entry at position %d", last)))
		return false
	}
	return true
}
