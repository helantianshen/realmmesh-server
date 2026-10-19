package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ConfigAuthMaxFailures     = "auth.max_failures"
	ConfigAuthCooldownSeconds = "auth.cooldown_seconds"
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
