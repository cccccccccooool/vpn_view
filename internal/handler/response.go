// response.go 提供 HTTP 响应的工具函数，统一 JSON 格式和错误码映射。
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"vpnview/internal/domain"
)

// writeJSON 将 payload 以 JSON 格式写入 HTTP 响应，同时设置 Content-Type 和状态码。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError 返回统一格式的 JSON 错误响应 {"error": msg}。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeDomainError 将 domain 层的业务错误映射为对应的 HTTP 状态码并返回。
// ErrNotSupported → 501, ErrUserNotFound → 404, ErrUserExists → 409，其余 → 500。
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotSupported):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, domain.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, domain.ErrUserExists):
		writeError(w, http.StatusConflict, "user already exists")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
