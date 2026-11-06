package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration is a named, append-only schema change. Names must be unique
// across the core and every extension (prefix them, e.g. "pro_001_...").
type Migration struct {
	Name string
	SQL  string
}

// Migrate applies the migrations that have not run yet, in order.
func (s *Store) Migrate(ctx context.Context, ms []Migration) error {
	if _, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return err
	}
	for _, m := range ms {
		err := s.tx(ctx, func(tx *sql.Tx) error {
			var n int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, m.Name).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`, m.Name, unix(s.now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	return nil
}

// CoreMigrations is the schema of the open-source edition.
var CoreMigrations = []Migration{
	{Name: "core_001_init", SQL: `
CREATE TABLE users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	is_admin      INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf       TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE products (
	id         INTEGER PRIMARY KEY,
	sku        TEXT NOT NULL UNIQUE COLLATE NOCASE,
	name       TEXT NOT NULL,
	unit       TEXT NOT NULL DEFAULT 'adet',
	location   TEXT NOT NULL DEFAULT '',
	min_stock  INTEGER NOT NULL DEFAULT 0 CHECK (min_stock >= 0),
	weight_kg  REAL NOT NULL DEFAULT 0 CHECK (weight_kg >= 0),
	volume_m3  REAL NOT NULL DEFAULT 0 CHECK (volume_m3 >= 0),
	archived   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE shipments (
	id           INTEGER PRIMARY KEY,
	code         TEXT UNIQUE,
	customer     TEXT NOT NULL,
	plate        TEXT NOT NULL DEFAULT '',
	driver       TEXT NOT NULL DEFAULT '',
	note         TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'draft'
	             CHECK (status IN ('draft','loaded','delivered','cancelled')),
	created_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at   INTEGER NOT NULL,
	loaded_at    INTEGER,
	delivered_at INTEGER
);
CREATE INDEX shipments_status ON shipments(status, created_at);

CREATE TABLE shipment_items (
	id          INTEGER PRIMARY KEY,
	shipment_id INTEGER NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
	product_id  INTEGER NOT NULL REFERENCES products(id),
	qty         INTEGER NOT NULL CHECK (qty > 0),
	UNIQUE (shipment_id, product_id)
);

CREATE TABLE movements (
	id          INTEGER PRIMARY KEY,
	product_id  INTEGER NOT NULL REFERENCES products(id),
	kind        TEXT NOT NULL CHECK (kind IN ('in','out','adjust')),
	qty         INTEGER NOT NULL CHECK (qty <> 0),
	note        TEXT NOT NULL DEFAULT '',
	shipment_id INTEGER REFERENCES shipments(id) ON DELETE SET NULL,
	user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX movements_product ON movements(product_id, created_at);
CREATE INDEX movements_created ON movements(created_at);
`},
}
