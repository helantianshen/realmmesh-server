// HTTP 与 gRPC 协议边界：对外错误契约
// 定义固定状态和业务码，将业务失败转换为不含底层详情的响应

package handler

import (
	"context"
	"errors"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/itemdata"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RPC 固定错误不包含请求值、密码哈希或底层数据库详情
var (
	errRPCEventsUnavailable      = status.Error(codes.Unavailable, "账户通知通道已关闭")
	errRPCConfigUnavailable      = status.Error(codes.Unavailable, "配置数据暂时不可用")
	errRPCConfigInvalid          = status.Error(codes.Internal, "中心登录时限配置缺失或无效")
	errRPCConfigInternal         = status.Error(codes.Internal, "读取游戏服配置失败")
	errRPCPlayerNameInvalid      = status.Error(codes.InvalidArgument, "玩家名字格式无效")
	errRPCPasswordInvalid        = status.Error(codes.InvalidArgument, "密码格式无效")
	errRPCPlayerNameTaken        = status.Error(codes.AlreadyExists, "玩家名字已被占用")
	errRPCUnavailable            = status.Error(codes.Unavailable, "账户数据暂时不可用")
	errRPCAuthenticationConflict = status.Error(codes.Aborted, "账户状态已变化，请重新尝试")
	errRPCInternal               = status.Error(codes.Internal, "认证处理失败")
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
	case errors.Is(err, biz.ErrAuthenticationConflict):
		return errRPCAuthenticationConflict
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
	case errors.Is(err, biz.ErrAuthenticationConflict):
		return kerrors.Conflict("AUTHENTICATION_CONFLICT", biz.ErrAuthenticationConflict.Error())
	case errors.Is(err, biz.ErrPlayerNameInvalid):
		return kerrors.BadRequest("PLAYER_NAME_INVALID", biz.ErrPlayerNameInvalid.Error())
	case errors.Is(err, biz.ErrPasswordInvalid):
		return kerrors.BadRequest("PASSWORD_INVALID", biz.ErrPasswordInvalid.Error())
	case errors.Is(err, biz.ErrPlayerNameTaken):
		return kerrors.Conflict("PLAYER_NAME_TAKEN", biz.ErrPlayerNameTaken.Error())

	case errors.Is(err, biz.ErrAccountNotFound):
		return kerrors.NotFound("ACCOUNT_NOT_FOUND", biz.ErrAccountNotFound.Error())
	case errors.Is(err, biz.ErrAdminDeletionForbidden):
		return kerrors.Forbidden("ADMIN_DELETION_FORBIDDEN", biz.ErrAdminDeletionForbidden.Error())
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

var (
	errRPCAuditInvalid         = status.Error(codes.InvalidArgument, "玩家连接审计参数无效")
	errRPCAuditInternal        = status.Error(codes.Internal, "玩家连接审计写入失败")
	errRPCAuditUnauthenticated = status.Error(codes.Unauthenticated, "缺少已认证服务器身份")
)

// rpcAuditError 保留调用取消和期限状态，其他错误只返回固定安全摘要
func rpcAuditError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, biz.ErrPlayerNameInvalid), errors.Is(err, biz.ErrPlayerConnectionEventInvalid):
		return errRPCAuditInvalid
	default:
		return errRPCAuditInternal
	}
}

var (
	errRPCWarehouseInternal = status.Error(codes.Internal, "仓库结果未确认，请保留操作编号核对")
	errRPCWarehouseInvalid  = status.Error(codes.InvalidArgument, "仓库请求或物品数据无效")
	errRPCWarehouseIdentity = status.Error(codes.PermissionDenied, "仓库账户或服务器身份无效")
	errRPCWarehouseDenied   = status.Error(codes.FailedPrecondition, "物品不符合存入名单")
	errRPCWarehouseConflict = status.Error(codes.AlreadyExists, "仓库操作编号或结果冲突")
	errRPCWarehouseMissing  = status.Error(codes.NotFound, "库存不足或记录不存在")
	errRPCWarehouseLimit    = status.Error(codes.ResourceExhausted, "仓库数量超过上限")
	errRPCWarehouseData     = status.Error(codes.FailedPrecondition, "物品数据或状态不一致")
)

// rpcWarehouseError 区分明确拒绝与未知提交结果，数据库故障不能被游戏服当作安全退款依据
func rpcWarehouseError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, biz.ErrWarehouseInvalid), errors.Is(err, repo.ErrWarehouseInvalid), errors.Is(err, itemdata.ErrMalformed), errors.Is(err, itemdata.ErrLimit), errors.Is(err, itemdata.ErrItemID):
		return errRPCWarehouseInvalid
	case errors.Is(err, biz.ErrWarehouseIdentity), errors.Is(err, biz.ErrAccountUnavailable):
		return errRPCWarehouseIdentity
	case errors.Is(err, biz.ErrWarehouseDenied), errors.Is(err, repo.ErrWarehouseDepositDenied):
		return errRPCWarehouseDenied
	case errors.Is(err, repo.ErrWarehouseOperationConflict):
		return errRPCWarehouseConflict
	case errors.Is(err, repo.ErrWarehouseItemNotFound), errors.Is(err, repo.ErrWarehouseOperationNotFound):
		return errRPCWarehouseMissing
	case errors.Is(err, repo.ErrWarehouseQuantityLimit):
		return errRPCWarehouseLimit
	case errors.Is(err, repo.ErrWarehouseHashCollision), errors.Is(err, repo.ErrWarehouseStateConflict):
		return errRPCWarehouseData
	default:
		return errRPCWarehouseInternal
	}
}

// warehouseHTTPError 保留管理权限错误，名单输入错误不返回内部失败
func warehouseHTTPError(err error) error {
	if errors.Is(err, itemdata.ErrRule) || errors.Is(err, biz.ErrWarehouseInvalid) {
		return ErrInvalidRequest
	}
	return BusinessError(err)
}
