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
	p, found, err := tx.FindProductByKey(ctx, name, brand)
	if err != nil {
		return resolution{}, err
	}
	if found {
		return resolution{productID: p.ID, method: store.MatchNormalized, score: ptr(1.0)}, nil
	}

	// The 119 unbranded products are too heterogeneous to be a block.
	if brand == "" {
		return resolution{method: store.MatchNew}, nil
	}
	candidates, best, err := im.candidates(ctx, tx, name, brand)
	if err != nil {
		return resolution{}, err
	}
	if len(candidates) == 0 {
		return resolution{method: store.MatchNew}, nil
	}

	translated := im.cfg.Translator.Translate(name)
	for _, c := range candidates {
		if translated == c.product.NormalizedName {
			return resolution{productID: c.product.ID, method: store.MatchTranslated, score: ptr(c.score)}, nil
		}
	}

	return resolution{
		method: store.MatchNew,
		warnings: []report.Warning{{
			Code: report.WarningPossibleDuplicate,
			Message: fmt.Sprintf("similar to product %d %q (score %.2f) but translation did not confirm it",
				best.product.ID, best.product.Name, best.score),
			ProductID: best.product.ID,
			Score:     ptr(best.score),
		}},
	}, nil
}

type candidate struct {
	product store.Product
	score   float64
}

// candidates returns the same-brand products whose token Jaccard with name
// reaches the threshold, and the best of them.
func (im *Importer) candidates(ctx context.Context, tx *store.Tx, name, brand string) ([]candidate, candidate, error) {
	sameBrand, err := tx.ProductsByBrand(ctx, brand)
	if err != nil {
		return nil, candidate{}, err
	}
	var (
		found []candidate
		best  candidate
	)
	for _, p := range sameBrand {
		c := candidate{p, similarity.Jaccard(name, p.NormalizedName)}
		if c.score < im.cfg.SimilarityThreshold {
			continue
		}
		found = append(found, c)
		if c.score > best.score {
			best = c
		}
	}
	return found, best, nil
}

func ptr[T any](v T) *T { return &v }
