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
	ID            int64
	WarehouseID   int64
	WarehouseCode string
	Code          string
	Customer      string
	Plate         string
	Driver        string
	Note          string
	Status        ShipStatus
	CreatedAt     time.Time
	LoadedAt      *time.Time
	DeliveredAt   *time.Time
	ItemCount     int
	Items         []ShipmentItem
}

// PickedAll reports whether every line has been fully picked by scanning.
func (s Shipment) PickedAll() bool {
	if len(s.Items) == 0 {
		return false
	}
	for _, it := range s.Items {
		if it.Picked < it.Qty {
			return false
		}
	}
	return true
}

// PickProgress returns picked and ordered line counts.
func (s Shipment) PickProgress() (done, total int) {
	for _, it := range s.Items {
		if it.Picked >= it.Qty {
			done++
		}
	}
	return done, len(s.Items)
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
	Stock      Qty     // in the shipment's warehouse
	Picked     Qty     // scanned on the terminal
	Packed     Qty     // put into packages
}

// Short reports whether current stock cannot cover the line.
func (it ShipmentItem) Short() bool { return it.Stock < it.Qty }

// Remaining is the quantity still to pick.
func (it ShipmentItem) Remaining() Qty { return max(it.Qty-it.Picked, 0) }

// ShipmentHeader holds the editable header fields.
type ShipmentHeader struct {
	WarehouseID int64 // used on create; 0 picks the first active warehouse
	Customer    string
	Plate       string
	Driver      string
	Note        string
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
		wh := h.WarehouseID
		if wh == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT id FROM warehouses WHERE active = 1 ORDER BY id LIMIT 1`).Scan(&wh); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO shipments (warehouse_id, customer, plate, driver, note, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, wh, h.Customer, h.Plate, h.Driver, h.Note, nullID(userID), unix(now))
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
	WarehouseID int64
	Status      ShipStatus
	Open        bool // draft or loaded
	Since       time.Time
	Limit       int
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
	if f.WarehouseID != 0 {
		where = append(where, "sh.warehouse_id = ?")
		args = append(args, f.WarehouseID)
	}
	if !f.Since.IsZero() {
		where = append(where, "sh.created_at >= ?")
		args = append(args, unix(f.Since))
	}
	q := `SELECT ` + shipmentCols + ` FROM shipments sh JOIN warehouses w ON w.id = sh.warehouse_id`
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

const shipmentCols = `sh.id, sh.warehouse_id, w.code, sh.code, sh.customer, sh.plate, sh.driver, sh.note, sh.status,
	sh.created_at, sh.loaded_at, sh.delivered_at,
	(SELECT COUNT(*) FROM shipment_items i WHERE i.shipment_id = sh.id)`

func scanShipment(sc interface{ Scan(...any) error }) (Shipment, error) {
	var sh Shipment
	var created int64
	var loaded, delivered sql.NullInt64
	err := sc.Scan(&sh.ID, &sh.WarehouseID, &sh.WarehouseCode, &sh.Code, &sh.Customer, &sh.Plate, &sh.Driver, &sh.Note, &sh.Status,
		&created, &loaded, &delivered, &sh.ItemCount)
	sh.CreatedAt = fromUnix(created)
	sh.LoadedAt = fromNullUnix(loaded)
	sh.DeliveredAt = fromNullUnix(delivered)
	return sh, err
}

// GetShipment loads a shipment with its items and current stock per line.
func (s *Store) GetShipment(ctx context.Context, id int64) (Shipment, error) {
	sh, err := scanShipment(s.DB.QueryRowContext(ctx,
		`SELECT `+shipmentCols+` FROM shipments sh JOIN warehouses w ON w.id = sh.warehouse_id WHERE sh.id = ?`, id))
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
		       p.weight_kg, p.volume_m3,
		       (SELECT COALESCE(SUM(m.qty), 0) FROM movements m JOIN bins x ON x.id = m.bin_id
		         WHERE m.product_id = i.product_id AND x.warehouse_id = sh.warehouse_id),
		       (SELECT COALESCE(SUM(k.qty), 0) FROM picks k WHERE k.shipment_item_id = i.id),
		       (SELECT COALESCE(SUM(pi.qty), 0) FROM package_items pi JOIN packages pk ON pk.id = pi.package_id
		         WHERE pk.shipment_id = i.shipment_id AND pi.product_id = i.product_id)
		FROM shipment_items i
		JOIN shipments sh ON sh.id = i.shipment_id
		JOIN products p ON p.id = i.product_id
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
			&it.Qty, &it.WeightKg, &it.VolumeM3, &it.Stock, &it.Picked, &it.Packed); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func shipmentStatus(ctx context.Context, tx *sql.Tx, id int64) (ShipStatus, error) {
	st, _, err := shipmentState(ctx, tx, id)
	return st, err
}

func shipmentState(ctx context.Context, tx *sql.Tx, id int64) (ShipStatus, int64, error) {
	var st ShipStatus
	var wh int64
	err := tx.QueryRowContext(ctx, `SELECT status, warehouse_id FROM shipments WHERE id = ?`, id).Scan(&st, &wh)
	if errors.Is(err, sql.ErrNoRows) {
		return st, 0, ErrNotFound
	}
	return st, wh, err
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

// allocation is stock taken from one bin for one line.
type allocation struct {
	productID, binID int64
	qty              Qty
}

// LoadShipment marks a draft as loaded and writes out-movements per bin.
// Quantities picked on the terminal leave from the scanned bins; the rest
// is taken automatically from the shipment's warehouse, shelves first and
// GENEL last. It fails without side effects if any line lacks stock.
func (s *Store) LoadShipment(ctx context.Context, shipmentID, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, wh, err := shipmentState(ctx, tx, shipmentID)
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
		var plan []allocation
		for _, it := range items {
			if it.Short() {
				return &InsufficientStockError{SKU: it.SKU, Name: it.Name, Available: it.Stock, Requested: it.Qty}
			}
			allocs, err := allocate(ctx, tx, wh, it)
			if err != nil {
				return err
			}
			plan = append(plan, allocs...)
		}
		now := s.now()
		for _, a := range plan {
			if _, err := insertMovement(ctx, tx, mv{product: a.productID, bin: a.binID, kind: MoveOut, qty: -a.qty,
				shipment: shipmentID, user: userID, at: now}); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE shipments SET status = 'loaded', loaded_at = ? WHERE id = ?`, unix(now), shipmentID)
		return err
	})
}

