package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Product is a stock-keeping unit with its current on-hand quantity.
type Product struct {
	ID        int64
	SKU       string
	Barcode   string
	Name      string
	Unit      string
	Location  string
	MinStock  Qty
	WeightKg  float64 // per unit
	VolumeM3  float64 // per unit
	Archived  bool
	CreatedAt time.Time
	Stock     Qty
}

// Low reports whether stock is at or below the minimum level.
func (p Product) Low() bool { return p.MinStock > 0 && p.Stock <= p.MinStock }

// ProductFilter narrows ListProducts.
type ProductFilter struct {
	Query           string
	LowOnly         bool
	IncludeArchived bool
	WarehouseID     int64 // 0 = stock across all warehouses
	Limit           int
}

const productCols = `
	p.id, p.sku, p.barcode, p.name, p.unit, p.location, p.min_stock, p.weight_kg, p.volume_m3,
	p.archived, p.created_at, COALESCE(s.stock, 0)`

const stockJoin = `
	LEFT JOIN (SELECT product_id, SUM(qty) AS stock FROM movements GROUP BY product_id) s
	       ON s.product_id = p.id`

// scopedStockJoin limits stock to one warehouse; its single argument is the
// warehouse id.
const scopedStockJoin = `
	LEFT JOIN (SELECT m.product_id, SUM(m.qty) AS stock FROM movements m
	           JOIN bins x ON x.id = m.bin_id WHERE x.warehouse_id = ? GROUP BY m.product_id) s
	       ON s.product_id = p.id`

func scanProduct(sc interface{ Scan(...any) error }) (Product, error) {
	var p Product
	var created int64
	err := sc.Scan(&p.ID, &p.SKU, &p.Barcode, &p.Name, &p.Unit, &p.Location, &p.MinStock,
		&p.WeightKg, &p.VolumeM3, &p.Archived, &created, &p.Stock)
	p.CreatedAt = fromUnix(created)
	return p, err
}

// ListProducts returns products ordered by name.
func (s *Store) ListProducts(ctx context.Context, f ProductFilter) ([]Product, error) {
	var where []string
	var args []any
	if !f.IncludeArchived {
		where = append(where, "p.archived = 0")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		where = append(where, `(p.sku LIKE ? ESCAPE '\' OR p.name LIKE ? ESCAPE '\' OR p.location LIKE ? ESCAPE '\' OR p.barcode = ?)`)
		args = append(args, like, like, like, NormalizeCode(q))
	}
	if f.LowOnly {
		where = append(where, "p.min_stock > 0 AND COALESCE(s.stock, 0) <= p.min_stock")
	}
	join := stockJoin
	if f.WarehouseID != 0 {
		join = scopedStockJoin
		args = append([]any{f.WarehouseID}, args...)
	}
	query := "SELECT " + productCols + " FROM products p" + join
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY p.name COLLATE NOCASE"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProduct loads one product with its stock.
func (s *Store) GetProduct(ctx context.Context, id int64) (Product, error) {
	return getProduct(ctx, s.DB, id)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetProductIn loads a product with its stock in one warehouse (0 = all).
func (s *Store) GetProductIn(ctx context.Context, id, warehouseID int64) (Product, error) {
	if warehouseID == 0 {
		return getProduct(ctx, s.DB, id)
	}
	p, err := scanProduct(s.DB.QueryRowContext(ctx,
		"SELECT "+productCols+" FROM products p"+scopedStockJoin+" WHERE p.id = ?", warehouseID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func getProduct(ctx context.Context, q queryer, id int64) (Product, error) {
	p, err := scanProduct(q.QueryRowContext(ctx,
		"SELECT "+productCols+" FROM products p"+stockJoin+" WHERE p.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// FindProductByCode resolves a scanned barcode or a typed stock code.
func (s *Store) FindProductByCode(ctx context.Context, code string) (Product, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Product{}, ErrUnknownCode
	}
	p, err := scanProduct(s.DB.QueryRowContext(ctx, "SELECT "+productCols+" FROM products p"+stockJoin+`
		WHERE p.archived = 0 AND (p.barcode = ? OR p.sku = ?)
		ORDER BY p.barcode = ? DESC LIMIT 1`, NormalizeCode(code), strings.ToUpper(code), NormalizeCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrUnknownCode
	}
	return p, err
}

func normalizeProduct(p *Product) error {
	p.SKU = strings.ToUpper(strings.TrimSpace(p.SKU))
	p.Barcode = NormalizeCode(p.Barcode)
	p.Name = strings.TrimSpace(p.Name)
	p.Unit = strings.TrimSpace(p.Unit)
	p.Location = strings.TrimSpace(p.Location)
	if p.Unit == "" {
		p.Unit = "adet"
	}
	if p.SKU == "" || p.Name == "" {
		return ErrMissingRequired
	}
	if p.MinStock < 0 || p.WeightKg < 0 || p.VolumeM3 < 0 {
		return ErrInvalidQty
	}
	return nil
}

// CreateProduct inserts a product and returns its id.
func (s *Store) CreateProduct(ctx context.Context, p Product) (int64, error) {
	if err := normalizeProduct(&p); err != nil {
		return 0, err
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO products (sku, barcode, name, unit, location, min_stock, weight_kg, volume_m3, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SKU, p.Barcode, p.Name, p.Unit, p.Location, p.MinStock, p.WeightKg, p.VolumeM3, unix(s.now()))
	if isUnique(err) {
		return 0, duplicateErr(err)
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateProduct saves editable fields.
func (s *Store) UpdateProduct(ctx context.Context, p Product) error {
	if err := normalizeProduct(&p); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE products SET sku = ?, barcode = ?, name = ?, unit = ?, location = ?, min_stock = ?,
		       weight_kg = ?, volume_m3 = ?
		WHERE id = ?`,
		p.SKU, p.Barcode, p.Name, p.Unit, p.Location, p.MinStock, p.WeightKg, p.VolumeM3, p.ID)
	if isUnique(err) {
		return duplicateErr(err)
	}
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// SetProductArchived hides or restores a product. History is kept.
func (s *Store) SetProductArchived(ctx context.Context, id int64, archived bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE products SET archived = ? WHERE id = ?`, archived, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// ErrDuplicateBarcode is returned when another product has the barcode.
var ErrDuplicateBarcode = errors.New("bu barkod başka bir üründe kayıtlı")

func duplicateErr(err error) error {
	if strings.Contains(err.Error(), "barcode") {
		return ErrDuplicateBarcode
	}
	return ErrDuplicate
}

func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
