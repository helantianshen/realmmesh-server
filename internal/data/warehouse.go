// 共享仓库迁移：建立只约束有效可堆叠库存的合并索引

package data

import "gorm.io/gorm"

// MigrateWarehouseConstraints 在启动迁移事务中建立部分唯一索引
func MigrateWarehouseConstraints(tx *gorm.DB) error {
	err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_warehouse_item
		 ON warehouse_items (user_id, item_id, item_data_hash)
		 WHERE deleted_at IS NULL AND stackable = TRUE`).Error
	if err != nil {
		return databaseError("迁移共享仓库约束", err)
	}
	return nil
}
