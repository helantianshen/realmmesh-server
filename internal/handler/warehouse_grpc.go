// 共享仓库 gRPC 适配：将可信游戏服的请求转换为中心业务操作
// 仅在数据库提交后返回发放授权，错误响应不泄露载荷或数据库细节

package handler

import (
	"context"

	warehousev1 "github.com/helantianshen/realmmesh-server/gen/warehouse/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/model"
)

// WarehouseGRPCHandler 共享业务对象，不在协议层维护物品状态
type WarehouseGRPCHandler struct {
	warehousev1.UnimplementedWarehouseServiceServer
	warehouseBiz *biz.WarehouseBiz
}

// warehousePlayer 转换账户身份，空消息交业务层作为非法身份拒绝
func warehousePlayer(player *warehousev1.PlayerIdentity) biz.WarehousePlayer {
	return biz.WarehousePlayer{UserID: player.GetUserId(), PlayerName: player.GetPlayerName()}
}

// warehouseWithdrawal 转换准备与提交共用的不可变请求参数
func warehouseWithdrawal(request *warehousev1.WithdrawalRequest) biz.WarehouseWithdrawal {
	return biz.WarehouseWithdrawal{Player: warehousePlayer(request.GetPlayer()), OperationID: request.GetOperationId(), InventoryID: request.GetInventoryId(), Quantity: request.GetQuantity()}
}

// warehouseItem 仅在准备取出时携带单件载荷，列表保留摘要以限制响应大小
func warehouseItem(item model.WarehouseItem, includeData bool) *warehousev1.Item {
	result := &warehousev1.Item{InventoryId: item.ID, ItemId: item.ItemID, ItemDataHash: item.ItemDataHash, Stackable: item.Stackable, Quantity: item.Quantity}
	if includeData {
		result.ItemData = item.ItemData
	}
	return result
}

// warehouseOperation 只输出已经提交的业务状态，不把未知状态当作发物许可
func warehouseOperation(operation *model.WarehouseOperation) (*warehousev1.Operation, error) {
	var state warehousev1.OperationState
	switch operation.State {
	case model.WarehouseCommitted:
		state = warehousev1.OperationState_OPERATION_STATE_DEPOSITED
	case model.WarehouseDebited:
		state = warehousev1.OperationState_OPERATION_STATE_DEBITED
	case model.WarehouseDelivered:
		state = warehousev1.OperationState_OPERATION_STATE_DELIVERED
	case model.WarehouseRefunded:
		state = warehousev1.OperationState_OPERATION_STATE_REFUNDED
	case model.WarehouseRejected:
		state = warehousev1.OperationState_OPERATION_STATE_REJECTED
	default:
		return nil, errRPCWarehouseInternal
	}
	return &warehousev1.Operation{OperationId: operation.OperationID, State: state, InventoryId: operation.InventoryID, ItemId: operation.ItemID, Quantity: operation.Quantity, ItemDataHash: operation.ItemDataHash}, nil
}

// ListItems 返回正数量库存摘要，游标只表示下一页位置
// RPC /realmmesh.warehouse.v1.WarehouseService/ListItems
func (h *WarehouseGRPCHandler) ListItems(ctx context.Context, request *warehousev1.ListItemsRequest) (*warehousev1.ListItemsResponse, error) {
	limit := int(request.GetLimit())
	if limit == 0 {
		limit = 20
	}
	items, err := h.warehouseBiz.List(ctx, warehousePlayer(request.GetPlayer()), request.GetAfterId(), limit)
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	result := &warehousev1.ListItemsResponse{}
	for _, item := range items {
		result.Items = append(result.Items, warehouseItem(item, false))
	}
	if len(items) == limit {
		result.NextAfterId = items[len(items)-1].ID
	}
	return result, nil
}

// Deposit 在暂扣物品通过中心校验并提交后返回结果
// RPC /realmmesh.warehouse.v1.WarehouseService/Deposit
func (h *WarehouseGRPCHandler) Deposit(ctx context.Context, request *warehousev1.DepositRequest) (*warehousev1.Operation, error) {
	result, err := h.warehouseBiz.Deposit(ctx, biz.WarehouseDeposit{Player: warehousePlayer(request.GetPlayer()), OperationID: request.GetOperationId(), ItemID: request.GetItemId(), ItemData: request.GetItemData(), Stackable: request.GetStackable(), Quantity: request.GetQuantity()})
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	return warehouseOperation(result)
}

// PrepareWithdrawal 查询物品快照用于游戏服检查，此响应不授权发放
// RPC /realmmesh.warehouse.v1.WarehouseService/PrepareWithdrawal
func (h *WarehouseGRPCHandler) PrepareWithdrawal(ctx context.Context, request *warehousev1.WithdrawalRequest) (*warehousev1.PrepareWithdrawalResponse, error) {
	result, err := h.warehouseBiz.Prepare(ctx, warehouseWithdrawal(request))
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	return &warehousev1.PrepareWithdrawalResponse{OperationId: request.GetOperationId(), Item: warehouseItem(*result, true), Quantity: request.GetQuantity()}, nil
}

// CommitWithdrawal 接受尚未发物的准备确认，提交扣库后返回结果
// RPC /realmmesh.warehouse.v1.WarehouseService/CommitWithdrawal
func (h *WarehouseGRPCHandler) CommitWithdrawal(ctx context.Context, request *warehousev1.CommitWithdrawalRequest) (*warehousev1.Operation, error) {
	result, err := h.warehouseBiz.Commit(ctx, warehouseWithdrawal(request.GetRequest()), request.GetItemDataHash())
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	return warehouseOperation(result)
}

// CompleteWithdrawal 记录确定发放或明确未发放结果，未知结果不得调用退款
// RPC /realmmesh.warehouse.v1.WarehouseService/CompleteWithdrawal
func (h *WarehouseGRPCHandler) CompleteWithdrawal(ctx context.Context, request *warehousev1.CompleteWithdrawalRequest) (*warehousev1.Operation, error) {
	result, err := h.warehouseBiz.Complete(ctx, warehousePlayer(request.GetPlayer()), request.GetOperationId(), request.GetDelivered(), request.GetReason())
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	return warehouseOperation(result)
}

// GetOperation 读取原服务器操作结果，查询本身不产生发物授权或补偿
// RPC /realmmesh.warehouse.v1.WarehouseService/GetOperation
func (h *WarehouseGRPCHandler) GetOperation(ctx context.Context, request *warehousev1.GetOperationRequest) (*warehousev1.Operation, error) {
	result, err := h.warehouseBiz.Operation(ctx, warehousePlayer(request.GetPlayer()), request.GetOperationId())
	if err != nil {
		return nil, rpcWarehouseError(err)
	}
	return warehouseOperation(result)
}
