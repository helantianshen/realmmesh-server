package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Config struct {
	ID       IDConfig       `json:"id"`
	Server   ServerConfig   `json:"server"`
	Postgres PostgresConfig `json:"postgres"`
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

func (c Config) Validate() error {
	if err := c.Server.Validate(); err != nil {
		return err
	}
	if err := c.Postgres.Validate(); err != nil {
		return err
	}
	return c.ID.Validate()
}

// Validate 校验 PostgreSQL 连接配置
func (c PostgresConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("postgres.host 不能为空")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("postgres.port 必须是 1–65535 的整数")
	}
	if strings.TrimSpace(c.User) == "" {
		return fmt.Errorf("postgres.user 不能为空")
	}
	if c.Password == "" {
		return fmt.Errorf("postgres.password 不能为空")
	}
	if strings.TrimSpace(c.Database) == "" {
		return fmt.Errorf("postgres.database 不能为空")
	}
	switch c.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("postgres.sslmode 必须是 disable、require、verify-ca 或 verify-full")
	}
}

// Validate 校验 HTTP 和 gRPC 连接信息配置
func (c ServerConfig) Validate() error {
	if err := c.HTTP.Validate(); err != nil {
		return err
	}
	return c.GRPC.Validate()
}

func (c HTTPConfig) Validate() error {
	return validateAddress("server.http.addr", c.Addr)
}

func (c GRPCConfig) Validate() error {
	return validateAddress("server.grpc.addr", c.Addr)
}

func validateAddress(field, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s 必须是 host:port 格式: %w", field, err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s 端口必须是 1–65535 的整数", field)
	}
	return nil
}

type IDConfig struct {
	NodeID int `json:"node_id"`
}

func (c IDConfig) Validate() error {
	if c.NodeID < 0 || c.NodeID > 1023 {
		return fmt.Errorf("id.node_id 必须是 0–1023 的整数")
	}
	return nil
}
