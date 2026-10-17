package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/rules"
	"github.com/marcosiw/catalog-consolidation/internal/store"
	"github.com/marcosiw/catalog-consolidation/internal/translate"
)

// Runs only when the real data was downloaded with `make data`. It pins the
// numbers of SPEC section 2 on the provided files.
func TestImporter_Run_ProvidedData(t *testing.T) {
	root := filepath.Join("..", "..")
	db, errDB := os.ReadFile(filepath.Join(root, "data", "catalog.db"))
	entries, errEntries := os.ReadFile(filepath.Join(root, "data", "ProductEntry.json"))
	if errDB != nil || errEntries != nil {
		t.Skip("real data not available; run make data")
	}
	path := filepath.Join(t.TempDir(), "catalog.db")
	if err := os.WriteFile(path, db, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	glossary, err := translate.LoadGlossary(filepath.Join(root, "config", "glossary-pt-en.json"))
	if err != nil {
		t.Fatal(err)
	}
	exclusions, err := rules.LoadExclusionList(filepath.Join(root, "config", "exclusion-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	im, err := New(s, Config{SimilarityThreshold: 0.6, BatchSize: 500, Translator: glossary, Exclusions: exclusions})
	if err != nil {
		t.Fatal(err)
	}
	first, err := im.Run(context.Background(), "ProductEntry.json", entries)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Counters{Linked: 267, Rejected: 1, DuplicateInBatch: 1}
	if first.Counters != want {
		t.Errorf("first run counters = %+v, want %+v", first.Counters, want)
	}
	translated := 0
	for _, e := range first.Entries {
		if e.MatchMethod == store.MatchTranslated {
			translated++
		}
	}
	if translated != 2 {
		t.Errorf("translated matches = %d, want 2 (Roteador, Processador)", translated)
	}
	if n := first.WarningCounts()[report.WarningSameProductMultipleIDs]; n != 11 {
		t.Errorf("same_product_multiple_ids warnings = %d, want 11", n)
	}

	second, err := im.Run(context.Background(), "ProductEntry.json", entries)
	if err != nil {
		t.Fatal(err)
	}
	want = store.Counters{Unchanged: 267, Rejected: 1, DuplicateInBatch: 1}
	if second.Counters != want {
		t.Errorf("second run counters = %+v, want %+v", second.Counters, want)
	}
}
