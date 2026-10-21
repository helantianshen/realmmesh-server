// 系统配置：持久化默认值初始化
// 在启动迁移事务中补齐缺失配置并校验必需项，保留已有配置值

package data

import (
	"context"
	"fmt"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InitSystemConfig 在调用方事务内补齐默认配置，保留已存在的值并校验必需项
func InitSystemConfig(ctx context.Context, tx *gorm.DB, generator *sonyflake.Sonyflake) error {
	defaultConfigs := []model.SystemConfig{
		{
			Key:   model.ConfigAuthLoginTimeoutSeconds,
			Value: "60",
		},
		{
			Key:   model.ConfigAuthMaxFailures,
			Value: "3",
		},
		{
			Key:   model.ConfigAuthCooldownSeconds,
			Value: "300",
		},
	}
	// 全表唯一键包含软删除记录，冲突时不覆盖配置或恢复已删除记录
	keepExistingConfig := clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoNothing: true,
	}

	for _, defaultConfig := range defaultConfigs {
		// 从共享雪花生成器获取主键，生成失败时中止初始化
		id, err := generator.NextID()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrSystemConfigInitialization, err)
		}
		defaultConfig.ID = id

		// 在当前事务中插入配置，唯一键冲突时跳过插入
		err = tx.WithContext(ctx).
			Clauses(keepExistingConfig).
			Create(&defaultConfig).Error
		if err != nil {
			return databaseError("插入默认系统配置", err)
		}
	}
	// 使用同一事务读取配置，确保刚插入的默认项也参与校验
	systemConfigRepo := repo.NewSystemConfigRepository(tx)
	if _, err := systemConfigRepo.LoginPolicy(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrSystemConfigInitialization, err)
	}
	if _, err := systemConfigRepo.LoginTimeoutSeconds(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrSystemConfigInitialization, err)
	}
	return nil
}
