package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// DefaultBinCode is created in every warehouse for stock not yet put away.
const DefaultBinCode = "GENEL"

// Branch is a site (şube) that owns one or more warehouses.
type Branch struct {
	ID         int64
	Name       string
	Warehouses []Warehouse
}

// Warehouse is a stock-holding location (depo) inside a branch.
type Warehouse struct {
	ID         int64
	BranchID   int64
	BranchName string
	Code       string
	Name       string
	Active     bool
	BinCount   int
	CreatedAt  time.Time
}

// Label is "01 · Ana depo".
func (w Warehouse) Label() string { return w.Code + " · " + w.Name }

// Bin is a shelf position (hücre) inside a warehouse.
type Bin struct {
	ID            int64
	WarehouseID   int64
	WarehouseCode string
	Code          string
	Active        bool
}

// BinStock is the quantity of one product in one bin.
type BinStock struct {
	BinID       int64
	BinCode     string
	WarehouseID int64
	ProductID   int64
	SKU         string
	Name        string
	Unit        string
	Qty         Qty
}

// NormalizeCode upper-cases and trims codes typed or scanned by users.
func NormalizeCode(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// ListBranches returns branches with their warehouses, active ones first.
func (s *Store) ListBranches(ctx context.Context) ([]Branch, error) {
	ws, err := s.ListWarehouses(ctx, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name FROM branches ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Branch
	idx := map[int64]int{}
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, err
		}
		idx[b.ID] = len(out)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, w := range ws {
		if i, ok := idx[w.BranchID]; ok {
			out[i].Warehouses = append(out[i].Warehouses, w)
		}
	}
	return out, nil
}

// CreateBranch adds a site.
func (s *Store) CreateBranch(ctx context.Context, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, ErrMissingRequired
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO branches (name, created_at) VALUES (?, ?)`, name, unix(s.now()))
	if isUnique(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const warehouseCols = `w.id, w.branch_id, b.name, w.code, w.name, w.active, w.created_at,
	(SELECT COUNT(*) FROM bins x WHERE x.warehouse_id = w.id AND x.active = 1)`

func scanWarehouse(sc interface{ Scan(...any) error }) (Warehouse, error) {
	var w Warehouse
	var created int64
	err := sc.Scan(&w.ID, &w.BranchID, &w.BranchName, &w.Code, &w.Name, &w.Active, &created, &w.BinCount)
	w.CreatedAt = fromUnix(created)
	return w, err
}

// ListWarehouses returns warehouses ordered by code.
func (s *Store) ListWarehouses(ctx context.Context, activeOnly bool) ([]Warehouse, error) {
	q := `SELECT ` + warehouseCols + ` FROM warehouses w JOIN branches b ON b.id = w.branch_id`
	if activeOnly {
		q += ` WHERE w.active = 1`
	}
	q += ` ORDER BY w.code COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Warehouse
	for rows.Next() {
		w, err := scanWarehouse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWarehouse loads one warehouse.
func (s *Store) GetWarehouse(ctx context.Context, id int64) (Warehouse, error) {
	w, err := scanWarehouse(s.DB.QueryRowContext(ctx,
		`SELECT `+warehouseCols+` FROM warehouses w JOIN branches b ON b.id = w.branch_id WHERE w.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

// CreateWarehouse adds a warehouse and its GENEL bin.
func (s *Store) CreateWarehouse(ctx context.Context, branchID int64, code, name string) (int64, error) {
	code, name = NormalizeCode(code), strings.TrimSpace(name)
	if code == "" || name == "" || branchID == 0 {
		return 0, ErrMissingRequired
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := unix(s.now())
		res, err := tx.ExecContext(ctx, `INSERT INTO warehouses (branch_id, code, name, created_at) VALUES (?, ?, ?, ?)`,
			branchID, code, name, now)
		if isUnique(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bins (warehouse_id, code, created_at) VALUES (?, ?, ?)`, id, DefaultBinCode, now)
		return err
	})
	return id, err
}

// SetWarehouseActive hides or restores a warehouse. Stock is kept.
func (s *Store) SetWarehouseActive(ctx context.Context, id int64, active bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE warehouses SET active = ? WHERE id = ?`, active, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// ListBins returns the active bins of a warehouse ordered by code, with
// GENEL first.
func (s *Store) ListBins(ctx context.Context, warehouseID int64) ([]Bin, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT x.id, x.warehouse_id, w.code, x.code, x.active
		FROM bins x JOIN warehouses w ON w.id = x.warehouse_id
		WHERE (? = 0 OR x.warehouse_id = ?) AND x.active = 1
		ORDER BY w.code COLLATE NOCASE, x.code <> ?, x.code COLLATE NOCASE`,
		warehouseID, warehouseID, DefaultBinCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bin
	for rows.Next() {
		var b Bin
		if err := rows.Scan(&b.ID, &b.WarehouseID, &b.WarehouseCode, &b.Code, &b.Active); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateBins adds bins by code; existing codes are skipped. It returns how
// many were created.
func (s *Store) CreateBins(ctx context.Context, warehouseID int64, codes []string) (int, error) {
	created := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouses WHERE id = ?`, warehouseID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		now := unix(s.now())
		for _, c := range codes {
			c = NormalizeCode(c)
			if c == "" || len(c) > 32 {
				continue
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO bins (warehouse_id, code, created_at) VALUES (?, ?, ?)
				ON CONFLICT (warehouse_id, code) DO UPDATE SET active = 1 WHERE active = 0`, warehouseID, c, now)
			if err != nil {
				return err
			}
			if k, _ := res.RowsAffected(); k > 0 {
				created++
			}
		}
		return nil
	})
	return created, err
}

