// 中心业务审计：保存账户和服务器管理操作的结果，不保存请求正文或凭据

package model

import (
	"time"

	"gorm.io/gorm"
)

// BusinessOperationEvent 只追加业务事实，身份未知时保留空值，不依赖目标仍然存在
// 目标与玩家时间线分别建立索引，业务删除不会级联删除审计记录
type BusinessOperationEvent struct {
	ID            int64          `gorm:"type:bigint;primaryKey;autoIncrement:false"`
	Operation     string         `gorm:"type:varchar(48);not null"`
	Result        string         `gorm:"type:varchar(16);not null"`
	Reason        string         `gorm:"type:varchar(48);not null"`
	Source        string         `gorm:"type:varchar(16);not null"`
	RequestID     string         `gorm:"type:varchar(128);not null"`
	ServerID      *int64         `gorm:"type:bigint"`
	ActorUserID   *int64         `gorm:"type:bigint"`
	TargetType    string         `gorm:"type:varchar(16);not null;index:business_operations_target_time,priority:1"`
	TargetID      *int64         `gorm:"type:bigint;index:business_operations_target_time,priority:2"`
	PlayerName    string         `gorm:"type:varchar(16);not null;index:business_operations_player_time,expression:lower(player_name),priority:1"`
	CooldownUntil *time.Time     `gorm:"type:timestamptz"`
	OccurredAt    time.Time      `gorm:"type:timestamptz;not null;index:business_operations_target_time,priority:3;index:business_operations_player_time,priority:2"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt     time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt     gorm.DeletedAt `gorm:"type:timestamptz"`
}

// TableName 固定中心业务审计的表名
func (BusinessOperationEvent) TableName() string { return "business_operation_events" }
