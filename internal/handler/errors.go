// HTTP 协议边界：对外错误契约
// 定义固定状态和业务码，将业务失败转换为不含底层详情的响应

package handler

import (
	"errors"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
)

// 固定协议错误交给 Kratos 编码为 code、reason、message 和 metadata
var (
	ErrInvalidRequest = kerrors.BadRequest("INVALID_REQUEST", "请求格式或参数无效")
	ErrUnauthorized   = kerrors.Unauthorized("UNAUTHORIZED", "请重新登录")
	ErrForbidden      = kerrors.Forbidden("ADMIN_REQUIRED", "需要管理员权限")
	ErrInternal       = kerrors.InternalServer("INTERNAL_ERROR", "服务暂时不可用")
	ErrWrongPassword  = kerrors.Unauthorized("WRONG_PASSWORD", "密码错误")
	ErrUnregistered   = kerrors.Unauthorized("UNREGISTERED", "账户不存在")
	ErrCoolingDown    = kerrors.New(http.StatusTooManyRequests, "COOLING_DOWN", "登录尝试过多，请稍后重试")
)

// BusinessError 将业务错误映射为固定协议错误，不向响应传递数据库或驱动详情
// 已知业务错误提供明确提示；未知错误统一返回服务不可用，不使用原始 err.Error()
func BusinessError(err error) error {
	switch {
	case errors.Is(err, biz.ErrPlayerNameInvalid):
		return kerrors.BadRequest("PLAYER_NAME_INVALID", biz.ErrPlayerNameInvalid.Error())
	case errors.Is(err, biz.ErrPasswordInvalid):
		return kerrors.BadRequest("PASSWORD_INVALID", biz.ErrPasswordInvalid.Error())
	case errors.Is(err, biz.ErrPlayerNameTaken):
		return kerrors.Conflict("PLAYER_NAME_TAKEN", biz.ErrPlayerNameTaken.Error())

	case errors.Is(err, biz.ErrAccountUnavailable):
		return ErrUnauthorized
	case errors.Is(err, biz.ErrAdminRequired):
		return ErrForbidden

	case errors.Is(err, biz.ErrServerNameInvalid):
		return kerrors.BadRequest("SERVER_NAME_INVALID", biz.ErrServerNameInvalid.Error())
	case errors.Is(err, biz.ErrServerNotFound):
		return kerrors.NotFound("SERVER_NOT_FOUND", biz.ErrServerNotFound.Error())
	case errors.Is(err, pagination.ErrInvalid):
		return kerrors.BadRequest("PAGINATION_INVALID", pagination.ErrInvalid.Error())

	default:
		return ErrInternal
	}
}