func allocate(ctx context.Context, tx *sql.Tx, warehouseID int64, it ShipmentItem) ([]allocation, error) {
	type binAvail struct {
		id    int64
		code  string
		have  Qty
		taken Qty
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT x.id, x.code, COALESCE((SELECT SUM(m.qty) FROM movements m WHERE m.bin_id = x.id AND m.product_id = ?), 0),
		       COALESCE((SELECT SUM(k.qty) FROM picks k WHERE k.bin_id = x.id AND k.shipment_item_id = ?), 0)
		FROM bins x WHERE x.warehouse_id = ?
		ORDER BY x.code = ?, x.code COLLATE NOCASE`, it.ProductID, it.ID, warehouseID, DefaultBinCode)
	if err != nil {
		return nil, err
	}
	var bins []binAvail
	for rows.Next() {
		var b binAvail
		if err := rows.Scan(&b.id, &b.code, &b.have, &b.taken); err != nil {
			rows.Close()
			return nil, err
		}
		bins = append(bins, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []allocation
	need := it.Qty
	// Picked quantities first, from the bins they were scanned in.
	for i := range bins {
		b := &bins[i]
		if b.taken == 0 {
			continue
		}
		take := min(b.taken, need)
		if b.have < take {
			return nil, &InsufficientStockError{SKU: it.SKU, Name: it.Name, Available: b.have, Requested: take, Bin: b.code}
		}
		out = append(out, allocation{it.ProductID, b.id, take})
		b.have -= take
		need -= take
	}
	for i := range bins {
		if need == 0 {
			break
		}
		b := &bins[i]
		if b.have <= 0 {
			continue
		}
		take := min(b.have, need)
		out = append(out, allocation{it.ProductID, b.id, take})
		need -= take
	}
	if need > 0 {
		return nil, &InsufficientStockError{SKU: it.SKU, Name: it.Name, Available: it.Qty - need, Requested: it.Qty}
	}
	return out, nil
}

// PickResult reports a line after a scan.
type PickResult struct {
	Item ShipmentItem
	Bin  Bin
	Done bool // the whole shipment is picked
}

// AddPick records that qty of a product was taken from a bin for a draft
// shipment. It refuses picks beyond the ordered quantity or the bin stock.
func (s *Store) AddPick(ctx context.Context, shipmentID, binID, productID int64, qty Qty, userID int64) (PickResult, error) {
	var res PickResult
	if qty <= 0 {
		return res, ErrNonPositive
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		st, wh, err := shipmentState(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft {
			return ErrInvalidState
		}
		bin, err := getBin(ctx, tx, binID)
		if err != nil {
			return err
		}
		if bin.WarehouseID != wh {
			return ErrWrongWarehouse
		}
		items, err := listItems(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		idx := -1
		for i, it := range items {
			if it.ProductID == productID {
				idx = i
			}
		}
		if idx < 0 {
			return ErrNotOnShipment
		}
		it := items[idx]
		if it.Picked+qty > it.Qty {
			return ErrOverPick
		}
		have, err := binQty(ctx, tx, bin.ID, productID)
		if err != nil {
			return err
		}
		var already Qty
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(qty), 0) FROM picks WHERE shipment_item_id = ? AND bin_id = ?`,
			it.ID, bin.ID).Scan(&already); err != nil {
			return err
		}
		if have < already+qty {
			return &InsufficientStockError{SKU: it.SKU, Name: it.Name, Available: have - already, Requested: qty, Bin: bin.Code}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO picks (shipment_item_id, bin_id, qty, user_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			it.ID, bin.ID, qty, nullID(userID), unix(s.now())); err != nil {
			return err
		}
		it.Picked += qty
		items[idx] = it
		res.Item, res.Bin = it, bin
		res.Done = Shipment{Items: items}.PickedAll()
		return nil
	})
	return res, err
}

