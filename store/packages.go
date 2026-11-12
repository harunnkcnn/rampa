package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Package is a carton or pallet built for a shipment.
type Package struct {
	ID           int64
	ShipmentID   int64
	ShipmentCode string
	Customer     string
	Seq          int
	Kind         string // koli, palet
	Status       string // open, closed
	CreatedAt    time.Time
	Items        []PackageItem
}

// Code is the label code, e.g. SVK-2026-0007-K02.
func (p Package) Code() string {
	letter := "K"
	if p.Kind == "palet" {
		letter = "P"
	}
	return fmt.Sprintf("%s-%s%02d", p.ShipmentCode, letter, p.Seq)
}

// KindLabel is the Turkish label.
func (p Package) KindLabel() string {
	if p.Kind == "palet" {
		return "Palet"
	}
	return "Koli"
}

// Open reports whether items may be added.
func (p Package) Open() bool { return p.Status == "open" }

// Units sums the quantities in the package.
func (p Package) Units() Qty {
	var t Qty
	for _, it := range p.Items {
		t += it.Qty
	}
	return t
}

// PackageItem is a product inside a package.
type PackageItem struct {
	ProductID int64
	SKU       string
	Name      string
	Unit      string
	Qty       Qty
}

// CreatePackage opens a new koli or palet on a shipment that is not closed.
func (s *Store) CreatePackage(ctx context.Context, shipmentID int64, kind string) (int64, error) {
	if kind != "koli" && kind != "palet" {
		return 0, ErrInvalidState
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft && st != ShipLoaded {
			return ErrInvalidState
		}
		var seq int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM packages WHERE shipment_id = ?`, shipmentID).Scan(&seq); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO packages (shipment_id, seq, kind, created_at) VALUES (?, ?, ?, ?)`,
			shipmentID, seq, kind, unix(s.now()))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// ListPackages returns a shipment's packages with their contents.
func (s *Store) ListPackages(ctx context.Context, shipmentID int64) ([]Package, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT pk.id FROM packages pk WHERE pk.shipment_id = ? ORDER BY pk.seq`, shipmentID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]Package, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetPackage(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// GetPackage loads one package with its items.
func (s *Store) GetPackage(ctx context.Context, id int64) (Package, error) {
	var p Package
	var created int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT pk.id, pk.shipment_id, sh.code, sh.customer, pk.seq, pk.kind, pk.status, pk.created_at
		FROM packages pk JOIN shipments sh ON sh.id = pk.shipment_id WHERE pk.id = ?`, id).
		Scan(&p.ID, &p.ShipmentID, &p.ShipmentCode, &p.Customer, &p.Seq, &p.Kind, &p.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt = fromUnix(created)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT pr.id, pr.sku, pr.name, pr.unit, pi.qty FROM package_items pi JOIN products pr ON pr.id = pi.product_id
		WHERE pi.package_id = ? ORDER BY pr.name COLLATE NOCASE`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var it PackageItem
		if err := rows.Scan(&it.ProductID, &it.SKU, &it.Name, &it.Unit, &it.Qty); err != nil {
			return p, err
		}
		p.Items = append(p.Items, it)
	}
	return p, rows.Err()
}

// PackItem puts qty of a product into an open package. The total packed
// across the shipment may not exceed the shipment line.
func (s *Store) PackItem(ctx context.Context, packageID, productID int64, qty Qty) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var shipmentID int64
		var status string
		err := tx.QueryRowContext(ctx, `SELECT shipment_id, status FROM packages WHERE id = ?`, packageID).Scan(&shipmentID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "open" {
			return ErrInvalidState
		}
		var ordered, packed Qty
		err = tx.QueryRowContext(ctx, `SELECT qty FROM shipment_items WHERE shipment_id = ? AND product_id = ?`,
			shipmentID, productID).Scan(&ordered)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotOnShipment
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(pi.qty), 0) FROM package_items pi JOIN packages pk ON pk.id = pi.package_id
			WHERE pk.shipment_id = ? AND pi.product_id = ?`, shipmentID, productID).Scan(&packed); err != nil {
			return err
		}
		if packed+qty > ordered {
			return ErrOverPick
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO package_items (package_id, product_id, qty) VALUES (?, ?, ?)
			ON CONFLICT (package_id, product_id) DO UPDATE SET qty = qty + excluded.qty`, packageID, productID, qty)
		return err
	})
}

// ClosePackage seals a package; an empty package is deleted instead.
func (s *Store) ClosePackage(ctx context.Context, packageID int64) (deleted bool, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM package_items WHERE package_id = ?`, packageID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			res, err := tx.ExecContext(ctx, `DELETE FROM packages WHERE id = ? AND status = 'open'`, packageID)
			if err != nil {
				return err
			}
			deleted = true
			return mustAffect(res)
		}
		res, err := tx.ExecContext(ctx, `UPDATE packages SET status = 'closed' WHERE id = ? AND status = 'open'`, packageID)
		if err != nil {
			return err
		}
		if mustAffect(res) != nil {
			return ErrInvalidState
		}
		return nil
	})
	return deleted, err
}
