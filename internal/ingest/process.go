package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/marcosiw/catalog-consolidation/internal/normalize"
	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/store"
)

// process runs one valid, distinct entry through the pipeline. Data problems
// become a rejection in the result; only system errors are returned.
func (im *Importer) process(ctx context.Context, tx *store.Tx, runID int64, e Entry) (report.Entry, error) {
	// Stage 0c: content policy, on the text as the seller sent it.
	if term, hit := im.cfg.Exclusions.Match(e.Name, value(e.Brand)); hit {
		return reportEntry(e, report.OutcomeRejected, report.ReasonExclusionList, fmt.Sprintf("contains %q", term)), nil
	}
	name, brand := normalize.Text(e.Name), normalize.Brand(e.Brand)
	if name == "" {
		return reportEntry(e, report.OutcomeRejected, report.ReasonMissingName, "name has no letters or digits"), nil
	}

	// Stages 1 to 3.
	match, err := im.matchProduct(ctx, tx, name, brand)
	if err != nil {
		return report.Entry{}, err
	}
	res := reportEntry(e, "", "", "")
	res.Warnings = match.warnings
	productID := match.productID
	if productID == 0 {
		productID, err = tx.InsertProduct(ctx, store.Product{
			Name:            displayName(e.Name),
			Brand:           e.Brand,
			Category:        e.Category,
			NormalizedName:  name,
			NormalizedBrand: brand,
		})
		if err != nil {
			return report.Entry{}, err
		}
		res.NewProduct = true
	}
	res.ProductID, res.MatchMethod, res.MatchScore = productID, match.method, match.score

	if err := im.link(ctx, tx, runID, e, productID, match, &res); err != nil {
		return report.Entry{}, err
	}

	others, err := tx.OtherSellerIDsForProduct(ctx, e.SellerName, productID, e.ID)
	if err != nil {
		return report.Entry{}, err
	}
	if len(others) > 0 {
		res.Warnings = append(res.Warnings, report.Warning{
			Code:      report.WarningSameProductMultipleIDs,
			Message:   fmt.Sprintf("seller also links this product with id(s) %s", strings.Join(others, ", ")),
			ProductID: productID,
		})
	}
	return res, nil
}

// displayName is the name a new catalog product gets: the seller's text with
// surrounding and repeated spaces removed, and nothing else changed.
func displayName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func reportEntry(e Entry, outcome report.Outcome, reason, detail string) report.Entry {
	return report.Entry{
		Position:        e.Position,
		SellerName:      e.SellerName,
		SellerProductID: e.ID,
		Name:            e.Name,
		Outcome:         outcome,
		Reason:          reason,
		Detail:          detail,
	}
}
