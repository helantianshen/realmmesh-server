// 共享仓库仓储：使用短事务提交幂等操作、原子数量变更与资产审计

package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WarehouseRepository 通过库存条件更新与操作唯一约束协调并发资产写入
type WarehouseRepository struct{ db *gorm.DB }

// WarehouseMutation 携带规范化单件载荷与本次操作的固定参数
// 存入的 InventoryID 是候选新记录主键，取出时是预检查的原库存主键
type WarehouseMutation struct {
	RecordID      int64
	EventID       int64
	InventoryID   int64
	UserID        int64
	ServerID      int64
	OperationID   string
	ItemID        string
	ItemData      []byte
	Stackable     bool
	Quantity      int64
	QuantityLimit int64
	// DepositDenied 是业务层当前名单快照的拒绝结论，不影响已存在操作的原结果
	DepositDenied bool
}

// WarehouseFinish 只允许原服务器确认已发放或明确未发放
type WarehouseFinish struct {
	OperationID string
	UserID      int64
	ServerID    int64
	EventID     int64
	Delivered   bool
	Reason      string
}

// Deposit 在同一短事务内去重、合并库存并写审计，返回已提交操作
func (r *WarehouseRepository) Deposit(ctx context.Context, input WarehouseMutation) (*model.WarehouseOperation, error) {
	if err := validateWarehouseMutation(input); err != nil {
		return nil, err
	}
	operation := newWarehouseOperation(input, model.WarehouseDeposit, model.WarehouseCommitted)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := claimWarehouseOperation(tx, &operation)
		if err != nil || !created {
			return err
		}
		if input.DepositDenied {
			return rejectWarehouseDeposit(tx, input.EventID, &operation, "denied")
		}

		// 唯一索引协调首次并发插入，更新表达式直接在当前数量上累加
		inventoryID, err := depositWarehouseItem(tx, input, operation.ItemDataHash)
		if errors.Is(err, ErrWarehouseQuantityLimit) {
			return rejectWarehouseDeposit(tx, input.EventID, &operation, "quantity_limit")
		}
		if errors.Is(err, ErrWarehouseHashCollision) {
			return rejectWarehouseDeposit(tx, input.EventID, &operation, "hash_collision")
		}
		if err != nil {
			return err
		}
		operation.InventoryID = inventoryID
		return recordWarehouseDeposit(tx, input.EventID, &operation)
	})
	if err != nil {
		return nil, warehouseRepositoryError("存入共享仓库", err)
	}
	if operation.State == model.WarehouseRejected {
		return &operation, warehouseDepositRejection(operation.Reason)
	}
	return &operation, nil
}

// recordWarehouseDeposit 在持有库存行锁的事务内绑定实际库存并追加审计
// 审计从 UPDATE 的结果取值；插入失败时，操作绑定和库存累加一起回滚
func recordWarehouseDeposit(tx *gorm.DB, eventID int64, operation *model.WarehouseOperation) error {
	var updated struct{ UpdatedAt time.Time }
	result := tx.Raw(`WITH updated AS (
		UPDATE warehouse_operations SET inventory_id = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
		RETURNING operation_id, user_id, server_id, inventory_id, item_id,
			item_data_hash, quantity, direction, state, reason, updated_at
	)
	INSERT INTO warehouse_events
		(id, operation_id, user_id, server_id, inventory_id, item_id, item_data_hash,
		 quantity, direction, state, reason, created_at, updated_at)
	SELECT ?, operation_id, user_id, server_id, inventory_id, item_id, item_data_hash,
		quantity, direction, state, reason, updated_at, updated_at FROM updated
	RETURNING updated_at`, operation.InventoryID, tx.NowFunc(), operation.ID, eventID).Scan(&updated)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrWarehouseStateConflict
	}
	operation.UpdatedAt = updated.UpdatedAt
	return nil
}

// rejectWarehouseDeposit 保留明确拒绝的幂等结果，避免拒绝归还后同一操作又入库
func rejectWarehouseDeposit(tx *gorm.DB, eventID int64, operation *model.WarehouseOperation, reason string) error {
	operation.State = model.WarehouseRejected
	operation.Reason = reason
	if err := tx.Model(operation).Updates(map[string]any{"state": operation.State, "reason": reason}).Error; err != nil {
		return err
	}
	// 拒绝记录的库存 ID 是原候选主键，不表示已经创建对应库存
	return appendWarehouseEvent(tx, eventID, operation)
}

