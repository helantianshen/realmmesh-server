package data

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenPostgres(ctx context.Context, cfg config.PostgresConfig) (*gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// 驱动错误可能带有连接信息，SQL 日志也可能包含认证字段，因此统一使用无敏感参数的错误摘要
	db, err := gorm.Open(postgres.Open(postgresDSN(cfg)), &gorm.Config{
		Logger:               logger.Default.LogMode(logger.Silent),
		DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, databaseError("初始化数据库", err)
	}
	pool, err := db.DB()
	if err != nil {
		return nil, databaseError("获取数据库连接池", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(2)
	pool.SetConnMaxLifetime(30 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, databaseError("连接数据库", err)
	}
	return db, nil
}

// Migrate 自动迁移表结构
func Migrate(ctx context.Context, db *gorm.DB) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.AutoMigrate(
			&model.User{},
		)
	})
	if err != nil {
		return databaseError("迁移用户表", err)
	}
	return nil
}

func databaseError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s超时", operation)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s已取消", operation)
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return fmt.Errorf("%s失败（SQLSTATE %s）", operation, state.SQLState())
	}
	return fmt.Errorf("%s失败，请检查数据库状态与连接配置", operation)
}

func postgresDSN(cfg config.PostgresConfig) string {
	address := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Database,
		User:   url.UserPassword(cfg.User, cfg.Password),
	}
	query := address.Query()
	query.Set("sslmode", cfg.SSLMode)
	query.Set("connect_timeout", "5")
	query.Set("timezone", "UTC")
	address.RawQuery = query.Encode()
	return address.String()
}
