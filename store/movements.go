package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// MoveKind classifies a stock movement.
type MoveKind string

const (
	MoveIn       MoveKind = "in"
	MoveOut      MoveKind = "out"
	MoveAdjust   MoveKind = "adjust"
	MoveTransfer MoveKind = "transfer"
)

// Valid reports whether k is a kind a user may record directly.
func (k MoveKind) Valid() bool { return k == MoveIn || k == MoveOut || k == MoveAdjust }

// Known reports whether k is any stored kind (for filters).
func (k MoveKind) Known() bool { return k.Valid() || k == MoveTransfer }

// Label is the Turkish UI label.
func (k MoveKind) Label() string {
	switch k {
	case MoveIn:
		return "Giriş"
	case MoveOut:
		return "Çıkış"
	case MoveAdjust:
		return "Sayım"
	case MoveTransfer:
		return "Transfer"
	}
	return string(k)
}

// Movement is one ledger line. Qty is signed: positive adds stock.
type Movement struct {
	ID            int64
	ProductID     int64
	ProductSKU    string
	ProductName   string
	Unit          string
	BinID         int64
	BinCode       string
	WarehouseCode string
	Kind          MoveKind
	Qty           Qty
	Note          string
	ShipmentID    int64
	ShipmentCode  string
	ReceiptID     int64
	ReceiptCode   string
	UserName      string
	CreatedAt     time.Time
}

// Where is "01/A-01-2" for display.
func (m Movement) Where() string { return m.WarehouseCode + "/" + m.BinCode }

// MovementInput is what a user records. For MoveIn/MoveOut Qty is the
// positive amount; for MoveAdjust it is the counted on-hand quantity of the
// product in that bin.
type MovementInput struct {
	ProductID int64
	BinID     int64
	Kind      MoveKind
	Qty       Qty
	Note      string
	UserID    int64
}

// RecordMovement validates and writes a manual movement atomically.
func (s *Store) RecordMovement(ctx context.Context, in MovementInput) (int64, error) {
	if !in.Kind.Valid() {
		return 0, ErrInvalidState
	}
	if in.Kind != MoveAdjust && in.Qty <= 0 {
		return 0, ErrNonPositive
	}
	if in.Kind == MoveAdjust && in.Qty < 0 {
		return 0, ErrInvalidQty
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := getProduct(ctx, tx, in.ProductID)
		if err != nil {
			return err
		}
		if p.Archived {
			return ErrArchivedProduct
		}
		bin, err := getBin(ctx, tx, in.BinID)
		if err != nil {
			return err
		}
		have, err := binQty(ctx, tx, bin.ID, p.ID)
		if err != nil {
			return err
		}
		delta := in.Qty
		switch in.Kind {
		case MoveOut:
			if have < in.Qty {
				return &InsufficientStockError{SKU: p.SKU, Name: p.Name, Available: have, Requested: in.Qty, Bin: bin.Code}
			}
			delta = -in.Qty
		case MoveAdjust:
			delta = in.Qty - have
			if delta == 0 {
				return ErrNoChange
			}
		}
		id, err = insertMovement(ctx, tx, mv{product: p.ID, bin: bin.ID, kind: in.Kind, qty: delta,
			note: strings.TrimSpace(in.Note), user: in.UserID, at: s.now()})
		return err
	})
	return id, err
}

// mv is one ledger row to insert.
type mv struct {
	product, bin                          int64
	kind                                  MoveKind
	qty                                   Qty
	note                                  string
	shipment, receipt, count, group, user int64
	at                                    time.Time
}

func insertMovement(ctx context.Context, tx *sql.Tx, m mv) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO movements (product_id, bin_id, kind, qty, note, shipment_id, receipt_id, count_id,
		                       transfer_group, user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.product, m.bin, m.kind, m.qty, m.note, nullID(m.shipment), nullID(m.receipt), nullID(m.count),
		nullID(m.group), nullID(m.user), unix(m.at))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Transfer moves stock of a product between two bins of the same warehouse.
