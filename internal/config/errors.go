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
)
