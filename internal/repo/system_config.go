// 系统配置：中心登录策略与游戏服登录时限读取
// 读取并校验持久化配置，不缓存查询结果

package repo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/gorm"
)

type SystemConfigRepository struct {
	db *gorm.DB
}

type LoginPolicy struct {
	MaxFailures int64
	Cooldown    time.Duration
}

// LoginPolicy 在同一次查询中读取并校验中心登录策略，不缓存配置
func (r *SystemConfigRepository) LoginPolicy(ctx context.Context) (LoginPolicy, error) {
	keys := []string{
		model.ConfigAuthMaxFailures,
		model.ConfigAuthCooldownSeconds,
	}
	var configs []model.SystemConfig
	// 绑定请求上下文，按配置键批量查询，并自动过滤软删除记录
	err := r.db.WithContext(ctx).
		Where("key IN ?", keys).
		Find(&configs).Error
	if err != nil {
		return LoginPolicy{}, repositoryError("读取登录配置", err)
	}

	// 按查询结果数量预分配映射容量，便于通过配置键读取字符串值
	values := make(map[string]string, len(configs))
	for _, config := range configs {
		values[config.Key] = config.Value
	}

	maxFailures, err := parsePositiveConfig(values, model.ConfigAuthMaxFailures)
	if err != nil {
		return LoginPolicy{}, err
	}

	cooldownSeconds, err := parsePositiveConfig(values, model.ConfigAuthCooldownSeconds)
	if err != nil {
		return LoginPolicy{}, err
	}
	// time.Duration 以纳秒保存时长，转换前检查秒数是否溢出
	const maxCooldownSeconds = math.MaxInt64 / int64(time.Second)
	if cooldownSeconds > maxCooldownSeconds {
		return LoginPolicy{}, fmt.Errorf("%w: %s 超出时长范围", ErrSystemConfigInvalid, model.ConfigAuthCooldownSeconds)
	}

	// 将配置中的秒数换算为 time.Duration 使用的纳秒时长
	return LoginPolicy{
		MaxFailures: maxFailures,
		Cooldown:    time.Duration(cooldownSeconds) * time.Second,
	}, nil
}

// parsePositiveConfig 将指定配置解析为正整数，缺失与格式错误分别返回对应错误
func parsePositiveConfig(values map[string]string, key string) (int64, error) {
	value, exists := values[key]
	if !exists {
		return 0, fmt.Errorf("%w: %s", ErrSystemConfigMissing, key)
	}
	// 按十进制解析为有符号 64 位整数，格式错误或超出 int64 范围时返回错误
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%w: %s 必须是正整数", ErrSystemConfigInvalid, key)
	}
	return number, nil
}

// LoginTimeoutSeconds 读取全服共用的登录时限，缺失或非法配置不回退到默认值
func (r *SystemConfigRepository) LoginTimeoutSeconds(ctx context.Context) (int32, error) {
	var config model.SystemConfig
	// 普通查询自动过滤软删除记录，不锁定配置行
	err := r.db.WithContext(ctx).
		Where("key = ?", model.ConfigAuthLoginTimeoutSeconds).
		First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("%w: %s", ErrSystemConfigMissing, model.ConfigAuthLoginTimeoutSeconds)
	}
	if err != nil {
		return 0, repositoryError("读取玩家登录时限", err)
	}

	// 按十进制解析为有符号 32 位整数，与 RPC 字段范围一致，拒绝溢出或非正数
	seconds, err := strconv.ParseInt(config.Value, 10, 32)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%w: %s 必须是 int32 范围内的正整数", ErrSystemConfigInvalid, config.Key)
	}
	return int32(seconds), nil
}