func (s *Store) Transfer(ctx context.Context, productID, fromBin, toBin int64, qty Qty, userID int64) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	if fromBin == toBin {
		return ErrSameBin
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		p, err := getProduct(ctx, tx, productID)
		if err != nil {
			return err
		}
		from, err := getBin(ctx, tx, fromBin)
		if err != nil {
			return err
		}
		to, err := getBin(ctx, tx, toBin)
		if err != nil {
			return err
		}
		if from.WarehouseID != to.WarehouseID {
			return ErrWrongWarehouse
		}
		have, err := binQty(ctx, tx, from.ID, p.ID)
		if err != nil {
			return err
		}
		if have < qty {
			return &InsufficientStockError{SKU: p.SKU, Name: p.Name, Available: have, Requested: qty, Bin: from.Code}
		}
		now := s.now()
		first, err := insertMovement(ctx, tx, mv{product: p.ID, bin: from.ID, kind: MoveTransfer, qty: -qty,
			note: "→ " + to.Code, user: userID, at: now})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE movements SET transfer_group = id WHERE id = ?`, first); err != nil {
			return err
		}
		_, err = insertMovement(ctx, tx, mv{product: p.ID, bin: to.ID, kind: MoveTransfer, qty: qty,
			note: "← " + from.Code, group: first, user: userID, at: now})
		return err
	})
}

// MovementFilter narrows ListMovements.
type MovementFilter struct {
	ProductID   int64
	WarehouseID int64
	BinID       int64
	Kind        MoveKind
	Since       time.Time
	Until       time.Time // exclusive
	Limit       int
	Offset      int
}

// ListMovements returns movements newest first.
func (s *Store) ListMovements(ctx context.Context, f MovementFilter) ([]Movement, error) {
	var where []string
	var args []any
	if f.ProductID != 0 {
		where = append(where, "m.product_id = ?")
		args = append(args, f.ProductID)
	}
	if f.WarehouseID != 0 {
		where = append(where, "x.warehouse_id = ?")
		args = append(args, f.WarehouseID)
	}
	if f.BinID != 0 {
		where = append(where, "m.bin_id = ?")
		args = append(args, f.BinID)
	}
	if f.Kind.Known() {
		where = append(where, "m.kind = ?")
		args = append(args, f.Kind)
	}
	if !f.Since.IsZero() {
		where = append(where, "m.created_at >= ?")
		args = append(args, unix(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "m.created_at < ?")
		args = append(args, unix(f.Until))
	}
	q := `
		SELECT m.id, m.product_id, p.sku, p.name, p.unit, m.bin_id, x.code, w.code, m.kind, m.qty, m.note,
		       COALESCE(m.shipment_id, 0), COALESCE(sh.code, ''), COALESCE(m.receipt_id, 0), COALESCE(r.code, ''),
		       COALESCE(u.username, ''), m.created_at
		FROM movements m
		JOIN products p ON p.id = m.product_id
		JOIN bins x ON x.id = m.bin_id
		JOIN warehouses w ON w.id = x.warehouse_id
		LEFT JOIN shipments sh ON sh.id = m.shipment_id
		LEFT JOIN receipts r ON r.id = m.receipt_id
		LEFT JOIN users u ON u.id = m.user_id`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY m.created_at DESC, m.id DESC"
	if f.Limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movement
	for rows.Next() {
		var m Movement
		var created int64
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductSKU, &m.ProductName, &m.Unit, &m.BinID, &m.BinCode,
			&m.WarehouseCode, &m.Kind, &m.Qty, &m.Note, &m.ShipmentID, &m.ShipmentCode, &m.ReceiptID, &m.ReceiptCode,
			&m.UserName, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = fromUnix(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Overview is the dashboard summary.
type Overview struct {
	ActiveProducts int
	LowStock       int
	TodayIn        int
	TodayOut       int
	OpenShipments  int
	LoadedToday    int
	OpenReceipts   int
}

// GetOverview computes dashboard counters for one warehouse, or all when
// warehouseID is 0. dayStart is local midnight.
func (s *Store) GetOverview(ctx context.Context, dayStart time.Time, warehouseID int64) (Overview, error) {
	var o Overview
	err := s.DB.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM products WHERE archived = 0),
		  (SELECT COUNT(*) FROM products p
		     LEFT JOIN (SELECT m.product_id, SUM(m.qty) AS stock FROM movements m JOIN bins x ON x.id = m.bin_id
		                WHERE ?2 = 0 OR x.warehouse_id = ?2 GROUP BY m.product_id) s ON s.product_id = p.id
		    WHERE p.archived = 0 AND p.min_stock > 0 AND COALESCE(s.stock,0) <= p.min_stock),
		  (SELECT COUNT(*) FROM movements m JOIN bins x ON x.id = m.bin_id
		    WHERE m.kind = 'in' AND m.created_at >= ?1 AND (?2 = 0 OR x.warehouse_id = ?2)),
		  (SELECT COUNT(*) FROM movements m JOIN bins x ON x.id = m.bin_id
		    WHERE m.kind = 'out' AND m.created_at >= ?1 AND (?2 = 0 OR x.warehouse_id = ?2)),
		  (SELECT COUNT(*) FROM shipments WHERE status IN ('draft','loaded') AND (?2 = 0 OR warehouse_id = ?2)),
		  (SELECT COUNT(*) FROM shipments WHERE loaded_at >= ?1 AND (?2 = 0 OR warehouse_id = ?2)),
		  (SELECT COUNT(*) FROM receipts WHERE status = 'open' AND (?2 = 0 OR warehouse_id = ?2))`,
		unix(dayStart), warehouseID).Scan(&o.ActiveProducts, &o.LowStock, &o.TodayIn, &o.TodayOut,
		&o.OpenShipments, &o.LoadedToday, &o.OpenReceipts)
	return o, err
}
