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

var ErrServerNotFound = errors.New("服务器不存在或已删除")
