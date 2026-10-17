package ingest

import (
	"bytes"
	"encoding/json"
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

// ParseEntries decodes the input file, a JSON array of entries. A malformed
// file is a system error: nothing can be imported from it.
func ParseEntries(data []byte) ([]Entry, error) {
	var raw []rawEntry
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse entries: %w", err)
	}
	entries := make([]Entry, len(raw))
	for i, r := range raw {
		entries[i] = Entry{
			Position:   i + 1,
			ID:         value(r.ID),
			SellerName: value(r.SellerName),
			Name:       value(r.Name),
			Brand:      r.Brand,
			Category:   r.Category,
		}
	}
	return entries, nil
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

type sellerItem struct {
	sellerName string
	id         string
}

// distinct keeps, for each (SellerName, Id), the last entry of the file: the
// most recent message of that seller about that item (SPEC decision 2). It
// returns the kept entries in file order and the superseded ones.
func distinct(entries []Entry) (kept, superseded []Entry, supersededBy map[int]int) {
	last := make(map[sellerItem]int)
	for _, e := range entries {
		last[sellerItem{e.SellerName, e.ID}] = e.Position
	}
	supersededBy = make(map[int]int)
	for _, e := range entries {
		if p := last[sellerItem{e.SellerName, e.ID}]; p != e.Position {
			superseded = append(superseded, e)
			supersededBy[e.Position] = p
			continue
		}
		kept = append(kept, e)
	}
	return kept, superseded, supersededBy
}
