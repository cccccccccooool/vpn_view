// ============================================================================
// 文件说明：internal/adapter/store/sqlite/store.go
// 职责概览：实现基于 SQLite 关系型数据库的用户数据持久化适配器（Store）。
//           对外实现 port.UserStore 数据库端口接口。
//           底层使用纯 Go 编写的驱动 `modernc.org/sqlite`，彻底免除 CGO 依赖，
//           保障了多平台跨平台（Windows, Linux, macOS）极速编译部署。
//           提供完整的高并发行扫描反序列化、增量原子累加流量等安全数据操作。
// ============================================================================

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vpnview/internal/domain"
	"vpnview/internal/port"

	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite 驱动，无 CGO 痛点
)

// Store 基于 SQLite 数据库的本地用户持久化物理存储介质适配器。
type Store struct {
	db *sql.DB // 嵌入式数据库长连接句柄
}

// New 初始化并创建一个新的 SQLite 用户存储适配器，自动创建数据库文件并初始化表结构模式。
func New(dbPath string) (port.UserStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}

	// 初始化数据表模式（Schema）。创建 users 表记录 VPN 账户核心数据
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
		return nil, fmt.Errorf("初始化 SQLite 表结构 Schema 失败: %w", err)
	}

	return &Store{db: db}, nil
}

// Create 往底层数据表中插入一条全新的用户数据。
// 事务校验：如果用户 ID 重复发生冲突，自动过滤并返回标准的 domain.ErrUserExists 领域错误。
func (s *Store) Create(ctx context.Context, user *domain.User) error {
	// 将用户凭证 Credentials 字典序列化为 JSON 字符串保存入库
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("序列化用户 Credentials 失败: %w", err)
	}

	q := `INSERT INTO users (id, name, credentials, upload, download, quota, speed_limit_up, speed_limit_down, expire_at, enabled, created_at)
	      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, q,
		user.ID, user.Name, string(credsJSON), user.Upload, user.Download, user.Quota,
		user.SpeedLimitUp, user.SpeedLimitDown, user.ExpireAt, user.Enabled, user.CreatedAt)
	if err != nil {
		// 校验 SQLite 唯一主键约束报错
		if err.Error() == "UNIQUE constraint failed: users.id" {
			return domain.ErrUserExists
		}
		return err
	}
	return nil
}

// GetByID 根据用户唯一 ID 字段精确加载单条用户行，并扫描还原为 domain.User 结构。
// 报错对齐：如果行不存在，自动捕获并转换返回 domain.ErrUserNotFound。
func (s *Store) GetByID(ctx context.Context, id string) (*domain.User, error) {
	q := `SELECT id, name, credentials, upload, download, quota, speed_limit_up, speed_limit_down, expire_at, enabled, created_at
	      FROM users WHERE id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanUser(row)
}

// List 全量加载数据库中存在的全部用户对象，结果按创建时间降序倒序输出。
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

// Update 覆盖式更新已有用户元数据记录的可变属性。
// 校验报错：若 RowsAffected 影响行数为 0，说明修改的用户 ID 根本不存在，返回 domain.ErrUserNotFound。
func (s *Store) Update(ctx context.Context, user *domain.User) error {
	credsJSON, err := json.Marshal(user.Credentials)
	if err != nil {
		return fmt.Errorf("序列化用户 Credentials 失败: %w", err)
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

// Delete 从数据库中物理抹除对应 ID 的用户账户。若无对应行，返回 domain.ErrUserNotFound。
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

// AddTraffic 增量更新流量字段。
// 使用 SQL 原生累加机制（upload = upload + ?），保证多线程高频定时计算并发写时不会发生脏写和覆盖丢失。
func (s *Store) AddTraffic(ctx context.Context, id string, upload, download int64) error {
	q := `UPDATE users SET upload = upload + ?, download = download + ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, upload, download, id)
	return err
}

// GetTotalTraffic 返回全员累积消耗的总上传、总下载流量字节总和，用于全局大屏看板。
func (s *Store) GetTotalTraffic(ctx context.Context) (int64, int64, error) {
	q := `SELECT COALESCE(SUM(upload), 0), COALESCE(SUM(download), 0) FROM users`
	var up, down int64
	err := s.db.QueryRowContext(ctx, q).Scan(&up, &down)
	return up, down, err
}

// Close 关闭与 SQLite 轻量嵌入式数据库的连接句柄，将日志刷盘。
func (s *Store) Close() error {
	return s.db.Close()
}

// scanner 用于抽象统一 sql.Row 和 sql.Rows 扫描读取的辅助匹配接口。
type scanner interface {
	Scan(dest ...any) error
}

// scanUser 抓取单行提取，执行凭证反序列化。对 sql.ErrNoRows 翻译为 domain.ErrUserNotFound。
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

	// 还原 credentials 字典
	if err := json.Unmarshal([]byte(credsJSON), &u.Credentials); err != nil {
		return nil, fmt.Errorf("反序列化凭证 Credentials 失败: %w", err)
	}
	return &u, nil
}
