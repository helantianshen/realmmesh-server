// 账户：当前身份与权限查询
// 通过用户仓储读取未删除账户，向协议层交付不含密码的账户信息

// Package biz 实现账户注册、登录与游戏服管理规则，通过仓储访问持久化状态
package biz

import (
	"context"
	"errors"
	"fmt"

	"github.com/helantianshen/realmmesh-server/internal/repo"
)

// AccountBiz 读取当前账户状态，供受保护入口验证身份
type AccountBiz struct {
	userRepo *repo.UserRepository
}

// Account 保存可交付的账户信息，不包含密码和登录失败状态
type Account struct {
	ID         int64
	PlayerName string
	IsAdmin    bool
}

// NewAccountBiz 组装账户查询业务
func NewAccountBiz(userRepo *repo.UserRepository) *AccountBiz {
	return &AccountBiz{userRepo: userRepo}
}

// Current 每次读取当前未删除账户，权限变更在后续请求中生效
func (accountBiz *AccountBiz) Current(ctx context.Context, userID int64) (*Account, error) {
	user, err := accountBiz.userRepo.FindByID(ctx, userID)
	if errors.Is(err, repo.ErrUserNotFound) {
		return nil, ErrAccountUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAccountLookup, err)
	}

	// 只交付身份与当前权限，不把数据库模型直接交给协议层序列化
	return &Account{
		ID:         user.ID,
		PlayerName: user.PlayerName,
		IsAdmin:    user.IsAdmin == 1,
	}, nil
}
