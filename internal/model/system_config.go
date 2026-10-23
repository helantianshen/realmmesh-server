// 系统配置：中心配置表模型与配置键
// 定义全局配置的存储结构及登录策略键，供迁移与仓储共同使用

package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ConfigWarehousePolicy = "warehouse.policy" // 完整仓库名单 JSON，替换后从下次中心入库校验生效

	ConfigAuthLoginTimeoutSeconds = "auth.login_timeout_seconds" // 玩家登录时限，单位为秒，只影响之后进入的玩家
	ConfigAuthMaxFailures         = "auth.max_failures"          // 登录失败次数上限，达到该次数立即进入休息期
	ConfigAuthCooldownSeconds     = "auth.cooldown_seconds"      // 登录休息时长，单位为秒
)

// TableName 指定中心系统配置表名
func (SystemConfig) TableName() string {
	return "system_configs"
}

type SystemConfig struct {
	ID        int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:system_configs_id_positive,id > 0"`
	Key       string         `gorm:"type:text;not null;uniqueIndex:system_configs_key_key"`
	Value     string         `gorm:"type:text;not null"`
	CreatedAt time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt gorm.DeletedAt `gorm:"type:timestamptz"`
}
