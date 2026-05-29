// ============================================================================
// 文件说明：internal/handler/user_handler.go
// 职责概览：实现用户管理控制的 HTTP 接口处理器（UserHandler）。
//           包含获取用户列表、新增用户、更新修改用户元数据、删除用户的 API 逻辑。
//           在拉取用户列表时，会自动结合 TrafficService，将内存中最新的瞬时网速数据
//           （上传速度、下载速度）拼装并返回，满足前端页面的实时动态呈现。
//           ID 自动生成：在创建用户时，支持灵活匹配自定义 id，其次可继承 uuid，
//           最后兜底由 32 位随机安全 Hex 字符自动作为用户 ID 生成。
// ============================================================================

package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vpnview/internal/service"
)

// UserHandler 负责代理并响应所有用户相关的 CRUD HTTP 接口请求。
type UserHandler struct {
	userSvc    *service.UserService    // 核心用户账户业务服务
	trafficSvc *service.TrafficService // 实时网速提取服务
}

// NewUserHandler 实例化并创建一个 UserHandler 接口处理器。
func NewUserHandler(userSvc *service.UserService, trafficSvc *service.TrafficService) *UserHandler {
	return &UserHandler{userSvc: userSvc, trafficSvc: trafficSvc}
}

// ListUsers 响应 GET /api/users 请求。
// 从数据库全量载入所有用户元数据，并通过流量定时器拉取当前全员网速缓存，
// 覆写拼装 `SpeedUp` 和 `SpeedDown` 后以标准 JSON 数据回传。
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userSvc.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取用户列表失败")
		return
	}

	speeds := h.trafficSvc.GetUserSpeeds() // 抓取实时瞬时速度表
	for _, user := range users {
		if speed, ok := speeds[user.ID]; ok {
			user.SpeedUp = speed[0]   // 注入实时上行字节速度
			user.SpeedDown = speed[1] // 注入实时下行字节速度
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// CreateUser 响应 POST /api/users 请求，创建新的 VPN 账户。
// ID 选择逻辑：
//  1. 优先取请求体中的 ID 字符串。
//  2. 其次提取 Credentials 字典内的 `uuid` 字段。
//  3. 若均未配置，自动调用随机数生成一个 32 字符的安全 Hex 字符串唯一 ID。
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID             string            `json:"id"`
		Name           string            `json:"name"`
		Credentials    map[string]string `json:"credentials"`
		Quota          int64             `json:"quota"`
		SpeedLimitUp   int64             `json:"speed_limit_up"`
		SpeedLimitDown int64             `json:"speed_limit_down"`
		ExpireAt       *time.Time        `json:"expire_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求参数 JSON 解析格式错误")
		return
	}
	if req.Credentials == nil {
		req.Credentials = map[string]string{}
	}
	if req.ID == "" {
		req.ID = strings.TrimSpace(req.Credentials["uuid"])
	}
	if req.ID == "" {
		req.ID = randomID() // 随机兜底 ID
	}
	if req.Name == "" {
		req.Name = req.ID
	}

	// 委托服务层创建并同步底层 VPN 内核
	if err := h.userSvc.CreateUser(r.Context(), req.ID, req.Name, req.Credentials, req.Quota, req.SpeedLimitUp, req.SpeedLimitDown, req.ExpireAt); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "id": req.ID})
}

// UpdateUser 响应 PATCH /api/users/{id} 部分属性修改接口。
// 支持动态开启或禁用用户。更新时只覆写请求体中非 nil 的有效字段。
// 精巧处理：允许传 "null" 将 expire_at 有效期重置为永久有效。
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "用户 ID 参数缺失")
		return
	}

	var req struct {
		Name           *string         `json:"name"`
		Quota          *int64          `json:"quota"`
		SpeedLimitUp   *int64          `json:"speed_limit_up"`
		SpeedLimitDown *int64          `json:"speed_limit_down"`
		ExpireAt       json.RawMessage `json:"expire_at"`
		Enabled        *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求修改参数解析失败")
		return
	}

	user, err := h.userSvc.GetUser(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// 若 Enabled 变更，委托 SetEnabled 执行（同步阻断活动网络连接）
	if req.Enabled != nil && *req.Enabled != user.Enabled {
		if err := h.userSvc.SetEnabled(r.Context(), id, *req.Enabled); err != nil {
			writeDomainError(w, err)
			return
		}
		user.Enabled = *req.Enabled
	}

	name := user.Name
	if req.Name != nil {
		name = *req.Name
	}
	quota := user.Quota
	if req.Quota != nil {
		quota = *req.Quota
	}
	speedUp := user.SpeedLimitUp
	if req.SpeedLimitUp != nil {
		speedUp = *req.SpeedLimitUp
	}
	speedDown := user.SpeedLimitDown
	if req.SpeedLimitDown != nil {
		speedDown = *req.SpeedLimitDown
	}

	// 特殊处理到期期限清空与解析
	expireAt := user.ExpireAt
	if len(req.ExpireAt) > 0 {
		if string(req.ExpireAt) == "null" {
			expireAt = nil // 置空即为永久有效
		} else {
			var t time.Time
			if err := json.Unmarshal(req.ExpireAt, &t); err != nil {
				writeError(w, http.StatusBadRequest, "无效的 expire_at 有效期格式")
				return
			}
			expireAt = &t
		}
	}

	// 提交更新落库与同步
	if err := h.userSvc.UpdateUser(r.Context(), id, name, quota, speedUp, speedDown, expireAt); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteUser 响应 DELETE /api/users/{id} 销毁用户接口。
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "用户 ID 参数缺失")
		return
	}
	if err := h.userSvc.DeleteUser(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// randomID 生成 32 字符的安全 Hex 唯一哈希，用于兜底自动用户 ID 补齐。
// 如果底层系统熵源异常，安全退回使用当前纳秒时间戳作为 ID，保障极速响应。
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b[:])
}
