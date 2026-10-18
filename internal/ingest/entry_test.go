package ingest

import (
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
)

func TestImporter_Run_RejectsInvalidEntry(t *testing.T) {
	tests := []struct {
		name       string
		entry      in
		wantReason string
	}{
		{"rejects_entry_without_id", in{"", "GameStore", "Xbox Series X", "Microsoft", nil}, report.ReasonMissingID},
		{"rejects_entry_without_seller_name", in{"a1", " ", "Xbox Series X", "Microsoft", nil}, report.ReasonMissingSellerName},
		{"rejects_entry_without_name", in{"a1", "GameStore", "", "Microsoft", nil}, report.ReasonMissingName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, catalog...)
			rep := f.run(t, defaultConfig(t), tt.entry)
			got := entryAt(t, rep, 1)
			if got.Outcome != report.OutcomeRejected || got.Reason != tt.wantReason {
				t.Errorf("Run = (%s, %s), want (rejected, %s)", got.Outcome, got.Reason, tt.wantReason)
			}
			if n := f.count(t, `SELECT count(*) FROM SellerProduct`); n != 0 {
				t.Errorf("links = %d, want 0", n)
			}
		})
	}
}

func TestImporter_Run_AcceptsSellerIDInAnyFormat(t *testing.T) {
	f := newFixture(t, catalog...)
	rep := f.run(t, defaultConfig(t),
		in{"ddddeee-ffff-4000-1111-222233334444", "S1", "Xbox Series X", "Microsoft", nil},
		in{"12345", "S2", "Xbox Series X", "Microsoft", nil},
		in{"SKU-XBX-01", "S3", "Xbox Series X", "Microsoft", nil},
	)
	if rep.Counters.Linked != 3 {
		t.Errorf("linked = %d, want 3", rep.Counters.Linked)
	}
}
