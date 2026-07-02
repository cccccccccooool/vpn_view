package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vpnview/internal/auth"
	"vpnview/internal/config"
)

type AuthHandler struct {
	authSvc *auth.JWTService
	blocker *auth.IPBlocker
	sec     config.SecurityConfig
}

func NewAuthHandler(authSvc *auth.JWTService, blocker *auth.IPBlocker, sec config.SecurityConfig) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, blocker: blocker, sec: sec}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := h.blocker.GetClientIP(r)

	var req struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	if !h.authSvc.MatchSecret(req.Secret) {
		h.blocker.RecordFailure(ip)
		writeError(w, http.StatusUnauthorized, "invalid admin secret")
		return
	}
	h.blocker.ResetAttempts(ip)

	token, expiresAt, err := h.authSvc.Sign()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign admin token")
		return
	}
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	secure := shouldUseSecureCookie(r, h.sec)
	csrfToken := randomToken()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     cookiePath(sessionCookieName),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     cookiePath(csrfCookieName),
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt,
		"csrf_token": csrfToken,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	secure := shouldUseSecureCookie(r, h.sec)
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     cookiePath(name),
			MaxAge:   -1,
			HttpOnly: name == sessionCookieName,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func cookiePath(name string) string {
	if name == csrfCookieName {
		return "/"
	}
	return "/api/"
}

func shouldUseSecureCookie(r *http.Request, sec config.SecurityConfig) bool {
	switch sec.CookieSecure {
	case "always":
		return true
	case "never":
		return false
	}
	switch sec.DeploymentMode {
	case "strict":
		return true
	case "self_signed":
		return requestIsHTTPS(r)
	default:
		return false
	}
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	cfVisitor := strings.ToLower(r.Header.Get("CF-Visitor"))
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on") ||
		strings.Contains(cfVisitor, `"scheme":"https"`) ||
		strings.Contains(cfVisitor, `"scheme": "https"`)
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b[:])
}
