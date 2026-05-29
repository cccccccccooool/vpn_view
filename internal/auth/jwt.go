// ============================================================================
// 文件说明：internal/auth/jwt.go
// 职责概览：提供系统管理面板的 JWT 身份认证服务（JWTService）。
//           负责无状态管理令牌（Token）的签名下发与解密验证。
//           系统采用 HMAC-SHA256 对称签名算法，通过全局唯一的 Auth Secret 验证身份。
// ============================================================================

package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService 处理无状态的 JWT 令牌签名生成和完整性有效性验证。
// 鉴于系统采用全功能管理员单一授权设计，凡是能正确提供全局 Auth Secret 认证通过者，
// 即被视作授信管理员，系统不独立维护多用户管理员账户表。
type JWTService struct {
	secret []byte        // JWT 签名密钥
	ttl    time.Duration // 签发出的 Token 默认生存有效期（TTL）
}

// NewJWTService 创建并实例化一个新的 JWTService 认证服务。
func NewJWTService(secret string, ttl time.Duration) *JWTService {
	return &JWTService{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Sign 签发并生成一个全新的 JWT 签名令牌字符串。
// 返回生成的 Token 字符串、过期时间点或可能抛出的错误。
func (s *JWTService) Sign() (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.ttl) // 计算过期截止时间点

	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(now),
		Issuer:    "vpnview", // 声明令牌发行者
	}

	// 使用 HS256 (HMAC-SHA256) 对称数字签名生成令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("生成 JWT 令牌签名失败: %w", err)
	}

	return signed, expiresAt, nil
}

// Verify 解码并强制校验传入的 JWT 令牌的签名和过期时间属性。
// 校验成功返回 nil，否则返回具体指示失败的异常错误。
func (s *JWTService) Verify(tokenStr string) error {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		// 校验令牌签名算法是否对齐 HS256 防止算法降级攻击
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("未预期的数字签名算法: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return fmt.Errorf("解析 JWT 令牌失败: %w", err)
	}
	if !token.Valid {
		return fmt.Errorf("非法的 JWT 授信令牌")
	}
	return nil
}

// MatchSecret 用于直接核对传入的原始字符串是否与当前配置的 HMAC 密钥相同。
func (s *JWTService) MatchSecret(input string) bool {
	return input == string(s.secret)
}
