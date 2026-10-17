package ingest

import (
	"context"
	"fmt"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/store"
)

// link records the seller's item: a new link, nothing (same product), or a
// moved link when the seller's last message points to another product
// (SPEC decision 2).
func (im *Importer) link(ctx context.Context, tx *store.Tx, runID int64, e Entry, productID int64, match productMatch, res *report.Entry) error {
	now := im.cfg.Now().UTC()
	l := store.Link{
		SellerName:        e.SellerName,
		SellerProductID:   e.ID,
		ProductID:         productID,
		SellerProductName: e.Name,
		SellerBrand:       e.Brand,
		SellerCategory:    e.Category,
		MatchMethod:       match.method,
		MatchScore:        match.score,
		ImportRunID:       runID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	existing, found, err := tx.FindLink(ctx, e.SellerName, e.ID)
	if err != nil {
		return err
	}

	switch {
	case !found:
		if _, err := tx.InsertLink(ctx, l); err != nil {
			return err
		}
		res.Outcome = report.OutcomeLinked
		if res.NewProduct {
			res.Outcome = report.OutcomeCreated
		}

	case existing.ProductID == productID:
		// Same product: the link stays as it was made. Only the seller's
		// latest data is refreshed, and only when it changed, so running the
		// same file twice writes nothing.
		res.Outcome = report.OutcomeUnchanged
		res.MatchMethod, res.MatchScore = existing.MatchMethod, existing.MatchScore
		if sameSellerData(existing, l) {
			return nil
		}
		existing.SellerProductName, existing.SellerBrand, existing.SellerCategory = l.SellerProductName, l.SellerBrand, l.SellerCategory
		existing.ImportRunID, existing.UpdatedAt = runID, now
		return tx.UpdateLink(ctx, existing)

	default:
		l.ID, l.CreatedAt = existing.ID, existing.CreatedAt
		if err := tx.UpdateLink(ctx, l); err != nil {
			return err
		}
		res.Outcome = report.OutcomeMoved
		res.Detail = fmt.Sprintf("moved from product %d", existing.ProductID)
		left, err := tx.CountLinks(ctx, existing.ProductID)
		if err != nil {
			return err
		}
		if left == 0 {
			res.Warnings = append(res.Warnings, report.Warning{
				Code:      report.WarningOrphanedProduct,
				Message:   fmt.Sprintf("product %d has no seller left; it was not deleted", existing.ProductID),
				ProductID: existing.ProductID,
			})
		}
	}
	return nil
}

func sameSellerData(a, b store.Link) bool {
	return a.SellerProductName == b.SellerProductName &&
		equalPtr(a.SellerBrand, b.SellerBrand) &&
		equalPtr(a.SellerCategory, b.SellerCategory)
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
