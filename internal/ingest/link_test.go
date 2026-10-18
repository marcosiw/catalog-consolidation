package ingest

import (
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
)

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
