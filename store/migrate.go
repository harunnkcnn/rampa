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
`}, {Name: "core_002_sites_bins", SQL: `
CREATE TABLE branches (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
	created_at INTEGER NOT NULL
);

CREATE TABLE warehouses (
	id         INTEGER PRIMARY KEY,
	branch_id  INTEGER NOT NULL REFERENCES branches(id),
	code       TEXT NOT NULL UNIQUE COLLATE NOCASE,
	name       TEXT NOT NULL,
	active     INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);

-- A bin (hücre) is a shelf position. Every warehouse has a GENEL bin for
-- stock that has not been put away yet.
CREATE TABLE bins (
	id           INTEGER PRIMARY KEY,
	warehouse_id INTEGER NOT NULL REFERENCES warehouses(id),
	code         TEXT NOT NULL COLLATE NOCASE,
	active       INTEGER NOT NULL DEFAULT 1,
	created_at   INTEGER NOT NULL,
	UNIQUE (warehouse_id, code)
);

INSERT INTO branches (id, name, created_at) VALUES (1, 'Merkez', CAST(strftime('%s','now') AS INTEGER));
INSERT INTO warehouses (id, branch_id, code, name, created_at) VALUES (1, 1, '01', 'Ana depo', CAST(strftime('%s','now') AS INTEGER));
INSERT INTO bins (id, warehouse_id, code, created_at) VALUES (1, 1, 'GENEL', CAST(strftime('%s','now') AS INTEGER));

CREATE TABLE user_warehouses (
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	warehouse_id INTEGER NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, warehouse_id)
);
INSERT INTO user_warehouses (user_id, warehouse_id) SELECT id, 1 FROM users WHERE is_admin = 0;

ALTER TABLE users ADD COLUMN pin_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN warehouse_id INTEGER REFERENCES warehouses(id) ON DELETE SET NULL;
-- Sessions opened with a PIN may only use the handheld screens.
ALTER TABLE sessions ADD COLUMN terminal INTEGER NOT NULL DEFAULT 0;

ALTER TABLE products ADD COLUMN barcode TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX products_barcode ON products(barcode) WHERE barcode <> '';

ALTER TABLE shipments ADD COLUMN warehouse_id INTEGER REFERENCES warehouses(id);
UPDATE shipments SET warehouse_id = 1;

CREATE TABLE receipts (
	id           INTEGER PRIMARY KEY,
	code         TEXT UNIQUE,
	warehouse_id INTEGER NOT NULL REFERENCES warehouses(id),
	supplier     TEXT NOT NULL,
	doc_no       TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
	created_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at   INTEGER NOT NULL,
	closed_at    INTEGER
);
CREATE INDEX receipts_status ON receipts(warehouse_id, status);

CREATE TABLE counts (
	id         INTEGER PRIMARY KEY,
	bin_id     INTEGER NOT NULL REFERENCES bins(id),
	status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','cancelled')),
	created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at INTEGER NOT NULL,
	done_at    INTEGER
);
CREATE UNIQUE INDEX counts_one_open ON counts(bin_id) WHERE status = 'open';

CREATE TABLE count_lines (
	count_id   INTEGER NOT NULL REFERENCES counts(id) ON DELETE CASCADE,
	product_id INTEGER NOT NULL REFERENCES products(id),
	qty        INTEGER NOT NULL CHECK (qty >= 0),
	PRIMARY KEY (count_id, product_id)
);

-- Movements are rebuilt to carry the bin and the new transfer kind. Nothing
-- references this table, so dropping it is safe.
CREATE TABLE movements_new (
	id             INTEGER PRIMARY KEY,
	product_id     INTEGER NOT NULL REFERENCES products(id),
	bin_id         INTEGER NOT NULL REFERENCES bins(id),
	kind           TEXT NOT NULL CHECK (kind IN ('in','out','adjust','transfer')),
	qty            INTEGER NOT NULL CHECK (qty <> 0),
	note           TEXT NOT NULL DEFAULT '',
	shipment_id    INTEGER REFERENCES shipments(id) ON DELETE SET NULL,
	receipt_id     INTEGER REFERENCES receipts(id) ON DELETE SET NULL,
	count_id       INTEGER REFERENCES counts(id) ON DELETE SET NULL,
	transfer_group INTEGER,
	user_id        INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at     INTEGER NOT NULL
);
INSERT INTO movements_new (id, product_id, bin_id, kind, qty, note, shipment_id, user_id, created_at)
	SELECT id, product_id, 1, kind, qty, note, shipment_id, user_id, created_at FROM movements;
DROP TABLE movements;
ALTER TABLE movements_new RENAME TO movements;
CREATE INDEX movements_product ON movements(product_id, created_at);
CREATE INDEX movements_created ON movements(created_at);
CREATE INDEX movements_bin ON movements(bin_id, product_id);

CREATE TABLE picks (
	id               INTEGER PRIMARY KEY,
	shipment_item_id INTEGER NOT NULL REFERENCES shipment_items(id) ON DELETE CASCADE,
	bin_id           INTEGER NOT NULL REFERENCES bins(id),
	qty              INTEGER NOT NULL CHECK (qty > 0),
	user_id          INTEGER REFERENCES users(id) ON DELETE SET NULL,
	created_at       INTEGER NOT NULL
);
CREATE INDEX picks_item ON picks(shipment_item_id);

CREATE TABLE packages (
	id          INTEGER PRIMARY KEY,
	shipment_id INTEGER NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
	seq         INTEGER NOT NULL,
	kind        TEXT NOT NULL CHECK (kind IN ('koli','palet')),
	status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
	created_at  INTEGER NOT NULL,
	UNIQUE (shipment_id, seq)
);

CREATE TABLE package_items (
	package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
	product_id INTEGER NOT NULL REFERENCES products(id),
	qty        INTEGER NOT NULL CHECK (qty > 0),
	PRIMARY KEY (package_id, product_id)
);
`},
}
