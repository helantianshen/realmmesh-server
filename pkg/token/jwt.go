// Package token 签发和校验用于 RealmMesh HTTP 访问的 JWT
package token

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 保存用户身份和标准过期声明，复用库提供的声明解析与时间校验
// string 标签让 JSON 将 int64 编码为字符串，避免前端读取雪花 ID 时丢失精度
type Claims struct {
	UserID int64 `json:"user_id,string"`
	jwt.RegisteredClaims
}

// Manager 持有独立签名密钥和签发时使用的有效期，可供多个请求并发使用
// 它只验证令牌，不查询账户状态，也不判断管理员权限
type Manager struct {
	signingKey []byte
	lifetime   time.Duration
	now        func() time.Time
}

// AccessToken 包含可交付的令牌及其截止时间，不包含签名密钥
type AccessToken struct {
	Value     string
	ExpiresAt time.Time
}

// New 创建固定使用 HS256 的令牌管理器，拒绝短密钥与非整数秒的有效期
// signingKey 应为部署时生成并持久保存的随机密钥，不应复用游戏服密钥
func New(signingKey string, lifetime time.Duration) (*Manager, error) {
	if len(signingKey) < 32 || strings.TrimSpace(signingKey) == "" {
		return nil, ErrSigningKeyInvalid
	}
	if lifetime <= 0 || lifetime%time.Second != 0 {
		return nil, ErrLifetimeInvalid
	}

	return &Manager{
		signingKey: []byte(signingKey),
		lifetime:   lifetime,
		now:        time.Now,
	}, nil
}

// Issue 为已认证的正整数用户 ID 签发访问令牌，返回令牌及截止时间
// 本方法不校验密码，调用者必须在账户认证成功后调用
// 载荷只写入用户 ID 与过期时间，不保存密码或可变的管理员权限
func (m *Manager) Issue(userID int64) (*AccessToken, error) {
	if userID <= 0 {
		return nil, ErrUserIDInvalid
	}

	// 1. JWT 时间按秒表示，统一截断后计算截止时间，保证返回值与载荷完全一致
	issuedAt := m.now().UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(m.lifetime)

	// 2. NewNumericDate 将 Go 时间转换为标准 exp 声明，其余标准声明留空
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	// 3. NewWithClaims 固定选择 HS256；SignedString 对头部和载荷签名，不加密载荷
	signed := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := signed.SignedString(m.signingKey)
	if err != nil {
		return nil, ErrSigningFailed
	}

	return &AccessToken{Value: value, ExpiresAt: expiresAt}, nil
}

// Verify 通过库验证签名和过期时间，再检查用户 ID，成功后返回身份
// 到达 exp 立即失效；解析失败只返回模块错误，不暴露原始令牌
func (m *Manager) Verify(encoded string) (int64, error) {
	// ParseWithClaims 负责解析与校验；固定 HS256，并要求令牌必须携带 exp
	// WithTimeFunc 与签发共用时钟，便于准确验证过期边界
	var claims Claims
	_, err := jwt.ParseWithClaims(
		encoded,
		&claims,
		func(*jwt.Token) (any, error) { return m.signingKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrExpired
		}
		return 0, ErrInvalid
	}

	// JSON 解码负责整数转换与溢出检查，这里只校验业务要求的正整数身份
	if claims.UserID <= 0 {
		return 0, ErrInvalid
	}

	return claims.UserID, nil
}
