package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/store"
)

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

	if err := im.startRun(ctx, rep); err != nil {
		return rep, err
	}
	if err := im.importEntries(ctx, rep, src, idx); err != nil {
		return rep, im.fail(ctx, rep, err)
	}
	im.finish(rep, store.RunCompleted)
	return rep, im.updateRun(ctx, rep)
}

func (im *Importer) startRun(ctx context.Context, rep *report.Report) error {
	return im.store.InTx(ctx, func(tx *store.Tx) error {
		id, err := tx.StartRun(ctx, rep.SourceFile, rep.SourceSha256, rep.StartedAt)
		rep.RunID = id
		return err
	})
}

// importEntries streams the file and commits its entries in batches.
func (im *Importer) importEntries(ctx context.Context, rep *report.Report, src Source, idx index) error {
	defer im.failOnPanic(ctx, rep)

	var batch []Entry
	err := stream(src, idx, func(e Entry) error {
		if !im.triage(rep, idx, e) {
			return nil
		}
		batch = append(batch, e)
		if len(batch) < im.cfg.BatchSize {
			return nil
		}
		err := im.runBatch(ctx, rep, batch)
		batch = batch[:0]
		return err
	})
	if err != nil || len(batch) == 0 {
		return err
	}
	return im.runBatch(ctx, rep, batch) // the last, partial batch
}

// runBatch processes entries in one transaction, together with the run's
// counters. Results reach the report only after the commit, so a failed
// batch leaves no trace in it.
func (im *Importer) runBatch(ctx context.Context, rep *report.Report, entries []Entry) error {
	results := make([]report.Entry, 0, len(entries))
	err := im.store.InTx(ctx, func(tx *store.Tx) error {
		for _, e := range entries {
			res, err := im.process(ctx, tx, rep.RunID, e)
			if err != nil {
				return fmt.Errorf("entry at position %d: %w", e.Position, err)
			}
			results = append(results, res)
		}
		counters := report.Tally(rep.Counters, results...)
		return tx.UpdateRun(ctx, rep.RunID, store.RunRunning, counters, time.Time{})
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		rep.Add(r)
	}
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

// failOnPanic, deferred, records the run as failed when a panic goes through
// and lets the panic go on: the caller owns the process.
func (im *Importer) failOnPanic(ctx context.Context, rep *report.Report) {
	if p := recover(); p != nil {
		im.fail(ctx, rep, fmt.Errorf("panic: %v", p))
		panic(p)
	}
}

func (im *Importer) updateRun(ctx context.Context, rep *report.Report) error {
	return im.store.InTx(ctx, func(tx *store.Tx) error {
		return tx.UpdateRun(ctx, rep.RunID, rep.Status, rep.Counters, rep.FinishedAt)
	})
}
