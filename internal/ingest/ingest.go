// Package ingest consolidates a seller file into the catalog (SPEC section 3).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/normalize"
	"github.com/marcosiw/catalog-consolidation/internal/report"
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

// Run imports src and returns its report.
//
// The file is streamed twice and never held in memory: a first pass indexes
// it (hash, and the last position of each seller item, for last-wins), a
// second pass imports it. Batches bound the size of each transaction, not
// the memory. If a batch fails, the batches already committed stay applied,
// the run is marked failed and the returned report covers what was
// committed; running the same file again is safe because links are
// idempotent and the last message wins.
func (im *Importer) Run(ctx context.Context, src Source) (*report.Report, error) {
	rep := &report.Report{
		Status:     store.RunRunning,
		SourceFile: src.Name(),
		StartedAt:  im.cfg.Now().UTC(),
	}
	idx, err := indexSource(src)
	if err != nil {
		// Nothing was written: a malformed file is refused before the run starts.
		im.finish(rep, store.RunFailed)
		return rep, err
	}
	rep.SourceSha256 = idx.sha256
	return rep, im.runBatches(ctx, rep, src, idx)
}

// triage reports an entry that is not processed: invalid, or superseded by a
// later occurrence of the same seller item (SPEC decision 2). It returns
// true when the entry must be processed.
func (im *Importer) triage(rep *report.Report, idx index, e Entry) bool {
	if reason := validate(e); reason != "" {
		rep.Add(reportEntry(e, report.OutcomeRejected, reason, ""))
		return false
	}
	if last := idx.last[sellerItem{e.SellerName, e.ID}]; last != e.Position {
		rep.Add(reportEntry(e, report.OutcomeDuplicateInBatch, "",
			fmt.Sprintf("superseded by the entry at position %d", last)))
		return false
	}
	return true
}

func (im *Importer) runBatches(ctx context.Context, rep *report.Report, src Source, idx index) (err error) {
	// A panic must not leave the run as "running": record it as failed and
	// let the panic go on to the caller, which owns the process.
	defer func() {
		if p := recover(); p != nil {
			if rep.RunID != 0 {
				im.fail(ctx, rep, fmt.Errorf("panic: %v", p))
			}
			panic(p)
		}
	}()

	tx, err := im.store.Begin(ctx)
	if err != nil {
		return err
	}
	rep.RunID, err = tx.StartRun(ctx, rep.SourceFile, rep.SourceSha256, rep.StartedAt)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		tx.Rollback()
		return err
	}

	batch := make([]Entry, 0, im.cfg.BatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := im.runBatch(ctx, rep, batch)
		batch = batch[:0]
		return err
	}
	err = stream(src, idx, func(e Entry) error {
		if !im.triage(rep, idx, e) {
			return nil
		}
		batch = append(batch, e)
		if len(batch) == im.cfg.BatchSize {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		return im.fail(ctx, rep, err)
	}

	im.finish(rep, store.RunCompleted)
	return im.updateRun(ctx, rep)
}

// runBatch processes entries in one transaction. Results reach the report
// only after the commit, so a failed batch leaves no trace in it.
func (im *Importer) runBatch(ctx context.Context, rep *report.Report, entries []Entry) error {
	tx, err := im.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	results := make([]report.Entry, 0, len(entries))
	for _, e := range entries {
		res, err := im.process(ctx, tx, rep.RunID, e)
		if err != nil {
			return fmt.Errorf("entry at position %d: %w", e.Position, err)
		}
		results = append(results, res)
	}
	committed := *rep
	for _, r := range results {
		committed.Add(r)
	}
	if err := tx.UpdateRun(ctx, rep.RunID, store.RunRunning, committed.Counters, time.Time{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}
	*rep = committed
	return nil
}

func (im *Importer) finish(rep *report.Report, status store.RunStatus) {
	rep.Status = status
	rep.FinishedAt = im.cfg.Now().UTC()
	rep.SortByPosition()
}

// fail marks the run failed, keeping the counters of the committed batches,
// and returns cause. The cause may be a cancelled context (e.g. Ctrl+C), so
// the status is written with a context that ignores the cancellation.
func (im *Importer) fail(ctx context.Context, rep *report.Report, cause error) error {
	im.finish(rep, store.RunFailed)
	if err := im.updateRun(context.WithoutCancel(ctx), rep); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (im *Importer) updateRun(ctx context.Context, rep *report.Report) error {
	tx, err := im.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.UpdateRun(ctx, rep.RunID, rep.Status, rep.Counters, rep.FinishedAt); err != nil {
		return err
	}
	return tx.Commit()
}

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
	match, err := im.resolve(ctx, tx, name, brand)
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

// link records the seller's item: a new link, nothing (same product), or a
// moved link when the seller's last message points to another product
// (SPEC decision 2).
func (im *Importer) link(ctx context.Context, tx *store.Tx, runID int64, e Entry, productID int64, match resolution, res *report.Entry) error {
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
