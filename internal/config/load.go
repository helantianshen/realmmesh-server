package config

import (
	"fmt"
	"net"
	"strconv"

	"errors"

	kconfig "github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
)

// Load 加载并校验启动配置，返回前关闭配置监听
func Load(path string) (cfg *Config, err error) {
	source := kconfig.New(
		kconfig.WithSource(file.NewSource(path)),
	)

	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("关闭配置监听: %w", closeErr))
			cfg = nil
		}
	}()

	if err := source.Load(); err != nil {
		return nil, fmt.Errorf("加载配置 %q: %w", path, err)
	}

	var result Config
	if err := source.Scan(&result); err != nil {
		return nil, fmt.Errorf("解析配置 %q: %w", path, err)
	}

	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("校验配置 %q: %w", path, err)
	}

	return &result, nil
}

// Validate 校验配置
func (c Config) Validate() error {
	if err := validateAddress("server.http.addr", c.Server.HTTP.Addr); err != nil {
		return err
	}

	return validateAddress("server.grpc.addr", c.Server.GRPC.Addr)
}

func validateAddress(field, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s 必须是 host:port 格式: %w", field, err)
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s 端口必须是 1–65535 的整数", field)
	}

	return nil
}
