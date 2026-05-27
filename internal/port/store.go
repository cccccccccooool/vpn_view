// store.go 定义了用户数据持久化的存储接口。

package port

import (
	"context"

	"vpnview/internal/domain"
)

// UserStore 定义了用户数据的持久化存储接口（数据库抽象层）。
// 实现者需保证操作的原子性和并发安全。
type UserStore interface {
	// Create 创建一个新用户记录。若用户 ID 已存在，应返回 ErrUserExists。
	Create(ctx context.Context, user *domain.User) error
	// GetByID 根据用户 ID 查询用户信息。若不存在，应返回 ErrUserNotFound。
	GetByID(ctx context.Context, id string) (*domain.User, error)
	// List 返回所有用户列表。
	List(ctx context.Context) ([]*domain.User, error)
	// Update 更新已有用户的信息（全量覆盖）。
	Update(ctx context.Context, user *domain.User) error
	// Delete 根据用户 ID 删除用户记录。
	Delete(ctx context.Context, id string) error
	// AddTraffic 为指定用户累加上传和下载流量（增量更新）。
	AddTraffic(ctx context.Context, id string, upload, download int64) error
	// GetTotalTraffic 获取所有用户的累计上传和下载流量总和。
	GetTotalTraffic(ctx context.Context) (upload, download int64, err error)
	// Close 关闭存储连接并释放资源。
	Close() error
}
