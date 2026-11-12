package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

// Count is a blind cycle count of one bin: the counter scans what is on the
// shelf without seeing the book quantity. Finishing it books the difference
// per product, including products that were expected but not found.
type Count struct {
	ID          int64
	BinID       int64
	BinCode     string
	WarehouseID int64
	Status      string // open, done, cancelled
	CreatedAt   time.Time
	DoneAt      *time.Time
	Lines       []CountLine
}

// CountLine is a product counted, or expected, in the bin.
type CountLine struct {
	ProductID int64
	SKU       string
	Name      string
	Unit      string
	Counted   Qty
	Expected  Qty
}

// Diff is counted minus expected.
func (l CountLine) Diff() Qty { return l.Counted - l.Expected }

// Differences returns the lines whose count differs from the books.
func (c Count) Differences() []CountLine {
	var out []CountLine
	for _, l := range c.Lines {
		if l.Diff() != 0 {
			out = append(out, l)
		}
	}
	return out
}

// Scanned returns only the lines the counter actually scanned.
func (c Count) Scanned() []CountLine {
	var out []CountLine
	for _, l := range c.Lines {
		if l.Counted > 0 {
			out = append(out, l)
		}
	}
	return out
}

// StartCount opens a count for a bin, or returns the one already open.
func (s *Store) StartCount(ctx context.Context, binID, userID int64) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getBin(ctx, tx, binID); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT id FROM counts WHERE bin_id = ? AND status = 'open'`, binID).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO counts (bin_id, created_by, created_at) VALUES (?, ?, ?)`,
			binID, nullID(userID), unix(s.now()))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// GetCount loads a count with counted and expected quantities.
func (s *Store) GetCount(ctx context.Context, id int64) (Count, error) {
	return getCount(ctx, s.DB, id)
}

type queryRower interface {
	queryer
	rowsQueryer
}

func getCount(ctx context.Context, q queryRower, id int64) (Count, error) {
	var c Count
	var created int64
	var done sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT c.id, c.bin_id, x.code, x.warehouse_id, c.status, c.created_at, c.done_at
		FROM counts c JOIN bins x ON x.id = c.bin_id WHERE c.id = ?`, id).
		Scan(&c.ID, &c.BinID, &c.BinCode, &c.WarehouseID, &c.Status, &created, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.CreatedAt = fromUnix(created)
	c.DoneAt = fromNullUnix(done)

	rows, err := q.QueryContext(ctx, `
		SELECT p.id, p.sku, p.name, p.unit,
		       COALESCE((SELECT qty FROM count_lines l WHERE l.count_id = ? AND l.product_id = p.id), 0),
		       COALESCE((SELECT SUM(m.qty) FROM movements m WHERE m.bin_id = ? AND m.product_id = p.id), 0)
		FROM products p
		WHERE p.id IN (SELECT product_id FROM count_lines WHERE count_id = ?)
		   OR p.id IN (SELECT product_id FROM movements WHERE bin_id = ? GROUP BY product_id HAVING SUM(qty) <> 0)`,
		c.ID, c.BinID, c.ID, c.BinID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var l CountLine
		if err := rows.Scan(&l.ProductID, &l.SKU, &l.Name, &l.Unit, &l.Counted, &l.Expected); err != nil {
			return c, err
		}
		c.Lines = append(c.Lines, l)
	}
	sort.SliceStable(c.Lines, func(i, j int) bool { return c.Lines[i].Name < c.Lines[j].Name })
	return c, rows.Err()
}

// AddCount adds qty of a product to an open count (scans accumulate).
// A negative qty corrects a mistaken scan; the total cannot drop below 0.
func (s *Store) AddCount(ctx context.Context, countID, productID int64, qty Qty) (Qty, error) {
	var total Qty
	if qty == 0 {
		return 0, ErrNonPositive
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM counts WHERE id = ?`, countID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "open" {
			return ErrInvalidState
		}
		if _, err := getProduct(ctx, tx, productID); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT qty FROM count_lines WHERE count_id = ? AND product_id = ?), 0)`,
			countID, productID).Scan(&total)
		if err != nil {
			return err
		}
		total += qty
		if total < 0 {
			return ErrInvalidQty
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO count_lines (count_id, product_id, qty) VALUES (?, ?, ?)
			ON CONFLICT (count_id, product_id) DO UPDATE SET qty = excluded.qty`, countID, productID, total)
		return err
	})
	return total, err
}

// FinishCount books the differences as adjust movements and closes the count.
// It returns the number of products adjusted.
func (s *Store) FinishCount(ctx context.Context, countID, userID int64) (int, error) {
	n := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := getCount(ctx, tx, countID)
		if err != nil {
			return err
		}
		if c.Status != "open" {
			return ErrInvalidState
		}
		now := s.now()
		for _, l := range c.Differences() {
			if _, err := insertMovement(ctx, tx, mv{product: l.ProductID, bin: c.BinID, kind: MoveAdjust, qty: l.Diff(),
				note: "Hücre sayımı", count: c.ID, user: userID, at: now}); err != nil {
				return err
			}
			n++
		}
		_, err = tx.ExecContext(ctx, `UPDATE counts SET status = 'done', done_at = ? WHERE id = ?`, unix(now), c.ID)
		return err
	})
	return n, err
}

// CancelCount discards an open count without touching stock.
func (s *Store) CancelCount(ctx context.Context, countID int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE counts SET status = 'cancelled', done_at = ? WHERE id = ? AND status = 'open'`,
		unix(s.now()), countID)
	if err != nil {
		return err
	}
	if mustAffect(res) != nil {
		return ErrInvalidState
	}
	return nil
}
