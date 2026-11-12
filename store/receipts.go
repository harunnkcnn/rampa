package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Receipt is a goods-in document (mal kabul), usually one supplier delivery
// note. Every scanned line is booked into stock immediately.
type Receipt struct {
	ID            int64
	Code          string
	WarehouseID   int64
	WarehouseCode string
	Supplier      string
	DocNo         string
	Status        string // open, closed
	CreatedAt     time.Time
	ClosedAt      *time.Time
	Lines         int
	Units         Qty
}

// Open reports whether lines may still be added.
func (r Receipt) Open() bool { return r.Status == "open" }

// ReceiptLine is one product received, summed per bin.
type ReceiptLine struct {
	ProductID int64
	SKU       string
	Name      string
	Unit      string
	BinCode   string
	Qty       Qty
}

// CreateReceipt opens a goods-in document coded like KBL-2026-0007.
func (s *Store) CreateReceipt(ctx context.Context, warehouseID int64, supplier, docNo string, userID int64) (int64, error) {
	supplier, docNo = strings.TrimSpace(supplier), strings.ToUpper(strings.TrimSpace(docNo))
	if supplier == "" {
		return 0, ErrMissingRequired
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		res, err := tx.ExecContext(ctx, `
			INSERT INTO receipts (warehouse_id, supplier, doc_no, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			warehouseID, supplier, docNo, nullID(userID), unix(now))
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE receipts SET code = ? WHERE id = ?`, fmt.Sprintf("KBL-%d-%04d", now.Year(), id), id)
		return err
	})
	return id, err
}

const receiptCols = `r.id, r.code, r.warehouse_id, w.code, r.supplier, r.doc_no, r.status, r.created_at, r.closed_at,
	(SELECT COUNT(DISTINCT product_id) FROM movements m WHERE m.receipt_id = r.id),
	(SELECT COALESCE(SUM(qty), 0) FROM movements m WHERE m.receipt_id = r.id)`

func scanReceipt(sc interface{ Scan(...any) error }) (Receipt, error) {
	var r Receipt
	var created int64
	var closed sql.NullInt64
	err := sc.Scan(&r.ID, &r.Code, &r.WarehouseID, &r.WarehouseCode, &r.Supplier, &r.DocNo, &r.Status,
		&created, &closed, &r.Lines, &r.Units)
	r.CreatedAt = fromUnix(created)
	r.ClosedAt = fromNullUnix(closed)
	return r, err
}

// ListReceipts returns receipts newest first; warehouseID 0 means all.
func (s *Store) ListReceipts(ctx context.Context, warehouseID int64, openOnly bool, limit int) ([]Receipt, error) {
	q := `SELECT ` + receiptCols + ` FROM receipts r JOIN warehouses w ON w.id = r.warehouse_id
		WHERE (? = 0 OR r.warehouse_id = ?)`
	if openOnly {
		q += ` AND r.status = 'open'`
	}
	q += ` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, q, warehouseID, warehouseID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetReceipt loads one receipt.
func (s *Store) GetReceipt(ctx context.Context, id int64) (Receipt, error) {
	r, err := scanReceipt(s.DB.QueryRowContext(ctx,
		`SELECT `+receiptCols+` FROM receipts r JOIN warehouses w ON w.id = r.warehouse_id WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// ReceiptLines sums received quantities per product and bin.
func (s *Store) ReceiptLines(ctx context.Context, receiptID int64) ([]ReceiptLine, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.sku, p.name, p.unit, x.code, SUM(m.qty)
		FROM movements m JOIN products p ON p.id = m.product_id JOIN bins x ON x.id = m.bin_id
		WHERE m.receipt_id = ?
		GROUP BY m.product_id, m.bin_id
		ORDER BY MAX(m.id) DESC`, receiptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReceiptLine
	for rows.Next() {
		var l ReceiptLine
		if err := rows.Scan(&l.ProductID, &l.SKU, &l.Name, &l.Unit, &l.BinCode, &l.Qty); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Receive books qty of a product into a bin under an open receipt.
func (s *Store) Receive(ctx context.Context, receiptID, binID, productID int64, qty Qty, userID int64) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var status, docNo string
		var wh int64
		err := tx.QueryRowContext(ctx, `SELECT status, warehouse_id, doc_no FROM receipts WHERE id = ?`, receiptID).
			Scan(&status, &wh, &docNo)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "open" {
			return ErrInvalidState
		}
		bin, err := getBin(ctx, tx, binID)
		if err != nil {
			return err
		}
		if bin.WarehouseID != wh {
			return ErrWrongWarehouse
		}
		p, err := getProduct(ctx, tx, productID)
		if err != nil {
			return err
		}
		if p.Archived {
			return ErrArchivedProduct
		}
		note := ""
		if docNo != "" {
			note = "İrsaliye " + docNo
		}
		_, err = insertMovement(ctx, tx, mv{product: p.ID, bin: bin.ID, kind: MoveIn, qty: qty, note: note,
			receipt: receiptID, user: userID, at: s.now()})
		return err
	})
}

// UndoLastReceiptLine reverses the most recent scan of a receipt, as long as
// the stock is still in the bin.
func (s *Store) UndoLastReceiptLine(ctx context.Context, receiptID int64, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var id, productID, binID int64
		var qty Qty
		err := tx.QueryRowContext(ctx, `
			SELECT id, product_id, bin_id, qty FROM movements
			WHERE receipt_id = ? AND kind = 'in' AND qty > 0 AND id NOT IN
			      (SELECT transfer_group FROM movements WHERE receipt_id = ? AND transfer_group IS NOT NULL)
			ORDER BY id DESC LIMIT 1`, receiptID, receiptID).Scan(&id, &productID, &binID, &qty)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoChange
		}
		if err != nil {
			return err
		}
		have, err := binQty(ctx, tx, binID, productID)
		if err != nil {
			return err
		}
		if have < qty {
			return ErrInvalidState
		}
		// The reversal points at the line it cancels via transfer_group so
		// a second undo moves on to the previous line.
		_, err = insertMovement(ctx, tx, mv{product: productID, bin: binID, kind: MoveIn, qty: -qty,
			note: "Okutma geri alındı", receipt: receiptID, group: id, user: userID, at: s.now()})
		return err
	})
}

// CloseReceipt finishes a receipt.
func (s *Store) CloseReceipt(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE receipts SET status = 'closed', closed_at = ? WHERE id = ? AND status = 'open'`,
		unix(s.now()), id)
	if err != nil {
		return err
	}
	if mustAffect(res) != nil {
		return ErrInvalidState
	}
	return nil
}
