// 共享仓库业务：验证物品、当前账户与名单，协调短事务入库和取出结果
// 网络准备与物品发放在数据库事务之外执行，未知通信结果不推断为未执行

package biz

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/helantianshen/realmmesh-server/internal/itemdata"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
)

const (
	WarehouseOperationLimit int64 = 2304       // 单次存取数量上限，超过即拒绝
	WarehouseQuantityLimit  int64 = 2147483647 // 新存入后的单条数量上限，明确退库不受此业务上限限制
)

// WarehouseBiz 复用现有身份认证和 ID 生成器，不维护玩家中心会话
type WarehouseBiz struct {
	warehouseRepo    *repo.WarehouseRepository
	userRepo         *repo.UserRepository
	systemConfigRepo *repo.SystemConfigRepository
	ids              *sonyflake.Sonyflake
}

// WarehousePlayer 表示游戏服为当前已认证连接提交的账户身份
type WarehousePlayer struct {
	UserID     int64
	PlayerName string
}

// WarehouseDeposit 表示暂扣到游戏服内存的一笔真实物品
type WarehouseDeposit struct {
	Player      WarehousePlayer
	OperationID string
	ItemID      string
	ItemData    []byte
	Stackable   bool
	Quantity    int64
}

// WarehouseWithdrawal 绑定准备阶段的库存记录和数量
type WarehouseWithdrawal struct {
	Player      WarehousePlayer
	OperationID string
	InventoryID int64
	Quantity    int64
}

// authorize 逐次确认可信服务器与当前可用账户，大小写名字必须匹配
func (b *WarehouseBiz) authorize(ctx context.Context, player WarehousePlayer) (int64, error) {
	server, ok := AuthenticatedServer(ctx)
	if !ok || server.ID <= 0 {
		return 0, ErrWarehouseIdentity
	}
	if player.UserID <= 0 || !playerNamePattern.MatchString(player.PlayerName) {
		return 0, ErrWarehouseInvalid
	}
	user, err := b.userRepo.FindByID(ctx, player.UserID)
	if errors.Is(err, repo.ErrUserNotFound) {
		return 0, ErrAccountUnavailable
	}
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(user.PlayerName, player.PlayerName) {
		return 0, ErrWarehouseIdentity
	}
	return server.ID, nil
}

// validateWarehouseOperation 拒绝空 UUID、非标准编码和非正数量
func validateWarehouseOperation(operationID string, quantity int64) error {
	id, err := uuid.Parse(operationID)
	if err != nil || id == uuid.Nil || id.String() != operationID || quantity <= 0 || quantity > WarehouseOperationLimit {
		return ErrWarehouseInvalid
	}
	return nil
}

// mutationIDs 在开启数据库事务前取得操作、审计和候选库存的雪花 ID
func (b *WarehouseBiz) mutationIDs(input *repo.WarehouseMutation) error {
	values := []*int64{&input.RecordID, &input.EventID}
	if input.InventoryID == 0 {
		values = append(values, &input.InventoryID)
	}
	for _, value := range values {
		id, err := b.ids.NextID()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrWarehouseInternal, err)
		}
		*value = id
	}
	return nil
}

// List 查询当前账户的正数量库存，按雪花 ID 游标分页
func (b *WarehouseBiz) List(ctx context.Context, player WarehousePlayer, afterID int64, limit int) ([]model.WarehouseItem, error) {
	if _, err := b.authorize(ctx, player); err != nil {
		return nil, err
	}
	if afterID < 0 || limit < 1 || limit > 50 {
		return nil, ErrWarehouseInvalid
	}
	return b.warehouseRepo.ListItems(ctx, player.UserID, afterID, limit)
}

