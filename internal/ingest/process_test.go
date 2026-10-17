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

func TestImporter_Run_KeepsCatalogUnchangedBySellerData(t *testing.T) {
	f := newFixture(t, catalog...)
	f.run(t, defaultConfig(t), in{"a1", "PhotoShop", "Câmera Canon EOS R6", "Canon", "Photo"})
	var name, category, sellerName, sellerCategory string
	if err := f.db.QueryRow(`SELECT p.Name, p.Category, s.SellerProductName, s.SellerCategory
		FROM SellerProduct s JOIN Product p ON p.Id = s.ProductId`).Scan(&name, &category, &sellerName, &sellerCategory); err != nil {
		t.Fatal(err)
	}
	if name != "Camera Canon EOS R6" || category != "Photography" || sellerName != "Câmera Canon EOS R6" || sellerCategory != "Photo" {
		t.Errorf("catalog = (%q, %q), seller = (%q, %q); want catalog untouched and seller data on the link",
			name, category, sellerName, sellerCategory)
	}
}

func TestImporter_Run_Validation(t *testing.T) {
	tests := []struct {
		name       string
		entry      in
		wantReason string
	}{
		{"rejects_entry_without_id", in{"", "GameStore", "Xbox Series X", "Microsoft", nil}, report.ReasonMissingID},
		{"rejects_entry_without_seller_name", in{"a1", " ", "Xbox Series X", "Microsoft", nil}, report.ReasonMissingSellerName},
		{"rejects_entry_without_name", in{"a1", "GameStore", "", "Microsoft", nil}, report.ReasonMissingName},
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

func TestImporter_Run_SellerItems(t *testing.T) {
	t.Run("keeps_last_occurrence_of_same_seller_and_id", func(t *testing.T) {
		f := newFixture(t, catalog...)
		rep := f.run(t, defaultConfig(t),
			in{"e5", "GardenStore", "Câmera Canon EOS R6", "Canon", "Photography"},
			in{"e5", "GardenStore", "Camera Canon EOS R6", "Canon", "Photo"},
		)
		first := entryAt(t, rep, 1)
		if first.Outcome != report.OutcomeDuplicateInBatch || rep.Counters.Linked != 1 {
			t.Errorf("first = %s, linked = %d; want duplicate_in_batch and 1", first.Outcome, rep.Counters.Linked)
		}
		var stored string
		f.db.QueryRow(`SELECT SellerProductName FROM SellerProduct`).Scan(&stored)
		if stored != "Camera Canon EOS R6" {
			t.Errorf("stored seller name = %q, want the last occurrence", stored)
		}
	})
	t.Run("links_same_id_from_different_sellers_separately", func(t *testing.T) {
		f := newFixture(t, catalog...)
		f.run(t, defaultConfig(t),
			in{"0011", "GardenStore", "Xbox Series X", "Microsoft", nil},
			in{"0011", "SportsHub", "Camera Canon EOS R6", "Canon", nil},
		)
		if f.linkedProduct(t, "GardenStore", "0011") != 1 || f.linkedProduct(t, "SportsHub", "0011") != 2 {
			t.Error("same id of two sellers should link two different products")
		}
	})
	t.Run("warns_when_seller_links_same_product_with_two_ids", func(t *testing.T) {
		f := newFixture(t, catalog...)
		rep := f.run(t, defaultConfig(t),
			in{"3333cccc", "CleaningWorld", "Xbox Series X", "Microsoft", nil},
			in{"cccc3333", "CleaningWorld", "Xbox  Series X", "Microsoft", nil},
		)
		if n := f.count(t, `SELECT count(*) FROM SellerProduct WHERE ProductId = 1`); n != 2 {
			t.Errorf("links to product 1 = %d, want both kept", n)
		}
		if !hasWarning(entryAt(t, rep, 2), report.WarningSameProductMultipleIDs) {
			t.Error("second id should carry a same_product_multiple_ids warning")
		}
	})
	t.Run("links_second_seller_to_product_created_earlier_in_the_file", func(t *testing.T) {
		f := newFixture(t, catalog...)
		rep := f.run(t, defaultConfig(t),
			in{"a1", "S1", "Nintendo Switch OLED", "Nintendo", nil},
			in{"b2", "S2", "Nintendo  Switch OLED", "NINTENDO", nil},
		)
		if rep.Counters.Created != 1 || rep.Counters.Linked != 1 {
			t.Errorf("counters = %+v, want 1 created and 1 linked", rep.Counters)
		}
	})
}

func TestImporter_Run_AcrossRuns(t *testing.T) {
	t.Run("changes_nothing_on_second_run_of_same_file", func(t *testing.T) {
		f := newFixture(t, catalog...)
		entries := []in{
			{"a1", "GameStore", "Xbox Series X", "Microsoft", nil},
			{"a2", "GameStore", "Nintendo Switch OLED", "Nintendo", nil},
			{"a3", "NetShop", "Roteador WiFi 6 TP-Link", "TP-Link", nil},
		}
		f.run(t, defaultConfig(t), entries...)
		var before string
		f.db.QueryRow(`SELECT group_concat(Id || UpdatedAt || ImportRunId) FROM SellerProduct`).Scan(&before)
		rep := f.run(t, defaultConfig(t), entries...)
		var after string
		f.db.QueryRow(`SELECT group_concat(Id || UpdatedAt || ImportRunId) FROM SellerProduct`).Scan(&after)
		if rep.Counters.Unchanged != 3 || before != after {
			t.Errorf("second run: unchanged = %d, links changed = %v; want 3 and false", rep.Counters.Unchanged, before != after)
		}
		if n := f.count(t, `SELECT count(*) FROM Product`); n != len(catalog)+1 {
			t.Errorf("products = %d, want %d", n, len(catalog)+1)
		}
	})
	t.Run("moves_link_when_seller_resends_id_for_another_product", func(t *testing.T) {
		f := newFixture(t, catalog...)
		f.run(t, defaultConfig(t), in{"a1", "GameStore", "Xbox Series X", "Microsoft", nil})
		rep := f.run(t, defaultConfig(t), in{"a1", "GameStore", "Camera Canon EOS R6", "Canon", nil})
		got := entryAt(t, rep, 1)
		if got.Outcome != report.OutcomeMoved || f.linkedProduct(t, "GameStore", "a1") != 2 {
			t.Errorf("Run = %s to product %d, want moved to product 2", got.Outcome, got.ProductID)
		}
		if !hasWarning(got, report.WarningOrphanedProduct) {
			t.Error("product 1 lost its only seller: want an orphaned_product warning")
		}
		if n := f.count(t, `SELECT count(*) FROM Product WHERE Id = 1`); n != 1 {
			t.Error("orphaned product must not be deleted")
		}
	})
	t.Run("refreshes_seller_data_without_moving_link", func(t *testing.T) {
		f := newFixture(t, catalog...)
		f.run(t, defaultConfig(t), in{"a1", "PhotoShop", "Câmera Canon EOS R6", "Canon", "Photo"})
		rep := f.run(t, defaultConfig(t), in{"a1", "PhotoShop", "Camera Canon EOS R6", "Canon", "Photography"})
		var name string
		f.db.QueryRow(`SELECT SellerProductName FROM SellerProduct`).Scan(&name)
		if entryAt(t, rep, 1).Outcome != report.OutcomeUnchanged || name != "Camera Canon EOS R6" {
			t.Errorf("outcome = %s, stored name = %q; want unchanged with the new seller name", entryAt(t, rep, 1).Outcome, name)
		}
	})
}
