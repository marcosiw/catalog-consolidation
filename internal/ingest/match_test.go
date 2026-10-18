package ingest

import (
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/store"
)

func TestImporter_Run_Matching(t *testing.T) {
	tests := []struct {
		name        string
		entry       in
		wantOutcome report.Outcome
		wantProduct int64 // 0: a new product (id 8)
		wantMethod  store.MatchMethod
	}{
		{"links_name_with_repeated_spaces", in{"a1", "GameStore", "Xbox  Series X", "Microsoft", "Gaming"}, report.OutcomeLinked, 1, store.MatchNormalized},
		{"links_name_with_accents", in{"a1", "PhotoShop", "Câmera Canon EOS R6", "Canon", "Photography"}, report.OutcomeLinked, 2, store.MatchNormalized},
		{"ignores_category_when_matching", in{"a1", "PhotoShop", "Camera Canon EOS R6", "Canon", "Photo"}, report.OutcomeLinked, 2, store.MatchNormalized},
		{"links_brand_written_with_apostrophe", in{"a1", "SportsHub", "Belt Leather Reversible", "Levi's", "Accessories"}, report.OutcomeLinked, 7, store.MatchNormalized},
		{"links_unbranded_entry_to_unbranded_product", in{"a1", "SportsHub", "Cable Organizer Kit", nil, "Accessories"}, report.OutcomeLinked, 5, store.MatchNormalized},
		{"links_translation_confirmed_by_similarity", in{"a1", "NetShop", "Roteador WiFi 6 TP-Link", "TP-Link", "Networking"}, report.OutcomeLinked, 3, store.MatchTranslated},
		{"creates_product_for_unknown_name", in{"a1", "GameStore", "Nintendo Switch OLED", "Nintendo", "Gaming"}, report.OutcomeCreated, 0, store.MatchNew},
		{"creates_product_for_same_name_with_other_brand", in{"a1", "GameStore", "Xbox Series X", "Generic", "Gaming"}, report.OutcomeCreated, 0, store.MatchNew},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, catalog...)
			rep := f.run(t, defaultConfig(t), tt.entry)
			got := entryAt(t, rep, 1)
			want := tt.wantProduct
			if want == 0 {
				want = int64(len(catalog) + 1)
			}
			if got.Outcome != tt.wantOutcome || got.ProductID != want || got.MatchMethod != tt.wantMethod {
				t.Errorf("Run(%q) = (%s, product %d, %s), want (%s, product %d, %s)",
					tt.entry.Name, got.Outcome, got.ProductID, got.MatchMethod, tt.wantOutcome, want, tt.wantMethod)
			}
			if p := f.linkedProduct(t, tt.entry.Seller, tt.entry.ID); p != want {
				t.Errorf("stored link points to product %d, want %d", p, want)
			}
		})
	}
}

func TestImporter_Run_CreatesProductAndWarnsWhenTranslationDoesNotConfirm(t *testing.T) {
	f := newFixture(t, catalog...)
	rep := f.run(t, defaultConfig(t), in{"a1", "PhoneShop", "Galaxy S24 Ultra White", "Samsung", "Electronics"})
	got := entryAt(t, rep, 1)
	if got.Outcome != report.OutcomeCreated || !hasWarning(got, report.WarningPossibleDuplicate) {
		t.Errorf("Run = (%s, %+v), want created with a possible_duplicate warning", got.Outcome, got.Warnings)
	}
	if got.Warnings[0].ProductID != 6 {
		t.Errorf("possible_duplicate points to product %d, want 6", got.Warnings[0].ProductID)
	}
}

// "Cable Organizer Kit Pro" scores 0.75 against the unbranded "Cable
// Organizer Kit": were unbranded products a block, it would be a candidate
// and carry a possible_duplicate warning.
func TestImporter_Run_CreatesUnbrandedProductWithoutSimilarity(t *testing.T) {
	f := newFixture(t, catalog...)
	rep := f.run(t, defaultConfig(t), in{"a1", "SportsHub", "Cable Organizer Kit Pro", nil, "Accessories"})
	got := entryAt(t, rep, 1)
	if got.Outcome != report.OutcomeCreated || len(got.Warnings) != 0 {
		t.Errorf("Run = (%s, %+v), want created without warnings", got.Outcome, got.Warnings)
	}
}
