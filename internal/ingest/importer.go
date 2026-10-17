package ingest

import (
	"errors"
	"fmt"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/rules"
	"github.com/marcosiw/catalog-consolidation/internal/store"
	"github.com/marcosiw/catalog-consolidation/internal/translate"
)

// Config holds the tunable parts of an import.
type Config struct {
	SimilarityThreshold float64              // minimum token Jaccard for a candidate (decision 7)
	BatchSize           int                  // entries per transaction (decision 9)
	Translator          translate.Translator // confirms candidates (stage 3)
	Exclusions          *rules.ExclusionList // content policy (stage 0c)
	Now                 func() time.Time     // clock, replaceable in tests
}

// Importer runs imports against one catalog.
type Importer struct {
	store *store.Store
	cfg   Config
}

// New validates cfg and returns an Importer.
func New(s *store.Store, cfg Config) (*Importer, error) {
	if cfg.SimilarityThreshold <= 0 || cfg.SimilarityThreshold > 1 {
		return nil, fmt.Errorf("similarity threshold must be in (0, 1], got %v", cfg.SimilarityThreshold)
	}
	if cfg.BatchSize <= 0 {
		return nil, fmt.Errorf("batch size must be positive, got %d", cfg.BatchSize)
	}
	if cfg.Translator == nil {
		return nil, errors.New("a translator is required")
	}
	if cfg.Exclusions == nil {
		cfg.Exclusions, _ = rules.NewExclusionList(nil)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Importer{store: s, cfg: cfg}, nil
}
