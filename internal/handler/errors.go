// HTTP 与 gRPC 协议边界：对外错误契约
// 定义固定状态和业务码，将业务失败转换为不含底层详情的响应

package handler

import (
	"context"
	"errors"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RPC 固定错误不包含请求值、密码哈希或底层数据库详情
var (
	errRPCConfigUnavailable = status.Error(codes.Unavailable, "配置数据暂时不可用")
	errRPCConfigInvalid     = status.Error(codes.Internal, "中心登录时限配置缺失或无效")
	errRPCConfigInternal    = status.Error(codes.Internal, "读取游戏服配置失败")
	errRPCPlayerNameInvalid = status.Error(codes.InvalidArgument, "玩家名字格式无效")
	errRPCPasswordInvalid   = status.Error(codes.InvalidArgument, "注册密码格式无效")
	errRPCPlayerNameTaken   = status.Error(codes.AlreadyExists, "玩家名字已被占用")
	errRPCUnavailable       = status.Error(codes.Unavailable, "账户数据暂时不可用")
	errRPCInternal          = status.Error(codes.Internal, "认证处理失败")
)

// rpcConfigError 区分依赖故障与配置错误，保留取消和超时语义并隐藏底层详情
func rpcConfigError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.FromContextError(context.Canceled).Err()
	case errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(context.DeadlineExceeded).Err()
	case errors.Is(err, repo.ErrSystemConfigMissing), errors.Is(err, repo.ErrSystemConfigInvalid):
		return errRPCConfigInvalid
	case errors.Is(err, repo.ErrRepository):
		return errRPCConfigUnavailable
	default:
		return errRPCConfigInternal
	}
}

// rpcBusinessError 保留取消和超时语义，将业务错误转换为固定 gRPC 状态
// 仓储访问失败返回 Unavailable；配置异常、损坏哈希和未知错误返回 Internal
func rpcBusinessError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.FromContextError(context.Canceled).Err()
	case errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(context.DeadlineExceeded).Err()
	case errors.Is(err, biz.ErrPlayerNameInvalid):
		return errRPCPlayerNameInvalid
	case errors.Is(err, biz.ErrPasswordInvalid):
		return errRPCPasswordInvalid
	case errors.Is(err, biz.ErrPlayerNameTaken):
		return errRPCPlayerNameTaken
	case errors.Is(err, repo.ErrRepository):
		return errRPCUnavailable
	default:
		return errRPCInternal
	}
}

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
