// ============================================================================
// 文件说明：internal/handler/response.go
// 职责概览：提供统一的 HTTP RESTful API 响应的 JSON 序列化工具函数（response）。
//           规范化输出响应的 Content-Type 为 application/json，
//           提供通用的错误响应包装，并且负责将 domain 领域层业务哨兵错误
//           （如 ErrNotSupported, ErrUserNotFound, ErrUserExists）
//           高内聚地自动对齐翻译映射为符合标准的 HTTP 状态码（501, 404, 409, 500）。
// ============================================================================

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"vpnview/internal/domain"
)

// writeJSON 将指定的 Go 结构体 payload 格式化为 JSON 字符串并写回 HTTP 响应体中。
// 自动规范化追加 JSON 和 UTF-8 字符响应头。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError 快捷输出格式一致的统一标准 JSON 错误响应 {"error": msg}。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeDomainError 核心对齐层。将 domain 领域核心层跑出的 Sentinel 业务错误，
// 自动转换映射为正确的 Web 状态码返回，满足 RESTful API 规范：
//  - domain.ErrNotSupported     → 501 Not Implemented (适配器不支持操作)
//  - domain.ErrUserNotFound     → 404 Not Found (用户账户不存在)
//  - domain.ErrUserExists       → 409 Conflict (用户账户已存在，冲突)
//  - 其它所有非预期或内部系统异常 → 500 Internal Server Error (系统内部报错)
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotSupported):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, domain.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "找不到指定的用户账户")
	case errors.Is(err, domain.ErrUserExists):
		writeError(w, http.StatusConflict, "该用户账户 ID 已经在系统注册存在，冲突")
	default:
		writeError(w, http.StatusInternalServerError, "系统内部服务器运行异常报错")
	}
}
