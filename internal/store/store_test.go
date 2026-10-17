package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/testutil"
)

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q) = %v", path, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func begin(t *testing.T, s *Store) *Tx {
	t.Helper()
	tx, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func startRun(t *testing.T, tx *Tx) int64 {
	t.Helper()
	id, err := tx.StartRun(context.Background(), "entries.json", "sha", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func TestOpen_MigratesOriginalSchema(t *testing.T) {
	path := testutil.NewOriginalDB(t,
		testutil.P("Tablet iPad Pro 12.9\"", "Apple", "Tablets"),
		testutil.P("Cable Organizer Kit", nil, "Accessories"),
	)
	s := openStore(t, path)
	ctx := context.Background()

	t.Run("sets_schema_version", func(t *testing.T) {
		var v int
		if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != SchemaVersion {
			t.Errorf("user_version = %d, want %d", v, SchemaVersion)
		}
	})
	t.Run("backfills_normalized_key_of_existing_products", func(t *testing.T) {
		tx := begin(t, s)
		p, ok, err := tx.FindProductByKey(ctx, "tablet ipad pro 12 9", "apple")
		if err != nil || !ok {
			t.Fatalf("FindProductByKey = (%v, %v, %v), want the iPad", p, ok, err)
		}
	})
	t.Run("stores_null_brand_as_empty_normalized_brand", func(t *testing.T) {
		tx := begin(t, s)
		p, ok, err := tx.FindProductByKey(ctx, "cable organizer kit", "")
		if err != nil || !ok {
			t.Fatalf("FindProductByKey = (%v, %v, %v), want the cable organizer", p, ok, err)
		}
		if p.Brand != nil {
			t.Errorf("Brand = %q, want NULL kept in the catalog", *p.Brand)
		}
	})
	t.Run("stores_seller_product_id_as_text", func(t *testing.T) {
		var typ string
		if err := s.db.QueryRowContext(ctx,
			`SELECT type FROM pragma_table_info('SellerProduct') WHERE name = 'SellerProductId'`).Scan(&typ); err != nil {
			t.Fatal(err)
		}
		if typ != "TEXT" {
			t.Errorf("SellerProductId type = %q, want TEXT", typ)
		}
	})
}

func TestOpen_IsIdempotent(t *testing.T) {
	path := testutil.NewOriginalDB(t, testutil.P("Xbox Series X", "Microsoft", "Gaming"))
	openStore(t, path).Close()
	s := openStore(t, path)
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM Product`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("products after reopening = %d, want 1", n)
	}
}

func TestOpen_RefusesCatalogWithDuplicateNormalizedKeys(t *testing.T) {
	path := testutil.NewOriginalDB(t,
		testutil.P("Xbox Series X", "Microsoft", "Gaming"),
		testutil.P("Xbox  Series X", "MICROSOFT", "Gaming"),
	)
	if _, err := Open(context.Background(), path); err == nil {
		t.Error("Open succeeded, want an error for duplicate products in the catalog")
	}
}

func TestOpen_EnforcesForeignKeys(t *testing.T) {
	s := openStore(t, testutil.NewOriginalDB(t))
	tx := begin(t, s)
	now := time.Now()
	runID := startRun(t, tx)
	_, err := tx.InsertLink(context.Background(), Link{
		SellerName: "S", SellerProductID: "1", ProductID: 999, SellerProductName: "x",
		MatchMethod: MatchNew, ImportRunID: runID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Error("InsertLink to a missing product succeeded, want a foreign key error")
	}
}

func TestTx_InsertProduct(t *testing.T) {
	ctx := context.Background()

	t.Run("stores_injection_payload_literally", func(t *testing.T) {
		s := openStore(t, testutil.NewOriginalDB(t, testutil.P("Xbox Series X", "Microsoft", "Gaming")))
		tx := begin(t, s)
		brand := "TestBrand'; SELECT 1; --"
		id, err := tx.InsertProduct(ctx, Product{
			Name: "Security Test Product", Brand: &brand, Category: ptr("Electronics"),
			NormalizedName: "security test product", NormalizedBrand: "testbrand select 1",
		})
		if err != nil {
			t.Fatal(err)
		}
		var stored string
		var count int
		if err := tx.tx.QueryRowContext(ctx, `SELECT Brand FROM Product WHERE Id = ?`, id).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM Product`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if stored != brand || count != 2 {
			t.Errorf("stored brand = %q with %d products, want %q with 2", stored, count, brand)
		}
	})
	t.Run("rejects_duplicate_normalized_key", func(t *testing.T) {
		s := openStore(t, testutil.NewOriginalDB(t, testutil.P("Xbox Series X", "Microsoft", "Gaming")))
		tx := begin(t, s)
		_, err := tx.InsertProduct(ctx, Product{
			Name: "XBOX Series X", Brand: ptr("microsoft"),
			NormalizedName: "xbox series x", NormalizedBrand: "microsoft",
		})
		if err == nil {
			t.Error("InsertProduct of a duplicate succeeded, want a UNIQUE error")
		}
	})
}

