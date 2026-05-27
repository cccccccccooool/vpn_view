// user_handler.go 实现用户管理相关的 HTTP 处理器，包括用户的增删改查。
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

// UserHandler 处理用户管理相关的 HTTP 请求。
type UserHandler struct {
	userSvc    *service.UserService
	trafficSvc *service.TrafficService
}

// NewUserHandler 创建 UserHandler 实例。
func NewUserHandler(userSvc *service.UserService, trafficSvc *service.TrafficService) *UserHandler {
	return &UserHandler{userSvc: userSvc, trafficSvc: trafficSvc}
}

// ListUsers 处理 GET /api/users 请求，返回所有用户列表。
// 同时将实时速度（上行/下行）合并到每个用户的响应中。
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userSvc.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	speeds := h.trafficSvc.GetUserSpeeds()
	for _, user := range users {
		if speed, ok := speeds[user.ID]; ok {
			user.SpeedUp = speed[0]
			user.SpeedDown = speed[1]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// CreateUser 处理 POST /api/users 请求，创建新用户。
// 支持自动生成 ID：优先使用请求体中的 id，其次取 credentials 中的 uuid，
// 如果都为空则使用随机生成的 hex ID。
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
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Credentials == nil {
		req.Credentials = map[string]string{}
	}
	if req.ID == "" {
		req.ID = strings.TrimSpace(req.Credentials["uuid"])
	}
	if req.ID == "" {
		req.ID = randomID()
	}
	if req.Name == "" {
		req.Name = req.ID
	}

	if err := h.userSvc.CreateUser(r.Context(), req.ID, req.Name, req.Credentials, req.Quota, req.SpeedLimitUp, req.SpeedLimitDown, req.ExpireAt); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "id": req.ID})
}

// UpdateUser 处理 PATCH /api/users/{id} 请求，部分更新用户信息。
// 仅更新请求体中非 nil 的字段，支持将 expire_at 设置为 null 来清除过期时间。
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
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
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.userSvc.GetUser(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

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
	expireAt := user.ExpireAt
	if len(req.ExpireAt) > 0 {
		if string(req.ExpireAt) == "null" {
			expireAt = nil
		} else {
			var t time.Time
			if err := json.Unmarshal(req.ExpireAt, &t); err != nil {
				writeError(w, http.StatusBadRequest, "invalid expire_at")
				return
			}
			expireAt = &t
		}
	}

	if err := h.userSvc.UpdateUser(r.Context(), id, name, quota, speedUp, speedDown, expireAt); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteUser 处理 DELETE /api/users/{id} 请求，删除指定用户。
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
		return
	}
	if err := h.userSvc.DeleteUser(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// randomID 生成 32 位随机十六进制字符串作为用户 ID；
// 若随机数生成失败则回退到时间戳格式。
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b[:])
}
