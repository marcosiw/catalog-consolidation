package ingest

import (
	"context"
	"fmt"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/similarity"
	"github.com/marcosiw/catalog-consolidation/internal/store"
)

// resolution is the catalog product an entry belongs to.
type resolution struct {
	productID int64 // 0 when the product must be created
	method    store.MatchMethod
	score     *float64
	warnings  []report.Warning
}

// resolve finds the catalog product of an entry (SPEC section 3):
//
//  1. the normalized key exists in the catalog → normalized;
//  2. otherwise, same-brand products whose token Jaccard reaches the
//     threshold are candidates; with no candidate → new product;
//  3. the translated name must be exactly the key of a candidate →
//     translated; otherwise → new product and a possible_duplicate warning.
//
// Similarity alone never links: two independent signals must agree.
func (im *Importer) resolve(ctx context.Context, tx *store.Tx, name, brand string) (resolution, error) {
	if p, ok, err := tx.FindProductByKey(ctx, name, brand); err != nil {
		return resolution{}, err
	} else if ok {
		return resolution{productID: p.ID, method: store.MatchNormalized, score: ptr(1.0)}, nil
	}

	// The 119 unbranded products are too heterogeneous to be a block.
	if brand == "" {
		return resolution{method: store.MatchNew}, nil
	}
	sameBrand, err := tx.ProductsByBrand(ctx, brand)
	if err != nil {
		return resolution{}, err
	}
	var (
		candidates []store.Product
		scores     []float64
	)
	for _, p := range sameBrand {
		if s := similarity.Jaccard(name, p.NormalizedName); s >= im.cfg.SimilarityThreshold {
			candidates = append(candidates, p)
			scores = append(scores, s)
		}
	}
	if len(candidates) == 0 {
		return resolution{method: store.MatchNew}, nil
	}

	translated := im.cfg.Translator.Translate(name)
	for i, c := range candidates {
		if translated == c.NormalizedName {
			return resolution{productID: c.ID, method: store.MatchTranslated, score: ptr(scores[i])}, nil
		}
	}

	best := 0
	for i := range scores {
		if scores[i] > scores[best] {
			best = i
		}
	}
	return resolution{
		method: store.MatchNew,
		warnings: []report.Warning{{
			Code: report.WarningPossibleDuplicate,
			Message: fmt.Sprintf("similar to product %d %q (score %.2f) but translation did not confirm it",
				candidates[best].ID, candidates[best].Name, scores[best]),
			ProductID: candidates[best].ID,
			Score:     ptr(scores[best]),
		}},
	}, nil
}

func ptr[T any](v T) *T { return &v }
