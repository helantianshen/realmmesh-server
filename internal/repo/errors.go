// 仓储层错误契约：持久化结果转换
// 区分记录缺失、约束冲突和数据库异常，保留安全的错误分类信息

package repo

import (
	"context"
	"errors"
	"fmt"
)

var ErrRepository = errors.New("数据库操作失败")

// repositoryError 保留可判定的错误类别，但不暴露驱动错误中的凭据或 SQL 参数
func repositoryError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrRepository, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w: %w", operation, ErrRepository, context.Canceled)
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return fmt.Errorf("%s: %w（SQLSTATE %s）", operation, ErrRepository, state.SQLState())
	}
	return fmt.Errorf("%s: %w，请检查数据库状态与连接配置", operation, ErrRepository)
}

var ErrPlayerNameConflict = errors.New("玩家名字唯一约束冲突")

var (
	ErrSystemConfigMissing = errors.New("必需系统配置缺失或已删除")
	ErrSystemConfigInvalid = errors.New("系统配置值无效")
)

var ErrUserNotFound = errors.New("账户不存在或已删除")

var ErrAdminDeletionForbidden = errors.New("禁止删除管理员账户")

var ErrServerNotFound = errors.New("服务器不存在或已删除")

var (
	ErrWarehouseInvalid           = errors.New("仓库操作参数无效")
	ErrWarehouseItemNotFound      = errors.New("仓库物品不存在或数量不足")
	ErrWarehouseOperationNotFound = errors.New("仓库操作不存在")
	ErrWarehouseOperationConflict = errors.New("仓库操作 ID 或完成结果冲突")
	ErrWarehouseHashCollision     = errors.New("仓库物品哈希相同但完整数据不同")
	ErrWarehouseQuantityLimit     = errors.New("仓库物品数量达到上限")
	ErrWarehouseStateConflict     = errors.New("仓库物品或操作状态不一致")
	ErrWarehouseDepositDenied     = errors.New("仓库名单拒绝本次存入")
)
