// ============================================================================
// 文件说明：internal/port/store.go
// 职责概览：定义了系统持久化数据存储的抽象接口（port.UserStore），即数据库访问层。
//           规定了用户账户元数据的 CRUD 操作，以及流量统计落库累计等操作，保证系统业务层
//           与底层的存储形式（如 SQLite、MySQL）解耦。
// ============================================================================

package port

import (
	"context"

	"vpnview/internal/domain"
)

// UserStore 定义了用于管理 VPN 用户账户、流量记录、限速指标持久化的标准存储抽象层。
// 所有具体数据库实现（如 SQLite 存储适配器）必须严格实现该接口，并保证并发读写的安全性。
type UserStore interface {
	// Create 新增一条用户元数据记录。如果给定的 User ID 已经重复存在，必须返回 ErrUserExists 错误。
	Create(ctx context.Context, user *domain.User) error

	// GetByID 根据用户唯一 ID 字段查询该用户的完整元数据。如果该用户 ID 并不存在，必须返回 ErrUserNotFound。
	GetByID(ctx context.Context, id string) (*domain.User, error)

	// List 全量加载数据库中存在的所有用户对象，用于自愈同步和列表展示。
	List(ctx context.Context) ([]*domain.User, error)

	// Update 对已有用户的数据模型进行全量更新（常用于修改密码、限速值、有效期等）。
	Update(ctx context.Context, user *domain.User) error

	// Delete 从数据库中物理删除对应 ID 的用户记录。
	Delete(ctx context.Context, id string) error

	// AddTraffic 增量向数据库中累加并更新该用户上传和下载的流量字节数（防止并发读写引发脏覆盖）。
	AddTraffic(ctx context.Context, id string, upload, download int64) error

	// GetTotalTraffic 累计计算并返回存储中所有用户累加后的总上传和总下载流量，用于系统全局看板。
	GetTotalTraffic(ctx context.Context) (upload, download int64, err error)

	// Close 安全地关闭底层持久化数据库的连接句柄，将内存写队列数据刷盘。
	Close() error
}
