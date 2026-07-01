package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vpnview/internal/domain"
	"vpnview/internal/port"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (port.UserStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		credentials TEXT NOT NULL,
		core_id TEXT NOT NULL DEFAULT '',
		adapter_type TEXT NOT NULL DEFAULT '',
		upload INTEGER DEFAULT 0,
		download INTEGER DEFAULT 0,
		quota INTEGER DEFAULT 0,
		speed_limit_up INTEGER DEFAULT 0,
		speed_limit_down INTEGER DEFAULT 0,
		expire_at DATETIME,
		enabled BOOLEAN DEFAULT 1,
		created_at DATETIME NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize SQLite schema: %w", err)
	}
	if err := migrateUsersSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func migrateUsersSchema(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN core_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN adapter_type TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate users schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, user *domain.User) error {
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	q := `INSERT INTO users (
		id, name, credentials, core_id, adapter_type, upload, download, quota,
		speed_limit_up, speed_limit_down, expire_at, enabled, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, q,
		user.ID, user.Name, string(credsJSON), user.CoreID, user.AdapterType,
		user.Upload, user.Download, user.Quota, user.SpeedLimitUp, user.SpeedLimitDown,
		user.ExpireAt, user.Enabled, user.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.id") {
			return domain.ErrUserExists
		}
		return err
	}
	return nil
}

func (s *Store) GetByID(ctx context.Context, id string) (*domain.User, error) {
	q := `SELECT id, name, credentials, core_id, adapter_type, upload, download, quota,
		speed_limit_up, speed_limit_down, expire_at, enabled, created_at
		FROM users WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanUser(row)
}

func (s *Store) List(ctx context.Context) ([]*domain.User, error) {
	q := `SELECT id, name, credentials, core_id, adapter_type, upload, download, quota,
		speed_limit_up, speed_limit_down, expire_at, enabled, created_at
		FROM users ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) Update(ctx context.Context, user *domain.User) error {
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	q := `UPDATE users SET
		name = ?, credentials = ?, core_id = ?, adapter_type = ?, upload = ?, download = ?,
		quota = ?, speed_limit_up = ?, speed_limit_down = ?, expire_at = ?, enabled = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q,
		user.Name, string(credsJSON), user.CoreID, user.AdapterType, user.Upload, user.Download,
		user.Quota, user.SpeedLimitUp, user.SpeedLimitDown, user.ExpireAt, user.Enabled, user.ID,
	)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (s *Store) AddTraffic(ctx context.Context, id string, upload, download int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET upload = upload + ?, download = download + ? WHERE id = ?`, upload, download, id)
	return err
}

func (s *Store) GetTotalTraffic(ctx context.Context) (int64, int64, error) {
	var up, down int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(upload), 0), COALESCE(SUM(download), 0) FROM users`).Scan(&up, &down)
	return up, down, err
}

func (s *Store) Close() error {
	return s.db.Close()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*domain.User, error) {
	var u domain.User
	var credsJSON string
	err := row.Scan(
		&u.ID, &u.Name, &credsJSON, &u.CoreID, &u.AdapterType, &u.Upload, &u.Download,
		&u.Quota, &u.SpeedLimitUp, &u.SpeedLimitDown, &u.ExpireAt, &u.Enabled, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(credsJSON), &u.Credentials); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}
	if u.Credentials == nil {
		u.Credentials = map[string]string{}
	}
	return &u, nil
}
