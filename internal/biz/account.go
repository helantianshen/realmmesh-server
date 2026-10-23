// 账户：当前身份查询、自助改密及管理员删除
// 通过用户仓储读取未删除账户，向协议层交付不含密码的账户信息

// Package biz 实现账户注册、登录与游戏服管理规则，通过仓储访问持久化状态
package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/helantianshen/realmmesh-server/internal/repo"
)

// AccountBiz 读取当前账户状态，并协调账户变更及提交后的通知
type AccountBiz struct {
	businessOperationBiz *BusinessOperationBiz
	userRepo             *repo.UserRepository
	loginBiz             *LoginBiz
	accountEventBiz      *AccountEventBiz
}

// PasswordChangeResult 复用密码验证状态及休息信息，成功和账户不存在也要求踢出
// Status 沿用 LoginStatus 的验证分类，但成功表示改密已提交，不授予登录状态
type PasswordChangeResult LoginResult

// ChangePassword 校验旧密码并原子提交新密码与共享失败状态，再尽力广播
// 业务拒绝返回正常结果，通知失败不改变已经提交的改密结果
func (accountBiz *AccountBiz) ChangePassword(ctx context.Context, playerName, oldPassword, newPassword string) (*PasswordChangeResult, error) {
	result, err := accountBiz.changePassword(ctx, playerName, oldPassword, newPassword)

	// 成功改密的通知已先处理，再记录审计；审计等待不会延后账户变更广播
	accountBiz.businessOperationBiz.RecordAuthentication(ctx, OperationChangePassword, playerName, (*LoginResult)(result), err)
	return result, err
}

// changePassword 提交密码与共享失败状态，成功后先广播，再交付业务结果
func (accountBiz *AccountBiz) changePassword(ctx context.Context, playerName, oldPassword, newPassword string) (*PasswordChangeResult, error) {
	result, err := accountBiz.loginBiz.authenticate(ctx, playerName, oldPassword, &newPassword)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasswordChangeFailed, err)
	}

	response := PasswordChangeResult(*result)
	if result.Status == LoginSucceeded {
		response.Kick = true
		accountBiz.publishChange(result.PlayerName, AccountPasswordChanged)
	} else if result.Status == LoginUnregistered {
		response.Kick = true
	}
	return &response, nil
}

// Delete 校验操作者当前管理员权限，软删除普通账户后发布通知
// 管理员账户包括操作者自身均不可删除，重复删除返回目标不存在且不再次广播
func (accountBiz *AccountBiz) Delete(ctx context.Context, actorID, userID int64) error {
	playerName, err := accountBiz.delete(ctx, actorID, userID)
	accountBiz.businessOperationBiz.Record(ctx, OperationEvent{
		Operation:   OperationDeleteAccount,
		ActorUserID: actorID,
		TargetType:  "account",
		TargetID:    userID,
		PlayerName:  playerName,
	}, err)
	return err
}

// delete 校验管理员，软删除普通账户并广播，返回已删除账户名供审计使用
func (accountBiz *AccountBiz) delete(ctx context.Context, actorID, userID int64) (string, error) {
	actor, err := accountBiz.Current(ctx, actorID)
	if err != nil {
		return "", err
	}
	if !actor.IsAdmin {
		return "", ErrAdminRequired
	}

	playerName, err := accountBiz.userRepo.DeleteRegular(ctx, userID)
	switch {
	case errors.Is(err, repo.ErrAdminDeletionForbidden):
		return "", ErrAdminDeletionForbidden
	case errors.Is(err, repo.ErrUserNotFound):
		return "", ErrAccountNotFound
	case err != nil:
		return "", fmt.Errorf("%w: %w", ErrAccountDeleteFailed, err)
	}

	accountBiz.publishChange(playerName, AccountDeleted)
	return playerName, nil
}

// publishChange 在持久化成功后发送通知，日志仅记录安全原因，不输出请求或密码
func (accountBiz *AccountBiz) publishChange(playerName string, reason AccountEventReason) {
	if err := accountBiz.accountEventBiz.Publish(AccountEvent{PlayerName: playerName, Reason: reason}); err != nil {
		slog.Warn("账户变更已提交，通知未发送", "reason", reason, "error", err)
	}
}

// Account 保存可交付的账户信息，不包含密码和登录失败状态
type Account struct {
	ID         int64
	PlayerName string
	IsAdmin    bool
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
