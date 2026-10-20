// 运行配置：配置校验错误
// 集中定义配置缺失或取值非法的错误，不包含实际凭据

package config

import "errors"

var (
	ErrPostgresHostRequired     = errors.New("postgres.host 不能为空")
	ErrPostgresPortInvalid      = errors.New("postgres.port 必须是 1–65535 的整数")
	ErrPostgresUserRequired     = errors.New("postgres.user 不能为空")
	ErrPostgresPasswordRequired = errors.New("postgres.password 不能为空")
	ErrPostgresDatabaseRequired = errors.New("postgres.database 不能为空")
	ErrPostgresSSLModeInvalid   = errors.New("postgres.sslmode 必须是 disable、require、verify-ca 或 verify-full")
	ErrNodeIDInvalid            = errors.New("id.node_id 必须是 0–1023 的整数")
	ErrAddressInvalid           = errors.New("地址必须是 host:port 格式")
	ErrPortInvalid              = errors.New("端口必须是 1–65535 的整数")
	ErrJWTSigningKeyInvalid     = errors.New("jwt.signing_key 至少需要 32 字节且不能全为空白")
	ErrJWTExpiresInInvalid      = errors.New("jwt.expires_in 必须为正整数秒的时长，例如 2h 或 30m")
)
