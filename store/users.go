package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// User is an account that can sign in to the panel.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	PINHash      string
	IsAdmin      bool
	CreatedAt    time.Time
}

// CountUsers is used to decide whether first-run setup is needed.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts an account. The caller hashes the password.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, admin bool) (int64, error) {
	username = strings.TrimSpace(username)
	if username == "" || passwordHash == "" {
		return 0, ErrMissingRequired
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, admin, unix(s.now()))
	if isUnique(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListUsers returns all accounts.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, username, '', CASE WHEN pin_hash <> '' THEN 'x' ELSE '' END, is_admin, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// HasPIN reports whether the user can sign in on a handheld terminal.
func (u User) HasPIN() bool { return u.PINHash != "" }

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	var created int64
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.PINHash, &u.IsAdmin, &created)
	u.CreatedAt = fromUnix(created)
	return u, err
}

// GetUserByUsername loads an account including its password hash.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `
		SELECT id, username, password_hash, pin_hash, is_admin, created_at FROM users WHERE username = ?`,
		strings.TrimSpace(username)))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// GetUser loads an account by id including its password hash.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `
		SELECT id, username, password_hash, pin_hash, is_admin, created_at FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// SetPasswordHash replaces a user's password and ends their other sessions.
func (s *Store) SetPasswordHash(ctx context.Context, userID int64, hash, keepTokenHash string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
		if err != nil {
			return err
		}
		if err := mustAffect(res); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepTokenHash)
		return err
	})
}

// SetPINHash sets or clears (empty hash) a user's terminal PIN.
func (s *Store) SetPINHash(ctx context.Context, userID int64, hash string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET pin_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// Session is a signed-in browser.
type Session struct {
	TokenHash   string
	CSRF        string
	ExpiresAt   time.Time
	WarehouseID int64 // 0 = all warehouses (panel) or not chosen yet (terminal)
	Terminal    bool  // opened with a PIN; handheld screens only
	User        User
}

// SetSessionWarehouse stores the warehouse a session works in.
func (s *Store) SetSessionWarehouse(ctx context.Context, tokenHash string, warehouseID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET warehouse_id = ? WHERE token_hash = ?`, nullID(warehouseID), tokenHash)
	return err
}

// CreateSession stores a hashed session token.
func (s *Store) CreateSession(ctx context.Context, tokenHash, csrf string, userID int64, expires time.Time, terminal bool) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, csrf, expires_at, terminal) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, csrf, unix(expires), terminal)
	return err
}

// GetSession returns a non-expired session with its user.
func (s *Store) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	var ss Session
	var exp, created int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT s.token_hash, s.csrf, s.expires_at, COALESCE(s.warehouse_id, 0), s.terminal, u.id, u.username, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, unix(s.now())).
		Scan(&ss.TokenHash, &ss.CSRF, &exp, &ss.WarehouseID, &ss.Terminal, &ss.User.ID, &ss.User.Username, &ss.User.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return ss, ErrNotFound
	}
	ss.ExpiresAt = fromUnix(exp)
	ss.User.CreatedAt = fromUnix(created)
	return ss, err
}

// DeleteSession signs a browser out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// PurgeExpiredSessions removes stale rows.
func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, unix(s.now()))
	return err
}

// Setting returns a stored value, or def when unset.
func (s *Store) Setting(ctx context.Context, key, def string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v, err
}

// SetSetting upserts a value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetSettingIfAbsent stores value only if key is unset and returns the
// effective value.
func (s *Store) SetSettingIfAbsent(ctx context.Context, key, value string) (string, error) {
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`, key, value); err != nil {
		return "", err
	}
	return s.Setting(ctx, key, value)
}
