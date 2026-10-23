// 仓库管理 HTTP 适配：管理员查询与原子替换全局名单配置

package handler

import (
	"net/http"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/itemdata"
)

// WarehouseHandler 只提供名单配置，不直接操作离线玩家背包
type WarehouseHandler struct{ warehouseBiz *biz.WarehouseBiz }

// Policy 返回完整策略快照，权限由业务层逐次查库
// POST /api/v1/admin/warehouse/policy
func (h *WarehouseHandler) Policy(ctx khttp.Context, actorID int64) error {
	policy, err := h.warehouseBiz.Policy(ctx, actorID)
	if err != nil {
		return warehouseHTTPError(err)
	}
	return ctx.JSON(http.StatusOK, policy)
}

// setWarehousePolicyRequest 包含白名单开关及完整黑白名单集合
type setWarehousePolicyRequest struct{ itemdata.Policy }

// SetPolicy 拒绝非法规则后一次替换全部配置，不建设规则脚本语言
// POST /api/v1/admin/warehouse/set-policy
func (h *WarehouseHandler) SetPolicy(ctx khttp.Context, actorID int64) error {
	var request *setWarehousePolicyRequest
	if err := ctx.Bind(&request); err != nil || request == nil {
		return ErrInvalidRequest
	}
	if err := h.warehouseBiz.SetPolicy(ctx, actorID, request.Policy); err != nil {
		return warehouseHTTPError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]bool{"updated": true})
}