func TestTx_ProductsByBrand_ReturnsOnlyThatBrand(t *testing.T) {
	s := openStore(t, testutil.NewOriginalDB(t,
		testutil.P("Router WiFi 6 TP-Link", "TP-Link", "Networking"),
		testutil.P("Smart Plug TP-Link 4-Pack", "TP-Link", "Smart Home"),
		testutil.P("Processor AMD Ryzen 9 7950X", "AMD", "Components"),
	))
	tx := begin(t, s)
	got, err := tx.ProductsByBrand(context.Background(), "tp link")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].NormalizedName != "router wifi 6 tp link" {
		t.Errorf("ProductsByBrand(%q) = %+v, want the 2 TP-Link products", "tp link", got)
	}
}

func TestTx_Links(t *testing.T) {
	ctx := context.Background()
	newLink := func(seller, id string, productID, runID int64) Link {
		now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
		return Link{
			SellerName: seller, SellerProductID: id, ProductID: productID,
			SellerProductName: "Xbox Series X", SellerBrand: ptr("Microsoft"),
			MatchMethod: MatchNormalized, MatchScore: ptr(1.0), ImportRunID: runID,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	setup := func(t *testing.T) (*Tx, int64) {
		s := openStore(t, testutil.NewOriginalDB(t,
			testutil.P("Xbox Series X", "Microsoft", "Gaming"),
			testutil.P("Nintendo Switch OLED", "Nintendo", "Gaming"),
		))
		tx := begin(t, s)
		return tx, startRun(t, tx)
	}

	t.Run("finds_inserted_link_with_seller_data", func(t *testing.T) {
		tx, run := setup(t)
		want := newLink("GameStore", "a1b2", 1, run)
		if _, err := tx.InsertLink(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, ok, err := tx.FindLink(ctx, "GameStore", "a1b2")
		if err != nil || !ok {
			t.Fatalf("FindLink = (%v, %v)", ok, err)
		}
		if got.ProductID != 1 || *got.SellerBrand != "Microsoft" || *got.MatchScore != 1 || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("FindLink = %+v, want %+v", got, want)
		}
	})
	t.Run("rejects_second_link_for_same_seller_and_id", func(t *testing.T) {
		tx, run := setup(t)
		if _, err := tx.InsertLink(ctx, newLink("GameStore", "a1b2", 1, run)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.InsertLink(ctx, newLink("GameStore", "a1b2", 2, run)); err == nil {
			t.Error("second InsertLink succeeded, want a UNIQUE error")
		}
	})
	t.Run("allows_same_id_for_different_sellers", func(t *testing.T) {
		tx, run := setup(t)
		if _, err := tx.InsertLink(ctx, newLink("GameStore", "a1b2", 1, run)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.InsertLink(ctx, newLink("SportsHub", "a1b2", 2, run)); err != nil {
			t.Errorf("InsertLink for another seller = %v, want success", err)
		}
	})
	t.Run("moves_link_to_another_product", func(t *testing.T) {
		tx, run := setup(t)
		l := newLink("GameStore", "a1b2", 1, run)
		var err error
		if l.ID, err = tx.InsertLink(ctx, l); err != nil {
			t.Fatal(err)
		}
		l.ProductID = 2
		if err := tx.UpdateLink(ctx, l); err != nil {
			t.Fatal(err)
		}
		before, _ := tx.CountLinks(ctx, 1)
		after, _ := tx.CountLinks(ctx, 2)
		if before != 0 || after != 1 {
			t.Errorf("links of product 1 and 2 = %d and %d, want 0 and 1", before, after)
		}
	})
	t.Run("lists_other_ids_of_the_seller_for_a_product", func(t *testing.T) {
		tx, run := setup(t)
		for _, id := range []string{"a1b2", "b2a1"} {
			if _, err := tx.InsertLink(ctx, newLink("GameStore", id, 1, run)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := tx.OtherSellerIDsForProduct(ctx, "GameStore", 1, "a1b2")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != "b2a1" {
			t.Errorf("OtherSellerIDsForProduct = %q, want [b2a1]", got)
		}
	})
}

func TestTx_UpdateRun_StoresStatusAndCounters(t *testing.T) {
	s := openStore(t, testutil.NewOriginalDB(t))
	tx := begin(t, s)
	ctx := context.Background()
	run := startRun(t, tx)
	if err := tx.UpdateRun(ctx, run, RunCompleted, Counters{Created: 2, Linked: 3}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var status string
	var created, linked int
	var finished sql.NullString
	if err := tx.tx.QueryRowContext(ctx, `SELECT Status, Created, Linked, FinishedAt FROM ImportRun WHERE Id = ?`, run).
		Scan(&status, &created, &linked, &finished); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || created != 2 || linked != 3 || !finished.Valid {
		t.Errorf("ImportRun = (%s, %d, %d, %v), want (completed, 2, 3, set)", status, created, linked, finished)
	}
}

// Runs only when the real data was downloaded with `make data`.
func TestOpen_MigratesProvidedCatalog(t *testing.T) {
	src := filepath.Join("..", "..", "data", "catalog.db")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("real data not available (%v); run make data", err)
	}
	path := filepath.Join(t.TempDir(), "catalog.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, path)
	var products, keys int
	if err := s.db.QueryRow(`SELECT count(*), count(DISTINCT NormalizedBrand || '|' || NormalizedName) FROM Product`).
		Scan(&products, &keys); err != nil {
		t.Fatal(err)
	}
	if products != 975 || keys != 975 {
		t.Errorf("products = %d with %d distinct keys, want 975 and 975", products, keys)
	}
}