// Deposit 先规范化载荷并复核重复请求，再使用当前名单快照提交入库
func (b *WarehouseBiz) Deposit(ctx context.Context, request WarehouseDeposit) (*model.WarehouseOperation, error) {
	serverID, err := b.authorize(ctx, request.Player)
	if err != nil {
		return nil, err
	}
	if err := validateWarehouseOperation(request.OperationID, request.Quantity); err != nil {
		return nil, err
	}
	if !request.Stackable && request.Quantity != 1 {
		return nil, ErrWarehouseInvalid
	}
	document, err := itemdata.Parse(request.ItemID, request.ItemData)
	if err != nil {
		return nil, err
	}
	input := repo.WarehouseMutation{OperationID: request.OperationID, UserID: request.Player.UserID, ServerID: serverID,
		ItemID: request.ItemID, ItemData: document.Canonical, Stackable: request.Stackable,
		Quantity: request.Quantity, QuantityLimit: WarehouseQuantityLimit}
	// 已提交操作不因后来名单变更变成失败，否则游戏服可能错误归还暂存
	previous, err := b.warehouseRepo.FindOperation(ctx, input.UserID, serverID, input.OperationID)
	if err == nil {
		if previous.Direction != model.WarehouseDeposit || previous.ItemID != input.ItemID || previous.Quantity != input.Quantity || previous.Stackable != input.Stackable || !bytes.Equal(previous.ItemData, input.ItemData) {
			return nil, repo.ErrWarehouseOperationConflict
		}
		return previous, warehouseRejectedError(previous)
	}
	if !errors.Is(err, repo.ErrWarehouseOperationNotFound) {
		return nil, err
	}
	policy, err := b.readPolicy(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := document.Allowed(policy)
	if err != nil {
		return nil, err
	}
	input.DepositDenied = !allowed
	if err := b.mutationIDs(&input); err != nil {
		return nil, err
	}
	result, err := b.warehouseRepo.Deposit(ctx, input)
	logWarehouseResult(input.OperationID, serverID, input.UserID, "deposit", result, err)
	return result, err
}

// Prepare 只查询最新库存供游戏服检查，不扣库、不预留也不创建发放授权
func (b *WarehouseBiz) Prepare(ctx context.Context, request WarehouseWithdrawal) (*model.WarehouseItem, error) {
	if _, err := b.authorize(ctx, request.Player); err != nil {
		return nil, err
	}
	if err := validateWarehouseOperation(request.OperationID, request.Quantity); err != nil {
		return nil, err
	}
	if request.InventoryID <= 0 {
		return nil, ErrWarehouseInvalid
	}
	item, err := b.warehouseRepo.FindItem(ctx, request.Player.UserID, request.InventoryID)
	if err != nil {
		return nil, err
	}
	if item.Quantity < request.Quantity {
		return nil, repo.ErrWarehouseItemNotFound
	}
	return item, nil
}

// Commit 在游戏服准备完成后使用条件扣减，重复请求只返回已有操作结果
func (b *WarehouseBiz) Commit(ctx context.Context, request WarehouseWithdrawal, expectedHash []byte) (*model.WarehouseOperation, error) {
	serverID, err := b.authorize(ctx, request.Player)
	if err != nil {
		return nil, err
	}
	if err := validateWarehouseOperation(request.OperationID, request.Quantity); err != nil {
		return nil, err
	}
	if request.InventoryID <= 0 || len(expectedHash) != 32 {
		return nil, ErrWarehouseInvalid
	}
	previous, err := b.warehouseRepo.FindOperation(ctx, request.Player.UserID, serverID, request.OperationID)
	if err == nil {
		if previous.Direction != model.WarehouseWithdrawal || previous.InventoryID != request.InventoryID || previous.Quantity != request.Quantity || !bytes.Equal(previous.ItemDataHash, expectedHash) {
			return nil, repo.ErrWarehouseOperationConflict
		}
		return previous, nil
	}
	if !errors.Is(err, repo.ErrWarehouseOperationNotFound) {
		return nil, err
	}
	item, err := b.warehouseRepo.FindItem(ctx, request.Player.UserID, request.InventoryID)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(item.ItemDataHash, expectedHash) {
		return nil, repo.ErrWarehouseStateConflict
	}
	input := repo.WarehouseMutation{OperationID: request.OperationID, InventoryID: item.ID,
		UserID: request.Player.UserID, ServerID: serverID, ItemID: item.ItemID, ItemData: item.ItemData,
		Stackable: item.Stackable, Quantity: request.Quantity, QuantityLimit: WarehouseQuantityLimit}
	if err := b.mutationIDs(&input); err != nil {
		return nil, err
	}
	result, err := b.warehouseRepo.Debit(ctx, input)
	logWarehouseResult(input.OperationID, serverID, input.UserID, "withdraw", result, err)
	return result, err
}

// operationOwner 限定历史结算到原服务器与原账户，不开放新的库存操作
// 已删除账户也允许原可信游戏服结算已有操作，避免把确定未发放变成无法退库
func operationOwner(ctx context.Context, player WarehousePlayer, operationID string) (int64, error) {
	server, ok := AuthenticatedServer(ctx)
	if !ok || server.ID <= 0 {
		return 0, ErrWarehouseIdentity
	}
	if player.UserID <= 0 || !playerNamePattern.MatchString(player.PlayerName) {
		return 0, ErrWarehouseInvalid
	}
	if err := validateWarehouseOperation(operationID, 1); err != nil {
		return 0, err
	}
	return server.ID, nil
}

// Complete 记录确定的发放或未发放结果，重复同结果不会重复退库
func (b *WarehouseBiz) Complete(ctx context.Context, player WarehousePlayer, operationID string, delivered bool, reason string) (*model.WarehouseOperation, error) {
	serverID, err := operationOwner(ctx, player, operationID)
	if err != nil {
		return nil, err
	}
	// 仅接受固定失败分类，避免任意异常正文或控制字符进入资产审计
	if delivered {
		reason = "delivered"
	} else {
		switch reason {
		case "inventory_full", "player_unavailable", "restore_failed", "state_changed":
		default:
			return nil, ErrWarehouseInvalid
		}
	}
	eventID, err := b.ids.NextID()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWarehouseInternal, err)
	}
	result, err := b.warehouseRepo.Finish(ctx, repo.WarehouseFinish{OperationID: operationID, UserID: player.UserID, ServerID: serverID, EventID: eventID, Delivered: delivered, Reason: reason})
	logWarehouseResult(operationID, serverID, player.UserID, "complete", result, err)
	return result, err
}

