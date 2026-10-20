package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const defaultJWTExpiresIn = "2h" // JWT 默认有效期为两小时，省略 expires_in 时使用

type Config struct {
	Server   ServerConfig   `json:"server"`
	Postgres PostgresConfig `json:"postgres"`
	ID       IDConfig       `json:"id"`
	JWT      JWTConfig      `json:"jwt"`
}

type ServerConfig struct {
	HTTP HTTPConfig `json:"http"`
	GRPC GRPCConfig `json:"grpc"`
}

type HTTPConfig struct {
	Addr string `json:"addr"`
}

type GRPCConfig struct {
	Addr string `json:"addr"`
}

type PostgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Database string `json:"database"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

type IDConfig struct {
	NodeID int `json:"node_id"`
}

// JWTConfig 配置 HTTP 令牌签名密钥和有效期，与游戏服凭证相互独立
type JWTConfig struct {
	SigningKey string `json:"signing_key"`
	ExpiresIn  string `json:"expires_in"`
}

// Validate 逐项校验服务、数据库、ID 和 JWT 配置
func (c Config) Validate() error {
	if err := c.Server.Validate(); err != nil {
		return err
	}
	if err := c.Postgres.Validate(); err != nil {
		return err
	}
	if err := c.ID.Validate(); err != nil {
		return err
	}
	return c.JWT.Validate()
}

// Validate 校验 HTTP 和 gRPC 连接信息配置
func (c ServerConfig) Validate() error {
	if err := c.HTTP.Validate(); err != nil {
		return err
	}
	return c.GRPC.Validate()
}

// Validate 校验 HTTP 监听地址和端口
func (c HTTPConfig) Validate() error {
	return validateAddress("server.http.addr", c.Addr)
}

// Validate 校验 gRPC 监听地址和端口
func (c GRPCConfig) Validate() error {
	return validateAddress("server.grpc.addr", c.Addr)
}

// Validate 校验 PostgreSQL 连接配置
func (c PostgresConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return ErrPostgresHostRequired
	}
	if c.Port < 1 || c.Port > 65535 {
		return ErrPostgresPortInvalid
	}
	if strings.TrimSpace(c.User) == "" {
		return ErrPostgresUserRequired
	}
	if c.Password == "" {
		return ErrPostgresPasswordRequired
	}
	if strings.TrimSpace(c.Database) == "" {
		return ErrPostgresDatabaseRequired
	}
	switch c.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
		return nil
	default:
		return ErrPostgresSSLModeInvalid
	}
}

// Validate 校验中心实例的 ID 节点编号范围
func (c IDConfig) Validate() error {
	if c.NodeID < 0 || c.NodeID > 1023 {
		return ErrNodeIDInvalid
	}
	return nil
}

// Validate 校验签名密钥长度和有效期，错误不包含配置中的密钥或原始输入
func (c JWTConfig) Validate() error {
	if len(c.SigningKey) < 32 || strings.TrimSpace(c.SigningKey) == "" {
		return ErrJWTSigningKeyInvalid
	}

	_, err := c.Lifetime()
	return err
}

// Lifetime 将有效期文本转换为时间间隔，省略配置时采用两小时
// 只接受正整数秒，避免 JWT 秒级时间戳截断配置中的小数秒
func (c JWTConfig) Lifetime() (time.Duration, error) {
	value := c.ExpiresIn
	if value == "" {
		value = defaultJWTExpiresIn
	}

	// ParseDuration 识别 2h、30m 等带单位的文本，并拒绝格式错误或超出 time.Duration 的值
	lifetime, err := time.ParseDuration(value)
	if err != nil || lifetime <= 0 || lifetime%time.Second != 0 {
		return 0, ErrJWTExpiresInInvalid
	}

	return lifetime, nil
}

// validateAddress 校验主机端口格式及端口范围
func validateAddress(field, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s: %w", field, ErrAddressInvalid)
	}
	// Atoi 将端口的十进制文本转换为整数，用于检查合法端口范围
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s: %w", field, ErrPortInvalid)
	}
	return nil
}