// warehouseDepositRejection 将已提交的拒绝原因映射为稳定业务错误
func warehouseDepositRejection(reason string) error {
	switch reason {
	case "denied":
		return ErrWarehouseDepositDenied
	case "quantity_limit":
		return ErrWarehouseQuantityLimit
	case "hash_collision":
		return ErrWarehouseHashCollision
	default:
		return ErrWarehouseStateConflict
	}
}

// Debit 在游戏服准备确认后执行条件扣库，不持有跨网络事务
func (r *WarehouseRepository) Debit(ctx context.Context, input WarehouseMutation) (*model.WarehouseOperation, error) {
	if err := validateWarehouseMutation(input); err != nil {
		return nil, err
	}
	operation := newWarehouseOperation(input, model.WarehouseWithdrawal, model.WarehouseDebited)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := claimWarehouseOperation(tx, &operation)
		if err != nil || !created {
			return err
		}

		// 数量与物品快照在同一 UPDATE 中复核，受影响行数决定是否授权发放
		result := tx.Exec(`UPDATE warehouse_items
			SET quantity = quantity - ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND user_id = ? AND deleted_at IS NULL
			AND item_id = ? AND item_data = ? AND item_data_hash = ? AND stackable = ?
			AND quantity >= ?`,
			input.Quantity, input.InventoryID, input.UserID, input.ItemID,
			input.ItemData, operation.ItemDataHash, input.Stackable, input.Quantity)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWarehouseItemNotFound
		}
		return appendWarehouseEvent(tx, input.EventID, &operation)
	})
	if err != nil {
		return nil, warehouseRepositoryError("扣减共享仓库", err)
	}
	return &operation, nil
}

// Finish 将一次已扣库操作确认发放或一次性退库，重复相同结果返回原操作
func (r *WarehouseRepository) Finish(ctx context.Context, input WarehouseFinish) (*model.WarehouseOperation, error) {
	if input.UserID <= 0 || input.ServerID <= 0 || input.EventID <= 0 || len(input.Reason) > 128 {
		return nil, ErrWarehouseInvalid
	}
	if _, err := uuid.Parse(input.OperationID); err != nil {
		return nil, ErrWarehouseInvalid
	}
	state := model.WarehouseRefunded
	if input.Delivered {
		state = model.WarehouseDelivered
	}
	var operation model.WarehouseOperation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := readWarehouseOperation(tx, input.OperationID, &operation); err != nil {
			return err
		}
		if operation.UserID != input.UserID || operation.ServerID != input.ServerID || operation.Direction != model.WarehouseWithdrawal {
			return ErrWarehouseOperationConflict
		}
		if operation.State == state {
			return nil
		}
		if operation.State != model.WarehouseDebited {
			return ErrWarehouseOperationConflict
		}

		// 条件状态迁移只允许一个确认赢得处理权，其他确认等待后读取已提交结果
		var updated struct{ UpdatedAt time.Time }
		result := tx.Raw(`UPDATE warehouse_operations
			SET state = ?, reason = ?, updated_at = ?
			WHERE id = ? AND state = ? RETURNING updated_at`,
			state, input.Reason, tx.NowFunc(), operation.ID, model.WarehouseDebited).Scan(&updated)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := readWarehouseOperation(tx, input.OperationID, &operation); err != nil {
				return err
			}
			if operation.State == state {
				return nil
			}
			return ErrWarehouseOperationConflict
		}
		operation.State = state
		operation.Reason = input.Reason
		operation.UpdatedAt = updated.UpdatedAt

		if !input.Delivered {
			// 退款不受新存入上限约束；原行归零仍保留，避免软删除后重建物品身份
			result := tx.Exec(`UPDATE warehouse_items
				SET quantity = quantity + ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ? AND user_id = ? AND deleted_at IS NULL
				AND item_id = ? AND item_data = ? AND item_data_hash = ? AND stackable = ?
				AND quantity <= ? AND (stackable OR quantity = 0)`,
				operation.Quantity, operation.InventoryID, operation.UserID, operation.ItemID,
				operation.ItemData, operation.ItemDataHash, operation.Stackable, math.MaxInt64-operation.Quantity)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWarehouseStateConflict
			}
		}
		return appendWarehouseEvent(tx, input.EventID, &operation)
	})
	if err != nil {
		return nil, warehouseRepositoryError("确认共享仓库取出", err)
	}
	return &operation, nil
}

