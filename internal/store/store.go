// Package store is the SQLite data access layer.
//
// Every statement that carries data uses ? parameters: values are sent to
// SQLite separately from the SQL text, so a value such as
// "TestBrand'; SELECT 1; --" is stored as text and never executed.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store is a migrated catalog database.
type Store struct {
	db *sql.DB
}

// Open opens the database at path and migrates it to SchemaVersion.
//
// Pragmas go in the DSN so that every pooled connection gets them:
// SQLite only enforces foreign keys when foreign_keys is ON, and it is OFF
// by default on each new connection.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Begin starts a transaction. Every read and write of an import goes through one.
func (s *Store) Begin(ctx context.Context) (*Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	return &Tx{tx: tx}, nil
}

// Tx is a transaction on the catalog.
type Tx struct {
	tx *sql.Tx
}

// Commit commits the transaction.
func (t *Tx) Commit() error { return t.tx.Commit() }

// Rollback aborts the transaction. It is safe to call after Commit.
func (t *Tx) Rollback() error {
	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}
	return nil
}

// Product is a catalog product as seen by the matching pipeline.
type Product struct {
	ID              int64
	Name            string
	Brand           *string
	Category        *string
	NormalizedName  string
	NormalizedBrand string
}

// FindProductByKey returns the product with this normalized key, if any.
func (t *Tx) FindProductByKey(ctx context.Context, normalizedName, normalizedBrand string) (Product, bool, error) {
	var (
		p               Product
		brand, category sql.NullString
	)
	err := t.tx.QueryRowContext(ctx, `
		SELECT Id, Name, Brand, Category, NormalizedName, NormalizedBrand
		FROM Product WHERE NormalizedBrand = ? AND NormalizedName = ?`,
		normalizedBrand, normalizedName,
	).Scan(&p.ID, &p.Name, &brand, &category, &p.NormalizedName, &p.NormalizedBrand)
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, false, nil
	}
	if err != nil {
		return Product{}, false, fmt.Errorf("find product by key: %w", err)
	}
	p.Brand, p.Category = nullable(brand), nullable(category)
	return p, true, nil
}

// ProductsByBrand returns every product of a normalized brand: the candidate
// block of the similarity stage.
func (t *Tx) ProductsByBrand(ctx context.Context, normalizedBrand string) ([]Product, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT Id, Name, Brand, Category, NormalizedName, NormalizedBrand
		FROM Product WHERE NormalizedBrand = ? ORDER BY Id`, normalizedBrand)
	if err != nil {
		return nil, fmt.Errorf("products by brand: %w", err)
	}
	defer rows.Close()
	var products []Product
	for rows.Next() {
		var (
			p               Product
			brand, category sql.NullString
		)
		if err := rows.Scan(&p.ID, &p.Name, &brand, &category, &p.NormalizedName, &p.NormalizedBrand); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		p.Brand, p.Category = nullable(brand), nullable(category)
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("products by brand: %w", err)
	}
	return products, nil
}

// InsertProduct adds a product to the catalog and returns its id.
func (t *Tx) InsertProduct(ctx context.Context, p Product) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO Product (Name, Brand, Category, NormalizedName, NormalizedBrand)
		VALUES (?, ?, ?, ?, ?)`,
		p.Name, p.Brand, p.Category, p.NormalizedName, p.NormalizedBrand)
	if err != nil {
		return 0, fmt.Errorf("insert product %q: %w", p.Name, err)
	}
	return res.LastInsertId()
}

// MatchMethod tells how a seller item was linked to its product.
type MatchMethod string

const (
	MatchNormalized MatchMethod = "normalized"
	MatchTranslated MatchMethod = "translated"
	MatchNew        MatchMethod = "new"
)

