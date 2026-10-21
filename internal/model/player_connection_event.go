// 玩家连接审计：保存游戏服报告的连接事实，不关联账户存在性或在线状态

package model

import (
	"time"

	"gorm.io/gorm"
)

// PlayerConnectionEvent 是独立审计记录，只追加，不以连接 ID 去重或覆盖
// 两组索引分别用于同服连接追踪，以及按玩家查询接收时间线
type PlayerConnectionEvent struct {
	ID           int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:player_connection_events_id_positive,id > 0"`
	ServerID     int64          `gorm:"type:bigint;not null;index:player_connection_events_server_connection,priority:1"`
	ConnectionID string         `gorm:"type:uuid;not null;index:player_connection_events_server_connection,priority:2"`
	PlayerName   string         `gorm:"type:varchar(16);not null;index:player_connection_events_player_received,expression:lower(player_name),priority:1"`
	EventType    int32          `gorm:"type:smallint;not null;check:player_connection_events_type_valid,event_type IN (1,2)"`
	OccurredAt   time.Time      `gorm:"type:timestamptz;not null"`
	ReceivedAt   time.Time      `gorm:"type:timestamptz;not null;index:player_connection_events_player_received,priority:2"`
	CreatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt    gorm.DeletedAt `gorm:"type:timestamptz"`
}

// TableName 固定玩家连接审计表名，不依赖 GORM 自动复数推导
func (PlayerConnectionEvent) TableName() string { return "player_connection_events" }
