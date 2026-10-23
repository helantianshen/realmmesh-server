// 共享仓库持久化模型：保存库存、幂等操作与同事务资产审计

package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	WarehouseDeposit    = "deposit"
	WarehouseWithdrawal = "withdrawal"
	WarehouseCommitted  = "committed"
	WarehouseDebited    = "debited"
	WarehouseDelivered  = "delivered"
	WarehouseRefunded   = "refunded"
	WarehouseRejected   = "rejected"
)

// WarehouseItem 保存规范化单件载荷，数量不参与物品身份
// 数量归零后保留记录，明确未发放时按原操作退回同一库存
type WarehouseItem struct {
	ID           int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:warehouse_items_id_positive,id > 0"`
	UserID       int64          `gorm:"type:bigint;not null;index:warehouse_items_user_id"`
	ItemID       string         `gorm:"type:text;not null"`
	ItemData     []byte         `gorm:"type:bytea;not null"`
	ItemDataHash []byte         `gorm:"type:bytea;not null;check:warehouse_items_hash_length,octet_length(item_data_hash) = 32"`
	Stackable    bool           `gorm:"not null;check:warehouse_items_unstackable_quantity,stackable OR quantity <= 1"`
	Quantity     int64          `gorm:"type:bigint;not null;check:warehouse_items_quantity_nonnegative,quantity >= 0"`
	CreatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt    gorm.DeletedAt `gorm:"type:timestamptz"`
}

// TableName 固定共享库存的数据库表名
func (WarehouseItem) TableName() string { return "warehouse_items" }

// WarehouseOperation 的操作 ID 跨软删除保持唯一，记录首次请求与已提交结果
// 载荷保留原操作事实，重复请求不能用同一 ID 更换账户、方向或物品
type WarehouseOperation struct {
	ID           int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:warehouse_operations_id_positive,id > 0"`
	OperationID  string         `gorm:"type:uuid;not null;uniqueIndex:warehouse_operations_operation_id_key"`
	UserID       int64          `gorm:"type:bigint;not null;index:warehouse_operations_user_time,priority:1"`
	ServerID     int64          `gorm:"type:bigint;not null"`
	Direction    string         `gorm:"type:varchar(16);not null;check:warehouse_operations_direction,direction IN ('deposit','withdrawal')"`
	State        string         `gorm:"type:varchar(16);not null;check:warehouse_operations_state,state IN ('committed','debited','delivered','refunded','rejected')"`
	InventoryID  int64          `gorm:"type:bigint;not null"`
	ItemID       string         `gorm:"type:text;not null"`
	ItemData     []byte         `gorm:"type:bytea;not null"`
	ItemDataHash []byte         `gorm:"type:bytea;not null;check:warehouse_operations_hash_length,octet_length(item_data_hash) = 32"`
	Stackable    bool           `gorm:"not null"`
	Quantity     int64          `gorm:"type:bigint;not null;check:warehouse_operations_quantity_positive,quantity > 0"`
	Reason       string         `gorm:"type:varchar(128);not null"`
	CreatedAt    time.Time      `gorm:"type:timestamptz;not null;index:warehouse_operations_user_time,priority:2"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt    gorm.DeletedAt `gorm:"type:timestamptz"`
}

// TableName 固定资产幂等操作的数据库表名
func (WarehouseOperation) TableName() string { return "warehouse_operations" }

// WarehouseEvent 追加资产状态迁移事实，与对应库存变更一起提交
type WarehouseEvent struct {
	ID           int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:warehouse_events_id_positive,id > 0"`
	OperationID  string         `gorm:"type:uuid;not null;index:warehouse_events_operation_time,priority:1"`
	UserID       int64          `gorm:"type:bigint;not null"`
	ServerID     int64          `gorm:"type:bigint;not null"`
	InventoryID  int64          `gorm:"type:bigint;not null"`
	ItemID       string         `gorm:"type:text;not null"`
	ItemDataHash []byte         `gorm:"type:bytea;not null;check:warehouse_events_hash_length,octet_length(item_data_hash) = 32"`
	Quantity     int64          `gorm:"type:bigint;not null;check:warehouse_events_quantity_positive,quantity > 0"`
	Direction    string         `gorm:"type:varchar(16);not null"`
	State        string         `gorm:"type:varchar(16);not null"`
	Reason       string         `gorm:"type:varchar(128);not null"`
	CreatedAt    time.Time      `gorm:"type:timestamptz;not null;index:warehouse_events_operation_time,priority:2"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt    gorm.DeletedAt `gorm:"type:timestamptz"`
}

// TableName 固定共享仓库事件审计的数据库表名
func (WarehouseEvent) TableName() string { return "warehouse_events" }