// Link is a SellerProduct row: a seller's item (identified by the seller's
// own id) linked to a catalog product, with the seller's last message.
type Link struct {
	ID                int64
	SellerName        string
	SellerProductID   string
	ProductID         int64
	SellerProductName string
	SellerBrand       *string
	SellerCategory    *string
	MatchMethod       MatchMethod
	MatchScore        *float64
	ImportRunID       int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// FindLink returns the link of a seller's item, if any.
func (t *Tx) FindLink(ctx context.Context, sellerName, sellerProductID string) (Link, bool, error) {
	var (
		l                  Link
		brand, category    sql.NullString
		score              sql.NullFloat64
		createdAt, updated string
	)
	err := t.tx.QueryRowContext(ctx, `
		SELECT Id, SellerName, SellerProductId, ProductId, SellerProductName, SellerBrand, SellerCategory,
		       MatchMethod, MatchScore, ImportRunId, CreatedAt, UpdatedAt
		FROM SellerProduct WHERE SellerName = ? AND SellerProductId = ?`,
		sellerName, sellerProductID,
	).Scan(&l.ID, &l.SellerName, &l.SellerProductID, &l.ProductID, &l.SellerProductName, &brand, &category,
		&l.MatchMethod, &score, &l.ImportRunID, &createdAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, false, nil
	}
	if err != nil {
		return Link{}, false, fmt.Errorf("find link: %w", err)
	}
	l.SellerBrand, l.SellerCategory = nullable(brand), nullable(category)
	if score.Valid {
		l.MatchScore = &score.Float64
	}
	if l.CreatedAt, err = parseTime(createdAt); err != nil {
		return Link{}, false, err
	}
	if l.UpdatedAt, err = parseTime(updated); err != nil {
		return Link{}, false, err
	}
	return l, true, nil
}

// InsertLink records a new seller item. The UNIQUE (SellerName,
// SellerProductId) constraint refuses a second link for the same item.
func (t *Tx) InsertLink(ctx context.Context, l Link) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO SellerProduct (SellerName, SellerProductId, ProductId, SellerProductName, SellerBrand,
		                           SellerCategory, MatchMethod, MatchScore, ImportRunId, CreatedAt, UpdatedAt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.SellerName, l.SellerProductID, l.ProductID, l.SellerProductName, l.SellerBrand,
		l.SellerCategory, string(l.MatchMethod), l.MatchScore, l.ImportRunID,
		formatTime(l.CreatedAt), formatTime(l.UpdatedAt))
	if err != nil {
		return 0, fmt.Errorf("insert link %s/%s: %w", l.SellerName, l.SellerProductID, err)
	}
	return res.LastInsertId()
}

// UpdateLink rewrites an existing link: product, seller data, match and run.
// CreatedAt is kept.
func (t *Tx) UpdateLink(ctx context.Context, l Link) error {
	_, err := t.tx.ExecContext(ctx, `
		UPDATE SellerProduct
		SET ProductId = ?, SellerProductName = ?, SellerBrand = ?, SellerCategory = ?,
		    MatchMethod = ?, MatchScore = ?, ImportRunId = ?, UpdatedAt = ?
		WHERE Id = ?`,
		l.ProductID, l.SellerProductName, l.SellerBrand, l.SellerCategory,
		string(l.MatchMethod), l.MatchScore, l.ImportRunID, formatTime(l.UpdatedAt), l.ID)
	if err != nil {
		return fmt.Errorf("update link %d: %w", l.ID, err)
	}
	return nil
}

// CountLinks returns how many seller items are linked to a product.
func (t *Tx) CountLinks(ctx context.Context, productID int64) (int, error) {
	var n int
	if err := t.tx.QueryRowContext(ctx,
		`SELECT count(*) FROM SellerProduct WHERE ProductId = ?`, productID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count links: %w", err)
	}
	return n, nil
}

// OtherSellerIDsForProduct returns the seller's other ids linked to the same
// product, excluding sellerProductID.
func (t *Tx) OtherSellerIDsForProduct(ctx context.Context, sellerName string, productID int64, sellerProductID string) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `
		SELECT SellerProductId FROM SellerProduct
		WHERE SellerName = ? AND ProductId = ? AND SellerProductId <> ?
		ORDER BY SellerProductId`, sellerName, productID, sellerProductID)
	if err != nil {
		return nil, fmt.Errorf("other seller ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan seller id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

// Timestamps are stored as RFC 3339 text in UTC: SQLite has no time type and
// this format sorts correctly as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}
