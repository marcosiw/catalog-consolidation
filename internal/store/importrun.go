package store

import (
	"context"
	"fmt"
	"time"
)

// RunStatus is the lifecycle of an ImportRun.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

// Counters summarize what an import did.
type Counters struct {
	Created          int `json:"created"`
	Linked           int `json:"linked"`
	Moved            int `json:"moved"`
	Unchanged        int `json:"unchanged"`
	Rejected         int `json:"rejected"`
	DuplicateInBatch int `json:"duplicate_in_batch"`
}

// StartRun records a new import of sourceFile and returns its id.
func (t *Tx) StartRun(ctx context.Context, sourceFile, sourceSha256 string, startedAt time.Time) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO ImportRun (SourceFile, SourceSha256, Status, StartedAt) VALUES (?, ?, ?, ?)`,
		sourceFile, sourceSha256, string(RunRunning), formatTime(startedAt))
	if err != nil {
		return 0, fmt.Errorf("start import run: %w", err)
	}
	return res.LastInsertId()
}

// UpdateRun stores the counters of a run and, when finishedAt is not zero,
// its final status.
func (t *Tx) UpdateRun(ctx context.Context, runID int64, status RunStatus, c Counters, finishedAt time.Time) error {
	var finished any
	if !finishedAt.IsZero() {
		finished = formatTime(finishedAt)
	}
	_, err := t.tx.ExecContext(ctx, `
		UPDATE ImportRun
		SET Status = ?, FinishedAt = ?, Created = ?, Linked = ?, Moved = ?, Unchanged = ?,
		    Rejected = ?, DuplicateInBatch = ?
		WHERE Id = ?`,
		string(status), finished, c.Created, c.Linked, c.Moved, c.Unchanged,
		c.Rejected, c.DuplicateInBatch, runID)
	if err != nil {
		return fmt.Errorf("update import run %d: %w", runID, err)
	}
	return nil
}
