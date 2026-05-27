// Package sqlite 提供了基于 SQLite 的用户持久化存储实现。
// SQLite-backed persistent user store implementation.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vpnview/internal/domain"
	"vpnview/internal/port"

	_ "modernc.org/sqlite"
)

// Store 是基于 SQLite 数据库的 port.UserStore 实现。
// 使用 modernc.org/sqlite 纯 Go 驱动，无需 CGO。
type Store struct {
	db *sql.DB
}

// New 创建一个新的 SQLite 用户存储并初始化表结构。
func New(dbPath string) (port.UserStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 如果表不存在则创建
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		credentials TEXT NOT NULL,
		upload INTEGER DEFAULT 0,
		download INTEGER DEFAULT 0,
		quota INTEGER DEFAULT 0,
		speed_limit_up INTEGER DEFAULT 0,
		speed_limit_down INTEGER DEFAULT 0,
		expire_at DATETIME,
		enabled BOOLEAN DEFAULT 1,
		created_at DATETIME NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Create 将新用户插入数据库。若 ID 已存在则返回 domain.ErrUserExists。
func (s *Store) Create(ctx context.Context, user *domain.User) error {
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	q := `INSERT INTO users (id, name, credentials, upload, download, quota, speed_limit_up, speed_limit_down, expire_at, enabled, created_at)
	      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, q,
		user.ID, user.Name, string(credsJSON), user.Upload, user.Download, user.Quota,
		user.SpeedLimitUp, user.SpeedLimitDown, user.ExpireAt, user.Enabled, user.CreatedAt)
	if err != nil {
		// 检查唯一约束冲突
		if err.Error() == "UNIQUE constraint failed: users.id" {
			return domain.ErrUserExists
		}
		return err
	}
	return nil
}

// GetByID 根据用户 ID 查询单个用户。未找到时返回 domain.ErrUserNotFound。
func (s *Store) GetByID(ctx context.Context, id string) (*domain.User, error) {
	q := `SELECT id, name, credentials, upload, download, quota, speed_limit_up, speed_limit_down, expire_at, enabled, created_at
	      FROM users WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanUser(row)
}

// List 返回所有用户列表，按创建时间倒序排列。
func (s *Store) List(ctx context.Context) ([]*domain.User, error) {
	q := `SELECT id, name, credentials, upload, download, quota, speed_limit_up, speed_limit_down, expire_at, enabled, created_at
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

// Update 更新指定用户的所有可变字段。未找到用户时返回 domain.ErrUserNotFound。
func (s *Store) Update(ctx context.Context, user *domain.User) error {
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	q := `UPDATE users SET
		name = ?, credentials = ?, upload = ?, download = ?, quota = ?,
		speed_limit_up = ?, speed_limit_down = ?, expire_at = ?, enabled = ?
		WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q,
		user.Name, string(credsJSON), user.Upload, user.Download, user.Quota,
		user.SpeedLimitUp, user.SpeedLimitDown, user.ExpireAt, user.Enabled, user.ID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// Delete 根据 ID 删除用户。未找到用户时返回 domain.ErrUserNotFound。
func (s *Store) Delete(ctx context.Context, id string) error {
	q := `DELETE FROM users WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// AddTraffic 为指定用户累加上传和下载流量计数。
func (s *Store) AddTraffic(ctx context.Context, id string, upload, download int64) error {
	q := `UPDATE users SET upload = upload + ?, download = download + ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, upload, download, id)
	return err
}

// GetTotalTraffic 返回所有用户的上传和下载流量总和。
func (s *Store) GetTotalTraffic(ctx context.Context) (int64, int64, error) {
	q := `SELECT COALESCE(SUM(upload), 0), COALESCE(SUM(download), 0) FROM users`
	var up, down int64
	err := s.db.QueryRowContext(ctx, q).Scan(&up, &down)
	return up, down, err
}

// Close 关闭底层 SQLite 数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// 用于将行扫描到 User 对象的辅助接口
type scanner interface {
	Scan(dest ...any) error
}

// scanUser 从数据库行中扫描并反序列化一个 User 对象。
// sql.ErrNoRows 会被转换为 domain.ErrUserNotFound。
func scanUser(row scanner) (*domain.User, error) {
	var u domain.User
	var credsJSON string
	err := row.Scan(
		&u.ID, &u.Name, &credsJSON, &u.Upload, &u.Download, &u.Quota,
		&u.SpeedLimitUp, &u.SpeedLimitDown, &u.ExpireAt, &u.Enabled, &u.CreatedAt,
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
	return &u, nil
}
