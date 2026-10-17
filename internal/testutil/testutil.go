// Package testutil builds databases with the schema provided with the
// assessment, so tests exercise the real migration path.
package testutil

import (
	"database/sql"
	_ "embed"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

//go:embed original_schema.sql
var originalSchema string

// Product is a catalog row to seed. Nil Brand or Category means NULL.
type Product struct {
	Name     string
	Brand    any
	Category any
}

// P is a short constructor for seed products: P("Xbox Series X", "Microsoft", nil).
func P(name string, brand, category any) Product {
	return Product{Name: name, Brand: brand, Category: category}
}

// NewOriginalDB writes a database with the original, unmigrated schema and
// the given products, in this order (ids start at 1), and returns its path.
func NewOriginalDB(t testing.TB, products ...Product) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range strings.Split(originalSchema, ";") {
		if strings.TrimSpace(stripComments(stmt)) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("original schema: %v", err)
		}
	}
	for _, p := range products {
		if _, err := db.Exec(`INSERT INTO Product (Name, Brand, Category) VALUES (?, ?, ?)`,
			p.Name, p.Brand, p.Category); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
