package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/marcosiw/catalog-consolidation/internal/normalize"
)

// migrations are applied in order, each in its own transaction. The schema
// version is kept in PRAGMA user_version, so applying them is idempotent:
// a database at version N only runs migrations N+1 onwards.
var migrations = []func(ctx context.Context, tx *sql.Tx) error{
	migrateV1,
}

// SchemaVersion is the user_version of a fully migrated database.
var SchemaVersion = len(migrations)

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this program (%d)", version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", v+1, err)
		}
		if err := migrations[v](ctx, tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		// PRAGMA does not accept parameters; v+1 is an integer we control.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("set schema version %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", v+1, err)
		}
	}
	return nil
}

// migrateV1 adapts the schema provided with the assessment (SPEC decisions 1, 5 and 6).
func migrateV1(ctx context.Context, tx *sql.Tx) error {
	// Decision 6: the normalized key lives in the catalog, guarded by a UNIQUE
	// index, so the database itself refuses duplicate products. The brand
	// comes first in the index so that the same index also serves the
	// same-brand candidate lookup of the similarity stage.
	for _, stmt := range []string{
		`ALTER TABLE Product ADD COLUMN NormalizedName TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE Product ADD COLUMN NormalizedBrand TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add normalized columns: %w", err)
		}
	}
	if err := backfillNormalizedKey(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE UNIQUE INDEX UX_Product_NormalizedKey ON Product (NormalizedBrand, NormalizedName)`); err != nil {
		return fmt.Errorf("create normalized key index (does the catalog already hold duplicates?): %w", err)
	}

	// Decision 5: one row per import, pointing at the file that was imported.
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE ImportRun (
			Id               INTEGER PRIMARY KEY AUTOINCREMENT,
			SourceFile       TEXT    NOT NULL,
			SourceSha256     TEXT    NOT NULL,
			Status           TEXT    NOT NULL CHECK (Status IN ('running', 'completed', 'failed')),
			StartedAt        TEXT    NOT NULL,
			FinishedAt       TEXT,
			Created          INTEGER NOT NULL DEFAULT 0,
			Linked           INTEGER NOT NULL DEFAULT 0,
			Moved            INTEGER NOT NULL DEFAULT 0,
			Unchanged        INTEGER NOT NULL DEFAULT 0,
			Rejected         INTEGER NOT NULL DEFAULT 0,
			DuplicateInBatch INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		return fmt.Errorf("create ImportRun: %w", err)
	}

	// Decisions 1 and 5: SQLite cannot change a column type, so SellerProduct
	// is rebuilt. It is empty in the provided database; rows could not be
	// carried over anyway, since the seller's data was never stored.
	var rows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM SellerProduct`).Scan(&rows); err != nil {
		return fmt.Errorf("count SellerProduct: %w", err)
	}
	if rows > 0 {
		return fmt.Errorf("SellerProduct has %d rows; this migration expects it empty", rows)
	}
	for _, stmt := range []string{
		`DROP TABLE SellerProduct`,
		`CREATE TABLE SellerProduct (
			Id                INTEGER PRIMARY KEY AUTOINCREMENT,
			SellerName        TEXT    NOT NULL,
			ProductId         INTEGER NOT NULL CONSTRAINT FK_Product_Id REFERENCES Product (Id),
			SellerProductId   TEXT    NOT NULL,
			SellerProductName TEXT    NOT NULL,
			SellerBrand       TEXT,
			SellerCategory    TEXT,
			MatchMethod       TEXT    NOT NULL CHECK (MatchMethod IN ('normalized', 'translated', 'new')),
			MatchScore        REAL,
			ImportRunId       INTEGER NOT NULL CONSTRAINT FK_ImportRun_Id REFERENCES ImportRun (Id),
			CreatedAt         TEXT    NOT NULL,
			UpdatedAt         TEXT    NOT NULL,
			CONSTRAINT UX_SellerProduct_SellerSku UNIQUE (SellerName, SellerProductId)
		)`,
		`CREATE INDEX IX_SellerProduct_ProductId ON SellerProduct (ProductId)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild SellerProduct: %w", err)
		}
	}
	return nil
}

// backfillNormalizedKey computes the normalized key of every existing product
// with the same function used for incoming entries.
func backfillNormalizedKey(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT Id, Name, Brand FROM Product`)
	if err != nil {
		return fmt.Errorf("read products: %w", err)
	}
	type key struct {
		id          int64
		name, brand string
	}
	var keys []key
	for rows.Next() {
		var (
			id    int64
			name  string
			brand sql.NullString
		)
		if err := rows.Scan(&id, &name, &brand); err != nil {
			rows.Close()
			return fmt.Errorf("scan product: %w", err)
		}
		keys = append(keys, key{id, normalize.Text(name), normalize.Brand(nullable(brand))})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("read products: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read products: %w", err)
	}

	update, err := tx.PrepareContext(ctx, `UPDATE Product SET NormalizedName = ?, NormalizedBrand = ? WHERE Id = ?`)
	if err != nil {
		return fmt.Errorf("prepare backfill: %w", err)
	}
	defer update.Close()
	for _, k := range keys {
		if _, err := update.ExecContext(ctx, k.name, k.brand, k.id); err != nil {
			return fmt.Errorf("backfill product %d: %w", k.id, err)
		}
	}
	return nil
}
