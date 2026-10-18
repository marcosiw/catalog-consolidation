package ingest

import (
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
)

func TestImporter_Run_RejectsByContent(t *testing.T) {
	tests := []struct {
		name       string
		entry      in
		wantReason string
	}{
		{"rejects_name_without_letters_or_digits", in{"a1", "GameStore", "!!!", "Microsoft", nil}, report.ReasonMissingName},
		{"rejects_name_in_exclusion_list", in{"a1", "MegaStore", "Security Test Product", "TestBrand", nil}, report.ReasonExclusionList},
		{"rejects_brand_in_exclusion_list", in{"a1", "MegaStore", "Anything", "TestBrand'; SELECT 1; --", nil}, report.ReasonExclusionList},
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

func TestImporter_Run_StoresInjectionPayloadAsData(t *testing.T) {
	f := newFixture(t, catalog...)
	cfg := defaultConfig(t)
	cfg.Exclusions = nil // only parameterization protects the database
	payload := "TestBrand'; SELECT 1; --"
	f.run(t, cfg, in{"a1", "MegaStore", "Security Test Product", payload, "Electronics"})
	if n := f.count(t, `SELECT count(*) FROM Product WHERE Brand = ?`, payload); n != 1 {
		t.Errorf("products with the literal payload as brand = %d, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM Product`); n != len(catalog)+1 {
		t.Errorf("products = %d, want %d", n, len(catalog)+1)
	}
}