// FindItem 查询账户可见的完整库存，保留零数量记录供重复扣库请求复核身份
// 此查询不授权发放，准备阶段需校验数量，实际扣库仍依赖 Debit 的条件更新
func (r *WarehouseRepository) FindItem(ctx context.Context, userID, inventoryID int64) (*model.WarehouseItem, error) {
	if userID <= 0 || inventoryID <= 0 {
		return nil, ErrWarehouseInvalid
	}
	var item model.WarehouseItem
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", inventoryID, userID).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWarehouseItemNotFound
	}
	if err != nil {
		return nil, repositoryError("查询共享仓库物品", err)
	}
	return &item, nil
}

// ListItems 按库存主键游标读取正数库存摘要，不加载物品载荷，单页最多一百条
func (r *WarehouseRepository) ListItems(ctx context.Context, userID, afterID int64, limit int) ([]model.WarehouseItem, error) {
	if userID <= 0 || afterID < 0 || limit < 1 || limit > 100 {
		return nil, ErrWarehouseInvalid
	}
	items := make([]model.WarehouseItem, 0)
	err := r.db.WithContext(ctx).Select("id", "user_id", "item_id", "item_data_hash", "stackable", "quantity").
		Where("user_id = ? AND id > ? AND quantity > 0", userID, afterID).
		Order("id ASC").Limit(limit).Find(&items).Error
	if err != nil {
		return nil, repositoryError("列出共享仓库物品", err)
	}
	return items, nil
}

// FindOperation 查询原服务器和账户的操作，软删除不解除幂等约束
func (r *WarehouseRepository) FindOperation(ctx context.Context, userID, serverID int64, operationID string) (*model.WarehouseOperation, error) {
	if userID <= 0 || serverID <= 0 {
		return nil, ErrWarehouseInvalid
	}
	if _, err := uuid.Parse(operationID); err != nil {
		return nil, ErrWarehouseInvalid
	}
	var operation model.WarehouseOperation
	err := readWarehouseOperation(r.db.WithContext(ctx), operationID, &operation)
	if err != nil {
		return nil, warehouseRepositoryError("查询共享仓库操作", err)
	}
	if operation.UserID != userID || operation.ServerID != serverID {
		return nil, ErrWarehouseOperationNotFound
	}
	return &operation, nil
}

// validateWarehouseMutation 在进入事务前拒绝非法数量、身份和操作标识
func validateWarehouseMutation(input WarehouseMutation) error {
	if input.RecordID <= 0 || input.EventID <= 0 || input.InventoryID <= 0 || input.UserID <= 0 || input.ServerID <= 0 {
		return ErrWarehouseInvalid
	}
	if input.ItemID == "" || len(input.ItemData) == 0 || input.Quantity <= 0 || input.QuantityLimit <= 0 {
		return ErrWarehouseInvalid
	}
	if !input.Stackable && input.Quantity != 1 {
		return ErrWarehouseInvalid
	}
	if _, err := uuid.Parse(input.OperationID); err != nil {
		return ErrWarehouseInvalid
	}
	return nil
}

// newWarehouseOperation 复制操作事实并由中心计算完整规范化载荷的 SHA-256
func newWarehouseOperation(input WarehouseMutation, direction, state string) model.WarehouseOperation {
	hash := sha256.Sum256(input.ItemData)
	return model.WarehouseOperation{
		ID: input.RecordID, OperationID: input.OperationID, UserID: input.UserID,
		ServerID: input.ServerID, Direction: direction, State: state,
		InventoryID: input.InventoryID, ItemID: input.ItemID,
		ItemData: bytes.Clone(input.ItemData), ItemDataHash: hash[:],
		Stackable: input.Stackable, Quantity: input.Quantity,
	}
}

// claimWarehouseOperation 通过永久唯一键争取首次处理权，重复请求必须匹配原参数
func claimWarehouseOperation(tx *gorm.DB, operation *model.WarehouseOperation) (bool, error) {
	result := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "operation_id"}}, DoNothing: true,
	}).Create(operation)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var existing model.WarehouseOperation
	if err := readWarehouseOperation(tx, operation.OperationID, &existing); err != nil {
		return false, err
	}
	if !sameWarehouseRequest(&existing, operation) {
		return false, ErrWarehouseOperationConflict
	}
	*operation = existing
	return false, nil
}