// FindBin resolves a scanned bin code inside a warehouse.
func (s *Store) FindBin(ctx context.Context, warehouseID int64, code string) (Bin, error) {
	return findBin(ctx, s.DB, warehouseID, code)
}

func findBin(ctx context.Context, q queryer, warehouseID int64, code string) (Bin, error) {
	var b Bin
	err := q.QueryRowContext(ctx, `
		SELECT x.id, x.warehouse_id, w.code, x.code, x.active
		FROM bins x JOIN warehouses w ON w.id = x.warehouse_id
		WHERE x.warehouse_id = ? AND x.code = ? AND x.active = 1`, warehouseID, NormalizeCode(code)).
		Scan(&b.ID, &b.WarehouseID, &b.WarehouseCode, &b.Code, &b.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrBinNotFound
	}
	return b, err
}

// GetBin loads a bin by id.
func (s *Store) GetBin(ctx context.Context, id int64) (Bin, error) {
	return getBin(ctx, s.DB, id)
}

func getBin(ctx context.Context, q queryer, id int64) (Bin, error) {
	var b Bin
	err := q.QueryRowContext(ctx, `
		SELECT x.id, x.warehouse_id, w.code, x.code, x.active
		FROM bins x JOIN warehouses w ON w.id = x.warehouse_id WHERE x.id = ?`, id).
		Scan(&b.ID, &b.WarehouseID, &b.WarehouseCode, &b.Code, &b.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrBinNotFound
	}
	return b, err
}

// DefaultBin returns the GENEL bin of a warehouse.
func (s *Store) DefaultBin(ctx context.Context, warehouseID int64) (Bin, error) {
	return findBin(ctx, s.DB, warehouseID, DefaultBinCode)
}

// BinContents lists products with non-zero stock in a bin.
func (s *Store) BinContents(ctx context.Context, binID int64) ([]BinStock, error) {
	return s.binStock(ctx, `m.bin_id = ?`, binID)
}

// ProductBins lists bins holding a product, optionally in one warehouse.
func (s *Store) ProductBins(ctx context.Context, productID, warehouseID int64) ([]BinStock, error) {
	if warehouseID == 0 {
		return s.binStock(ctx, `m.product_id = ?`, productID)
	}
	return s.binStock(ctx, `m.product_id = ? AND x.warehouse_id = ?`, productID, warehouseID)
}

func (s *Store) binStock(ctx context.Context, where string, args ...any) ([]BinStock, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT x.id, w.code || '/' || x.code, x.warehouse_id, p.id, p.sku, p.name, p.unit, SUM(m.qty)
		FROM movements m
		JOIN bins x ON x.id = m.bin_id
		JOIN warehouses w ON w.id = x.warehouse_id
		JOIN products p ON p.id = m.product_id
		WHERE `+where+`
		GROUP BY m.bin_id, m.product_id
		HAVING SUM(m.qty) <> 0
		ORDER BY w.code COLLATE NOCASE, x.code COLLATE NOCASE, p.name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BinStock
	for rows.Next() {
		var b BinStock
		if err := rows.Scan(&b.BinID, &b.BinCode, &b.WarehouseID, &b.ProductID, &b.SKU, &b.Name, &b.Unit, &b.Qty); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// binQty is the on-hand quantity of a product in one bin.
func binQty(ctx context.Context, q queryer, binID, productID int64) (Qty, error) {
	var v Qty
	err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(qty), 0) FROM movements WHERE bin_id = ? AND product_id = ?`,
		binID, productID).Scan(&v)
	return v, err
}

// warehouseQty is the on-hand quantity of a product in one warehouse.
func warehouseQty(ctx context.Context, q queryer, warehouseID, productID int64) (Qty, error) {
	var v Qty
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(m.qty), 0) FROM movements m JOIN bins x ON x.id = m.bin_id
		WHERE x.warehouse_id = ? AND m.product_id = ?`, warehouseID, productID).Scan(&v)
	return v, err
}

// UserWarehouses returns the warehouses a user may work in. Admins get all
// active warehouses.
func (s *Store) UserWarehouses(ctx context.Context, userID int64, isAdmin bool) ([]Warehouse, error) {
	all, err := s.ListWarehouses(ctx, true)
	if err != nil || isAdmin {
		return all, err
	}
	allowed, err := s.userWarehouseIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []Warehouse
	for _, w := range all {
		if allowed[w.ID] {
			out = append(out, w)
		}
	}
	return out, nil
}

func (s *Store) userWarehouseIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT warehouse_id FROM user_warehouses WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// CanUseWarehouse reports whether a user may work in a warehouse.
func (s *Store) CanUseWarehouse(ctx context.Context, u User, warehouseID int64) (bool, error) {
	ws, err := s.UserWarehouses(ctx, u.ID, u.IsAdmin)
	if err != nil {
		return false, err
	}
	for _, w := range ws {
		if w.ID == warehouseID {
			return true, nil
		}
	}
	return false, nil
}

// SetUserWarehouses replaces a user's warehouse access.
func (s *Store) SetUserWarehouses(ctx context.Context, userID int64, warehouseIDs []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_warehouses WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, id := range warehouseIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO user_warehouses (user_id, warehouse_id) SELECT ?, id FROM warehouses WHERE id = ?`,
				userID, id); err != nil {
				return err
			}
		}
		return nil
	})
}