// Operation 只允许原可信服务器查询指定账户的操作，不根据查询结果自动发物
func (b *WarehouseBiz) Operation(ctx context.Context, player WarehousePlayer, operationID string) (*model.WarehouseOperation, error) {
	serverID, err := operationOwner(ctx, player, operationID)
	if err != nil {
		return nil, err
	}
	return b.warehouseRepo.FindOperation(ctx, player.UserID, serverID, operationID)
}

// readPolicy 每次读取一个完整 JSON 快照，不混用新旧名单配置
func (b *WarehouseBiz) readPolicy(ctx context.Context) (itemdata.Policy, error) {
	return b.systemConfigRepo.WarehousePolicy(ctx)
}

// requireWarehouseAdmin 读取当前账户权限，配置操作不信任令牌中的历史角色
func (b *WarehouseBiz) requireWarehouseAdmin(ctx context.Context, userID int64) error {
	user, err := b.userRepo.FindByID(ctx, userID)
	if errors.Is(err, repo.ErrUserNotFound) {
		return ErrAccountUnavailable
	}
	if err != nil {
		return err
	}
	if user.IsAdmin != 1 {
		return ErrAdminRequired
	}
	return nil
}

// Policy 返回管理员可审查的完整名单快照
func (b *WarehouseBiz) Policy(ctx context.Context, actorID int64) (itemdata.Policy, error) {
	if err := b.requireWarehouseAdmin(ctx, actorID); err != nil {
		return itemdata.Policy{}, err
	}
	return b.readPolicy(ctx)
}

// SetPolicy 校验有限规则后一次替换配置，下次入库校验读取新快照
func (b *WarehouseBiz) SetPolicy(ctx context.Context, actorID int64, policy itemdata.Policy) error {
	if err := b.requireWarehouseAdmin(ctx, actorID); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return ErrWarehouseInvalid
	}
	if len(encoded) > 65536 {
		return ErrWarehouseInvalid
	}
	return b.systemConfigRepo.SetWarehousePolicy(ctx, string(encoded))
}

// logWarehouseResult 输出关联标识和已提交结果，不输出载荷与底层数据库详情
func logWarehouseResult(operationID string, serverID, userID int64, action string, operation *model.WarehouseOperation, err error) {
	args := []any{"operation_id", operationID, "server_id", serverID, "user_id", userID, "action", action}
	if operation != nil {
		args = append(args, "inventory_id", operation.InventoryID, "item_id", operation.ItemID,
			"quantity", operation.Quantity, "state", operation.State,
			"item_data_hash", hex.EncodeToString(operation.ItemDataHash), "reason", operation.Reason)
	}
	if err != nil {
		slog.Warn("仓库操作未确认成功", args...)
		return
	}
	slog.Info("仓库操作已提交", args...)
}

// warehouseRejectedError 保留同一操作首次提交的明确拒绝，不因名单变化改变结果
func warehouseRejectedError(operation *model.WarehouseOperation) error {
	if operation.State != model.WarehouseRejected {
		return nil
	}
	switch operation.Reason {
	case "denied":
		return repo.ErrWarehouseDepositDenied
	case "quantity_limit":
		return repo.ErrWarehouseQuantityLimit
	case "hash_collision":
		return repo.ErrWarehouseHashCollision
	default:
		return ErrWarehouseInternal
	}
}
