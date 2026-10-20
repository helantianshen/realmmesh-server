package token

import "errors"

var (
	ErrSigningKeyInvalid = errors.New("JWT 签名密钥至少需要 32 字节且不能全为空白")
	ErrLifetimeInvalid   = errors.New("JWT 有效期必须为正整数秒")
	ErrUserIDInvalid     = errors.New("JWT 用户 ID 必须为正整数")
	ErrSigningFailed     = errors.New("JWT 签发失败")
	ErrInvalid           = errors.New("JWT 无效")
	ErrExpired           = errors.New("JWT 已过期")
)
