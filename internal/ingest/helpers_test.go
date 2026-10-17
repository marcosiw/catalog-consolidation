package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/rules"
	"github.com/marcosiw/catalog-consolidation/internal/store"
	"github.com/marcosiw/catalog-consolidation/internal/testutil"
	"github.com/marcosiw/catalog-consolidation/internal/translate"
)

// catalog is the seed used by most tests. Ids follow the order: 1 to 7.
var catalog = []testutil.Product{
	testutil.P("Xbox Series X", "Microsoft", "Gaming"),
	testutil.P("Camera Canon EOS R6", "Canon", "Photography"),
	testutil.P("Router WiFi 6 TP-Link", "TP-Link", "Networking"),
	testutil.P("Smart Plug TP-Link 4-Pack", "TP-Link", "Smart Home"),
	testutil.P("Cable Organizer Kit", nil, "Accessories"),
	testutil.P("Galaxy S24 Ultra Black", "Samsung", "Electronics"),
	testutil.P("Belt Leather Reversible", "Levis", "Accessories"),
}

// in is an input entry; a nil field is omitted from the JSON.
type in struct {
	ID, Seller, Name string
	Brand, Category  any
}

func entriesJSON(t *testing.T, entries ...in) []byte {
	t.Helper()
	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		m := map[string]any{"Id": e.ID, "SellerName": e.Seller, "Name": e.Name, "Brand": e.Brand, "Category": e.Category}
		raw[i] = m
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fixture struct {
	path  string
	store *store.Store
	db    *sql.DB // independent connection for assertions
}

func newFixture(t *testing.T, products ...testutil.Product) *fixture {
	t.Helper()
	path := testutil.NewOriginalDB(t, products...)
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &fixture{path: path, store: s, db: db}
}

func defaultConfig(t *testing.T) Config {
	t.Helper()
	g, err := translate.NewGlossary(map[string]string{"roteador": "router", "processador": "processor"})
	if err != nil {
		t.Fatal(err)
	}
	ex, err := rules.NewExclusionList([]string{"Security Test Product", "SELECT 1;"})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	return Config{
		SimilarityThreshold: 0.6,
		BatchSize:           100,
		Translator:          g,
		Exclusions:          ex,
		Now:                 func() time.Time { clock = clock.Add(time.Second); return clock },
	}
}

func (f *fixture) run(t *testing.T, cfg Config, entries ...in) *report.Report {
	t.Helper()
	im, err := New(f.store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := im.Run(context.Background(), Bytes("entries.json", entriesJSON(t, entries...)))
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	return rep
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (f *fixture) linkedProduct(t *testing.T, seller, id string) int64 {
	t.Helper()
	var p int64
	err := f.db.QueryRow(`SELECT ProductId FROM SellerProduct WHERE SellerName = ? AND SellerProductId = ?`, seller, id).Scan(&p)
	if err != nil {
		t.Fatalf("link %s/%s: %v", seller, id, err)
	}
	return p
}

func entryAt(t *testing.T, rep *report.Report, position int) report.Entry {
	t.Helper()
	for _, e := range rep.Entries {
		if e.Position == position {
			return e
		}
	}
	t.Fatalf("no report entry at position %d", position)
	return report.Entry{}
}

func hasWarning(e report.Entry, code string) bool {
	for _, w := range e.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
