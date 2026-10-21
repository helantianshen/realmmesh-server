// 账户管理：管理员删除普通账户的 HTTP 协议适配

package handler

import (
	"net/http"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
)

// AccountHandler 将管理员账户操作交给账户业务，不接受请求指定操作者权限
type AccountHandler struct {
	accountBiz *biz.AccountBiz
}

// deleteAccountRequest 使用十进制字符串承载雪花 ID，避免前端数字精度丢失
type deleteAccountRequest struct {
	UserID int64 `json:"user_id,string"`
}

// Delete 删除普通账户，操作者来自已验证 JWT，当前权限由业务层查库
// POST /api/v1/admin/users/delete
func (h *AccountHandler) Delete(ctx khttp.Context, actorID int64) error {
	var request deleteAccountRequest
	if err := ctx.Bind(&request); err != nil || request.UserID <= 0 {
		return ErrInvalidRequest
	}

	if err := h.accountBiz.Delete(ctx, actorID, request.UserID); err != nil {
		return BusinessError(err)
	}
	return ctx.Blob(http.StatusNoContent, "", nil)
}
