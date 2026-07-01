package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"vpnview/internal/auth"
)

type AuthHandler struct {
	authSvc *auth.JWTService
	blocker *auth.IPBlocker
}

func NewAuthHandler(authSvc *auth.JWTService, blocker *auth.IPBlocker) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, blocker: blocker}
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
	secure := shouldUseSecureCookie(r)
	csrfToken := randomToken()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/api/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/api/",
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
	secure := shouldUseSecureCookie(r)
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/api/",
			MaxAge:   -1,
			HttpOnly: name == sessionCookieName,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func shouldUseSecureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1"
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b[:])
}
