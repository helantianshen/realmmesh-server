package data

import (
	"context"
	"errors"
	"fmt"
)

var ErrDatabase = errors.New("数据库操作失败")

// databaseError 保留可判定的错误类别，但不暴露驱动错误中的凭据或 SQL 参数
func databaseError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrDatabase, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w: %w", operation, ErrDatabase, context.Canceled)
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return fmt.Errorf("%s: %w（SQLSTATE %s）", operation, ErrDatabase, state.SQLState())
	}
	return fmt.Errorf("%s: %w，请检查数据库状态与连接配置", operation, ErrDatabase)
}

var ErrSystemConfigInitialization = errors.New("初始化系统配置失败")
