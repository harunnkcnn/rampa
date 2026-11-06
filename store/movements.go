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
	MoveIn     MoveKind = "in"
	MoveOut    MoveKind = "out"
	MoveAdjust MoveKind = "adjust"
)

// Valid reports whether k is a known kind.
func (k MoveKind) Valid() bool { return k == MoveIn || k == MoveOut || k == MoveAdjust }

// Label is the Turkish UI label.
func (k MoveKind) Label() string {
	switch k {
	case MoveIn:
		return "Giriş"
	case MoveOut:
		return "Çıkış"
	case MoveAdjust:
		return "Sayım"
	}
	return string(k)
}

// Movement is one ledger line. Qty is signed: positive adds stock.
type Movement struct {
	ID           int64
	ProductID    int64
	ProductSKU   string
	ProductName  string
	Unit         string
	Kind         MoveKind
	Qty          Qty
	Note         string
	ShipmentID   int64
	ShipmentCode string
	UserName     string
	CreatedAt    time.Time
}

// MovementInput is what a user records. For MoveIn/MoveOut Qty is the
// positive amount; for MoveAdjust it is the counted on-hand quantity.
type MovementInput struct {
	ProductID int64
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
		delta := in.Qty
		switch in.Kind {
		case MoveOut:
			if p.Stock < in.Qty {
				return &InsufficientStockError{SKU: p.SKU, Name: p.Name, Available: p.Stock, Requested: in.Qty}
			}
			delta = -in.Qty
		case MoveAdjust:
			delta = in.Qty - p.Stock
			if delta == 0 {
				return ErrNoChange
			}
		}
		id, err = insertMovement(ctx, tx, in.ProductID, in.Kind, delta, strings.TrimSpace(in.Note), 0, in.UserID, s.now())
		return err
	})
	return id, err
}

func insertMovement(ctx context.Context, tx *sql.Tx, productID int64, kind MoveKind, delta Qty,
	note string, shipmentID, userID int64, at time.Time) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO movements (product_id, kind, qty, note, shipment_id, user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		productID, kind, delta, note, nullID(shipmentID), nullID(userID), unix(at))
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

// MovementFilter narrows ListMovements.
type MovementFilter struct {
	ProductID int64
	Kind      MoveKind
	Since     time.Time
	Until     time.Time // exclusive
	Limit     int
	Offset    int
}

// ListMovements returns movements newest first.
func (s *Store) ListMovements(ctx context.Context, f MovementFilter) ([]Movement, error) {
	var where []string
	var args []any
	if f.ProductID != 0 {
		where = append(where, "m.product_id = ?")
		args = append(args, f.ProductID)
	}
	if f.Kind.Valid() {
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
		SELECT m.id, m.product_id, p.sku, p.name, p.unit, m.kind, m.qty, m.note,
		       COALESCE(m.shipment_id, 0), COALESCE(sh.code, ''), COALESCE(u.username, ''), m.created_at
		FROM movements m
		JOIN products p ON p.id = m.product_id
		LEFT JOIN shipments sh ON sh.id = m.shipment_id
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
		if err := rows.Scan(&m.ID, &m.ProductID, &m.ProductSKU, &m.ProductName, &m.Unit, &m.Kind,
			&m.Qty, &m.Note, &m.ShipmentID, &m.ShipmentCode, &m.UserName, &created); err != nil {
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
}

// GetOverview computes dashboard counters. dayStart is local midnight.
func (s *Store) GetOverview(ctx context.Context, dayStart time.Time) (Overview, error) {
	var o Overview
	err := s.DB.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM products WHERE archived = 0),
		  (SELECT COUNT(*) FROM products p `+stockJoin+`
		    WHERE p.archived = 0 AND p.min_stock > 0 AND COALESCE(s.stock,0) <= p.min_stock),
		  (SELECT COUNT(*) FROM movements WHERE kind = 'in'  AND created_at >= ?1),
		  (SELECT COUNT(*) FROM movements WHERE kind = 'out' AND created_at >= ?1),
		  (SELECT COUNT(*) FROM shipments WHERE status IN ('draft','loaded')),
		  (SELECT COUNT(*) FROM shipments WHERE loaded_at >= ?1)`,
		unix(dayStart)).Scan(&o.ActiveProducts, &o.LowStock, &o.TodayIn, &o.TodayOut, &o.OpenShipments, &o.LoadedToday)
	return o, err
}
