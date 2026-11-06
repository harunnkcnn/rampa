package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ShipStatus is the lifecycle state of a shipment.
type ShipStatus string

const (
	ShipDraft     ShipStatus = "draft"
	ShipLoaded    ShipStatus = "loaded"
	ShipDelivered ShipStatus = "delivered"
	ShipCancelled ShipStatus = "cancelled"
)

// Label is the Turkish UI label.
func (s ShipStatus) Label() string {
	switch s {
	case ShipDraft:
		return "Hazırlanıyor"
	case ShipLoaded:
		return "Yüklendi"
	case ShipDelivered:
		return "Teslim edildi"
	case ShipCancelled:
		return "İptal"
	}
	return string(s)
}

// Valid reports whether s is a known status.
func (s ShipStatus) Valid() bool {
	switch s {
	case ShipDraft, ShipLoaded, ShipDelivered, ShipCancelled:
		return true
	}
	return false
}

// Shipment is an outbound load. Loading it deducts stock.
type Shipment struct {
	ID          int64
	Code        string
	Customer    string
	Plate       string
	Driver      string
	Note        string
	Status      ShipStatus
	CreatedAt   time.Time
	LoadedAt    *time.Time
	DeliveredAt *time.Time
	ItemCount   int
	Items       []ShipmentItem
}

// Editable reports whether items and header may change.
func (s Shipment) Editable() bool { return s.Status == ShipDraft }

// TotalWeightKg sums item weights.
func (s Shipment) TotalWeightKg() float64 {
	var t float64
	for _, it := range s.Items {
		t += it.WeightKg * it.Qty.Float()
	}
	return t
}

// TotalVolumeM3 sums item volumes.
func (s Shipment) TotalVolumeM3() float64 {
	var t float64
	for _, it := range s.Items {
		t += it.VolumeM3 * it.Qty.Float()
	}
	return t
}

// ShipmentItem is one product line on a shipment.
type ShipmentItem struct {
	ID         int64
	ShipmentID int64
	ProductID  int64
	SKU        string
	Name       string
	Unit       string
	Qty        Qty
	WeightKg   float64 // per unit
	VolumeM3   float64 // per unit
	Stock      Qty
}

// Short reports whether current stock cannot cover the line.
func (it ShipmentItem) Short() bool { return it.Stock < it.Qty }

// ShipmentHeader holds the editable header fields.
type ShipmentHeader struct {
	Customer string
	Plate    string
	Driver   string
	Note     string
}

func (h *ShipmentHeader) normalize() error {
	h.Customer = strings.TrimSpace(h.Customer)
	h.Plate = strings.ToUpper(strings.Join(strings.Fields(h.Plate), " "))
	h.Driver = strings.TrimSpace(h.Driver)
	h.Note = strings.TrimSpace(h.Note)
	if h.Customer == "" {
		return ErrMissingRequired
	}
	return nil
}

// CreateShipment opens a draft and assigns a code like SVK-2026-0007.
func (s *Store) CreateShipment(ctx context.Context, h ShipmentHeader, userID int64) (int64, error) {
	if err := h.normalize(); err != nil {
		return 0, err
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		res, err := tx.ExecContext(ctx, `
			INSERT INTO shipments (customer, plate, driver, note, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, h.Customer, h.Plate, h.Driver, h.Note, nullID(userID), unix(now))
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shipments SET code = ? WHERE id = ?`,
			fmt.Sprintf("SVK-%d-%04d", now.Year(), id), id)
		return err
	})
	return id, err
}

// UpdateShipmentHeader edits a draft's header.
func (s *Store) UpdateShipmentHeader(ctx context.Context, id int64, h ShipmentHeader) error {
	if err := h.normalize(); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE shipments SET customer = ?, plate = ?, driver = ?, note = ?
		WHERE id = ? AND status IN ('draft','loaded')`, h.Customer, h.Plate, h.Driver, h.Note, id)
	if err != nil {
		return err
	}
	if err := mustAffect(res); err != nil {
		return ErrInvalidState
	}
	return nil
}

// ShipmentFilter narrows ListShipments.
type ShipmentFilter struct {
	Status ShipStatus
	Open   bool // draft or loaded
	Since  time.Time
	Limit  int
}

// ListShipments returns shipments newest first, without items.
func (s *Store) ListShipments(ctx context.Context, f ShipmentFilter) ([]Shipment, error) {
	var where []string
	var args []any
	if f.Status.Valid() {
		where = append(where, "sh.status = ?")
		args = append(args, f.Status)
	}
	if f.Open {
		where = append(where, "sh.status IN ('draft','loaded')")
	}
	if !f.Since.IsZero() {
		where = append(where, "sh.created_at >= ?")
		args = append(args, unix(f.Since))
	}
	q := `SELECT sh.id, sh.code, sh.customer, sh.plate, sh.driver, sh.note, sh.status,
	             sh.created_at, sh.loaded_at, sh.delivered_at,
	             (SELECT COUNT(*) FROM shipment_items i WHERE i.shipment_id = sh.id)
	      FROM shipments sh`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY sh.created_at DESC, sh.id DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shipment
	for rows.Next() {
		sh, err := scanShipment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func scanShipment(sc interface{ Scan(...any) error }) (Shipment, error) {
	var sh Shipment
	var created int64
	var loaded, delivered sql.NullInt64
	err := sc.Scan(&sh.ID, &sh.Code, &sh.Customer, &sh.Plate, &sh.Driver, &sh.Note, &sh.Status,
		&created, &loaded, &delivered, &sh.ItemCount)
	sh.CreatedAt = fromUnix(created)
	sh.LoadedAt = fromNullUnix(loaded)
	sh.DeliveredAt = fromNullUnix(delivered)
	return sh, err
}

// GetShipment loads a shipment with its items and current stock per line.
func (s *Store) GetShipment(ctx context.Context, id int64) (Shipment, error) {
	sh, err := scanShipment(s.DB.QueryRowContext(ctx, `
		SELECT sh.id, sh.code, sh.customer, sh.plate, sh.driver, sh.note, sh.status,
		       sh.created_at, sh.loaded_at, sh.delivered_at,
		       (SELECT COUNT(*) FROM shipment_items i WHERE i.shipment_id = sh.id)
		FROM shipments sh WHERE sh.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sh, ErrNotFound
	}
	if err != nil {
		return sh, err
	}
	sh.Items, err = listItems(ctx, s.DB, id)
	return sh, err
}

type rowsQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listItems(ctx context.Context, q rowsQueryer, shipmentID int64) ([]ShipmentItem, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT i.id, i.shipment_id, i.product_id, p.sku, p.name, p.unit, i.qty,
		       p.weight_kg, p.volume_m3, COALESCE(s.stock, 0)
		FROM shipment_items i
		JOIN products p ON p.id = i.product_id
		LEFT JOIN (SELECT product_id, SUM(qty) AS stock FROM movements GROUP BY product_id) s
		       ON s.product_id = p.id
		WHERE i.shipment_id = ?
		ORDER BY i.id`, shipmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShipmentItem
	for rows.Next() {
		var it ShipmentItem
		if err := rows.Scan(&it.ID, &it.ShipmentID, &it.ProductID, &it.SKU, &it.Name, &it.Unit,
			&it.Qty, &it.WeightKg, &it.VolumeM3, &it.Stock); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func shipmentStatus(ctx context.Context, tx *sql.Tx, id int64) (ShipStatus, error) {
	var st ShipStatus
	err := tx.QueryRowContext(ctx, `SELECT status FROM shipments WHERE id = ?`, id).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}

// AddShipmentItem adds qty of a product to a draft, merging duplicate lines.
func (s *Store) AddShipmentItem(ctx context.Context, shipmentID, productID int64, qty Qty) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft {
			return ErrInvalidState
		}
		p, err := getProduct(ctx, tx, productID)
		if err != nil {
			return err
		}
		if p.Archived {
			return ErrArchivedProduct
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO shipment_items (shipment_id, product_id, qty) VALUES (?, ?, ?)
			ON CONFLICT (shipment_id, product_id) DO UPDATE SET qty = qty + excluded.qty`,
			shipmentID, productID, qty)
		return err
	})
}

// RemoveShipmentItem deletes a line from a draft.
func (s *Store) RemoveShipmentItem(ctx context.Context, shipmentID, itemID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft {
			return ErrInvalidState
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM shipment_items WHERE id = ? AND shipment_id = ?`, itemID, shipmentID)
		if err != nil {
			return err
		}
		return mustAffect(res)
	})
}

// LoadShipment marks a draft as loaded and writes one out-movement per line.
// It fails without side effects if any line lacks stock.
func (s *Store) LoadShipment(ctx context.Context, shipmentID, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft {
			return ErrInvalidState
		}
		items, err := listItems(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrEmptyShipment
		}
		for _, it := range items {
			if it.Short() {
				return &InsufficientStockError{SKU: it.SKU, Name: it.Name, Available: it.Stock, Requested: it.Qty}
			}
		}
		now := s.now()
		for _, it := range items {
			if _, err := insertMovement(ctx, tx, it.ProductID, MoveOut, -it.Qty, "", shipmentID, userID, now); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE shipments SET status = 'loaded', loaded_at = ? WHERE id = ?`, unix(now), shipmentID)
		return err
	})
}

// DeliverShipment closes a loaded shipment.
func (s *Store) DeliverShipment(ctx context.Context, shipmentID int64) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE shipments SET status = 'delivered', delivered_at = ?
		WHERE id = ? AND status = 'loaded'`, unix(s.now()), shipmentID)
	if err != nil {
		return err
	}
	if mustAffect(res) != nil {
		return ErrInvalidState
	}
	return nil
}

// CancelShipment cancels a draft, or a loaded shipment by returning its stock.
func (s *Store) CancelShipment(ctx context.Context, shipmentID, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		switch st {
		case ShipDraft:
		case ShipLoaded:
			items, err := listItems(ctx, tx, shipmentID)
			if err != nil {
				return err
			}
			now := s.now()
			for _, it := range items {
				if _, err := insertMovement(ctx, tx, it.ProductID, MoveIn, it.Qty,
					"Sevkiyat iptali, stoğa iade", shipmentID, userID, now); err != nil {
					return err
				}
			}
		default:
			return ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE shipments SET status = 'cancelled' WHERE id = ?`, shipmentID)
		return err
	})
}