// ClearPicks forgets the scanned picks of one line, e.g. after a mistake.
func (s *Store) ClearPicks(ctx context.Context, shipmentID, itemID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := shipmentStatus(ctx, tx, shipmentID)
		if err != nil {
			return err
		}
		if st != ShipDraft {
			return ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM picks WHERE shipment_item_id IN
			(SELECT id FROM shipment_items WHERE id = ? AND shipment_id = ?)`, itemID, shipmentID)
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
			// Return each quantity to the bin it left from.
			rows, err := tx.QueryContext(ctx, `
				SELECT product_id, bin_id, -SUM(qty) FROM movements
				WHERE shipment_id = ? AND kind = 'out' GROUP BY product_id, bin_id`, shipmentID)
			if err != nil {
				return err
			}
			var back []allocation
			for rows.Next() {
				var a allocation
				if err := rows.Scan(&a.productID, &a.binID, &a.qty); err != nil {
					rows.Close()
					return err
				}
				back = append(back, a)
			}
			rows.Close()
			now := s.now()
			for _, a := range back {
				if a.qty <= 0 {
					continue
				}
				if _, err := insertMovement(ctx, tx, mv{product: a.productID, bin: a.binID, kind: MoveIn, qty: a.qty,
					note: "Sevkiyat iptali, stoğa iade", shipment: shipmentID, user: userID, at: now}); err != nil {
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
