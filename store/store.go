// Package store holds Rampa's SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver" // database/sql driver
	_ "github.com/ncruces/go-sqlite3/embed"  // bundled SQLite build
)

var (
	ErrNotFound        = errors.New("kayıt bulunamadı")
	ErrDuplicate       = errors.New("bu değer zaten kullanılıyor")
	ErrInvalidState    = errors.New("bu işlem kaydın mevcut durumunda yapılamaz")
	ErrNoChange        = errors.New("sayım mevcut stokla aynı, hareket oluşturulmadı")
	ErrNonPositive     = errors.New("miktar sıfırdan büyük olmalı")
	ErrEmptyShipment   = errors.New("sevkiyatta ürün yok")
	ErrArchivedProduct = errors.New("arşivlenmiş ürüne hareket girilemez")
	ErrMissingRequired = errors.New("zorunlu alanlar eksik")
	ErrBinNotFound     = errors.New("bu depoda böyle bir hücre yok")
	ErrUnknownCode     = errors.New("barkod ya da stok kodu tanınmadı")
	ErrWrongWarehouse  = errors.New("hücre bu işlemin deposuna ait değil")
	ErrNotOnShipment   = errors.New("bu ürün sevkiyatta yok")
	ErrOverPick        = errors.New("sevkiyattaki miktardan fazla okutuldu")
	ErrSameBin         = errors.New("kaynak ve hedef hücre aynı")
	ErrOpenCount       = errors.New("bu hücrede açık bir sayım var")
)

// InsufficientStockError reports which product would go negative.
type InsufficientStockError struct {
	SKU       string
	Name      string
	Available Qty
	Requested Qty
	Bin       string // set when the check was for one bin
}

func (e *InsufficientStockError) Error() string {
	where := ""
	if e.Bin != "" {
		where = " " + e.Bin + " hücresinde"
	}
	return fmt.Sprintf("%s (%s) için%s yeterli stok yok: mevcut %s, istenen %s",
		e.Name, e.SKU, where, e.Available, e.Requested)
}

// Store wraps the database handle.
type Store struct {
	DB  *sql.DB
	now func() time.Time
}

// Open opens (or creates) the SQLite database at path.
func Open(path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(normal)")
	q.Set("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("veritabanı açılamadı: %w", err)
	}
	return &Store{DB: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// SetClock overrides the time source (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Now returns the store's current time.
func (s *Store) Now() time.Time { return s.now() }

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isUnique(err error) bool {
	return errors.Is(err, sqlite3.CONSTRAINT_UNIQUE)
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(v int64) time.Time { return time.Unix(v, 0) }

func fromNullUnix(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}