// sameWarehouseRequest 比较业务参数，忽略本次生成的候选主键和当前配置上限
func sameWarehouseRequest(existing, requested *model.WarehouseOperation) bool {
	if existing.UserID != requested.UserID || existing.ServerID != requested.ServerID || existing.Direction != requested.Direction {
		return false
	}
	if existing.Direction == model.WarehouseWithdrawal && existing.InventoryID != requested.InventoryID {
		return false
	}
	return existing.ItemID == requested.ItemID && existing.Stackable == requested.Stackable &&
		existing.Quantity == requested.Quantity && bytes.Equal(existing.ItemData, requested.ItemData) &&
		bytes.Equal(existing.ItemDataHash, requested.ItemDataHash)
}

// depositWarehouseItem 对可堆叠物品执行冲突更新，不可堆叠物品每件保存一行
func depositWarehouseItem(tx *gorm.DB, input WarehouseMutation, hash []byte) (int64, error) {
	// 已完成操作在调用此函数前返回原结果，配置收紧不改变旧操作的幂等结论
	if input.Quantity > input.QuantityLimit {
		return 0, ErrWarehouseQuantityLimit
	}
	item := model.WarehouseItem{
		ID: input.InventoryID, UserID: input.UserID, ItemID: input.ItemID,
		ItemData: input.ItemData, ItemDataHash: hash, Stackable: input.Stackable,
		Quantity: input.Quantity,
	}
	if !input.Stackable {
		if err := tx.Create(&item).Error; err != nil {
			return 0, err
		}
		return item.ID, nil
	}
	var stored struct{ ID int64 }
	result := tx.Raw(`INSERT INTO warehouse_items AS current
		(id, user_id, item_id, item_data, item_data_hash, stackable, quantity, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, TRUE, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id, item_id, item_data_hash)
		WHERE deleted_at IS NULL AND stackable = TRUE
		DO UPDATE SET quantity = current.quantity + EXCLUDED.quantity, updated_at = CURRENT_TIMESTAMP
		WHERE current.item_data = EXCLUDED.item_data AND current.quantity <= ? - EXCLUDED.quantity
		RETURNING current.id`,
		input.InventoryID, input.UserID, input.ItemID, input.ItemData, hash, input.Quantity, input.QuantityLimit).Scan(&stored)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 1 {
		return stored.ID, nil
	}

	// 冲突行已由 UPDATE 协调，完整载荷不同必须拒绝，不能将哈希当作数据等价证明
	var existing model.WarehouseItem
	err := tx.Where("user_id = ? AND item_id = ? AND item_data_hash = ? AND stackable = TRUE", input.UserID, input.ItemID, hash).
		Take(&existing).Error
	if err != nil {
		return 0, err
	}
	if !bytes.Equal(existing.ItemData, input.ItemData) {
		return 0, ErrWarehouseHashCollision
	}
	return 0, ErrWarehouseQuantityLimit
}

// readWarehouseOperation 读取包含软删除记录的原操作，缺失时返回固定错误
func readWarehouseOperation(tx *gorm.DB, operationID string, operation *model.WarehouseOperation) error {
	err := tx.Unscoped().Where("operation_id = ?", operationID).Take(operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrWarehouseOperationNotFound
	}
	return err
}

// appendWarehouseEvent 在调用方事务内追加结果，审计失败时库存与操作一并回滚
func appendWarehouseEvent(tx *gorm.DB, id int64, operation *model.WarehouseOperation) error {
	now := time.Now().UTC()
	event := model.WarehouseEvent{
		ID: id, OperationID: operation.OperationID, UserID: operation.UserID,
		ServerID: operation.ServerID, InventoryID: operation.InventoryID,
		ItemID: operation.ItemID, ItemDataHash: operation.ItemDataHash, Quantity: operation.Quantity,
		Direction: operation.Direction, State: operation.State, Reason: operation.Reason,
		CreatedAt: now, UpdatedAt: now,
	}
	return tx.Create(&event).Error
}

// warehouseRepositoryError 保留业务可判定错误，数据库异常统一脱敏
func warehouseRepositoryError(action string, err error) error {
	knownErrors := []error{
		ErrWarehouseInvalid, ErrWarehouseItemNotFound, ErrWarehouseOperationNotFound,
		ErrWarehouseOperationConflict, ErrWarehouseHashCollision, ErrWarehouseQuantityLimit, ErrWarehouseStateConflict,
		ErrWarehouseDepositDenied,
	}
	for _, known := range knownErrors {
		if errors.Is(err, known) {
			return known
		}
	}
	return repositoryError(action, err)
}
