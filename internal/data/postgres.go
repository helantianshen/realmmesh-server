// 持久化基础设施：PostgreSQL 连接与启动迁移
// 管理连接池和连通性检查，在事务中迁移模型并初始化系统配置

// Package data 管理数据库连接、模型迁移与持久化系统配置初始化
package data

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/sony/sonyflake/v2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenPostgres 创建连接池并检查数据库连通性，连接失败时关闭连接池
func OpenPostgres(ctx context.Context, cfg config.PostgresConfig) (*gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// 驱动错误可能带有连接信息，SQL 日志也可能包含认证字段，因此统一使用无敏感参数的错误摘要
	db, err := gorm.Open(postgres.Open(postgresDSN(cfg)), &gorm.Config{
		Logger:               logger.Default.LogMode(logger.Silent),
		DisableAutomaticPing: true,
		// 单条写入使用数据库自动提交，多步原子操作由调用方显式开启事务
		SkipDefaultTransaction: true,
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

// Migrate 在同一事务内迁移表结构并初始化系统配置
func Migrate(ctx context.Context, db *gorm.DB, generator *sonyflake.Sonyflake) error {
	// 事务回调返回错误时回滚，返回 nil 时提交表结构和配置数据
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 根据模型创建或调整表结构及索引，不删除模型中已移除的列
		err := tx.AutoMigrate(
			&model.User{},
			&model.SystemConfig{},
			&model.Server{},
			&model.PlayerConnectionEvent{},
		)
		if err != nil {
			return databaseError("迁移表结构", err)
		}
		return InitSystemConfig(ctx, tx, generator)
	})
	if err == nil {
		return nil
	}
	// 回调中的错误已分类和脱敏，只转换事务开始或提交阶段的原始错误
	if errors.Is(err, ErrSystemConfigInitialization) || errors.Is(err, ErrDatabase) {
		return err
	}
	return databaseError("迁移数据库", err)
}

// postgresDSN 构造 PostgreSQL 连接串，并转义账号密码中的特殊字符
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
