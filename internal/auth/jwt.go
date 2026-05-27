// Package auth 提供管理面板的身份认证与安全防护能力。
// 包含 JWT 令牌签发/验证（JWTService）以及基于 IP 的
// 暴力破解防御与自动防火墙联动（IPBlocker）。
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService 处理无状态的 JWT 令牌签名和验证。
// 使用单个共享密钥 (HMAC-SHA256)，没有用户名 — 这是
// 一个纯粹的管理身份验证系统，知道密钥即等于
// 获得授权。
type JWTService struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTService 使用给定的密钥和令牌 TTL 创建一个新的 JWT 服务。
func NewJWTService(secret string, ttl time.Duration) *JWTService {
	return &JWTService{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Sign 生成一个新的 JWT 令牌。返回签名的令牌字符串及其
// 过期时间。
func (s *JWTService) Sign() (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.ttl)

	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(now),
		Issuer:    "vpnview",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return signed, expiresAt, nil
}

// Verify 验证 JWT 令牌字符串。如果令牌有效
// 且未过期，则返回 nil；否则返回错误。
func (s *JWTService) Verify(tokenStr string) error {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	if !token.Valid {
		return fmt.Errorf("invalid token")
	}
	return nil
}

// MatchSecret 检查提供的密钥是否与配置的密钥匹配。
func (s *JWTService) MatchSecret(input string) bool {
	return input == string(s.secret)
}
