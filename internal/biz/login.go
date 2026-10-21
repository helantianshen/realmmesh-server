// 账户认证：登录与自助改密共用的密码校验和休息期策略
// 在仓储事务内判断登录结果、更新失败次数和截止时间，并返回重试或踢人信息

package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/password"
)

type LoginStatus string

const (
	LoginSucceeded     LoginStatus = "succeeded"      // 密码验证成功
	LoginUnregistered  LoginStatus = "unregistered"   // 账户不存在或已删除，不计失败次数
	LoginWrongPassword LoginStatus = "wrong_password" // 密码错误，仍可尝试
	LoginCoolingDown   LoginStatus = "cooling_down"   // 处于休息期，游戏服应踢出当前连接
)

type LoginResult struct {
	Status            LoginStatus
	UserID            int64
	PlayerName        string
	RemainingAttempts int64
	CooldownUntil     *time.Time
	RetryAfterSeconds int64
	Kick              bool
}

type LoginBiz struct {
	userRepo         *repo.UserRepository
	systemConfigRepo *repo.SystemConfigRepository
}

// Authenticate 校验登录并持久化失败状态，正常拒绝通过 LoginResult 返回
//
// 先读取账户快照并在事务外计算密码，再锁定最新账户检查休息期和密码是否变化
// 按最新失败次数累计错误；成功清零，失败达到阈值则进入新的休息期
// 需要写入的结果仅在事务提交成功后返回，基础设施异常不视为密码错误
func (loginBiz *LoginBiz) Authenticate(ctx context.Context, playerName, plainPassword string) (*LoginResult, error) {
	result, err := loginBiz.authenticate(ctx, playerName, plainPassword, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoginFailed, err)
	}
	return result, nil
}

// authenticate 在事务外计算密码，在短事务中复核快照并提交共享认证状态
// newPassword 为 nil 表示登录；改密先检查休息期和新密码格式，再验证旧密码
// 数据库提交失败时，密码和失败状态一并回滚
func (loginBiz *LoginBiz) authenticate(ctx context.Context, playerName, plainPassword string, newPassword *string) (*LoginResult, error) {
	if !playerNamePattern.MatchString(playerName) {
		return nil, ErrPlayerNameInvalid
	}

	policy, err := loginBiz.systemConfigRepo.LoginPolicy(ctx)
	if err != nil {
		return nil, err
	}

	// 快照查询不加锁，使用数据库时钟判断已有休息期，避免应用时钟偏差
	snapshot, observedAt, err := loginBiz.userRepo.AuthenticationSnapshot(ctx, playerName)
	if errors.Is(err, repo.ErrUserNotFound) {
		return &LoginResult{Status: LoginUnregistered}, nil
	}
	if err != nil {
		return nil, err
	}
	if snapshot.CooldownUntil != nil && observedAt.Before(*snapshot.CooldownUntil) {
		return cooldownResult(snapshot, observedAt), nil
	}
	if newPassword != nil && !passwordPattern.MatchString(*newPassword) {
		return nil, ErrPasswordInvalid
	}

	// Argon2id 验证和新哈希生成均不持有数据库事务或账户行锁
	verifyErr := password.ErrMismatch
	if passwordPattern.MatchString(plainPassword) {
		verifyErr = password.Verify(plainPassword, snapshot.PasswordHash)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if verifyErr != nil && !errors.Is(verifyErr, password.ErrMismatch) {
		return nil, verifyErr
	}
	var newHash string
	if verifyErr == nil && newPassword != nil {
		newHash = password.Hash(*newPassword)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var result *LoginResult
	err = loginBiz.userRepo.UpdateAuthenticationState(ctx, playerName, func(user *model.User, now time.Time) (bool, error) {
		// 重新检查计算期间产生的休息期，其优先级高于密码快照冲突
		if user.CooldownUntil != nil && now.Before(*user.CooldownUntil) {
			result = cooldownResult(user, now)
			return false, nil
		}

		// 哈希变化表示计算基于过期密码，拒绝本次结果且不计数，不自动重算
		if user.PasswordHash != snapshot.PasswordHash {
			return false, ErrAuthenticationConflict
		}

		if user.CooldownUntil != nil {
			user.LoginFailures = 0
			user.CooldownUntil = nil
		}

		if verifyErr == nil {
			// 复用事务外生成的哈希，和最新账户的清零状态一起提交
			if newPassword != nil {
				user.PasswordHash = newHash
			}
			user.LoginFailures = 0
			result = &LoginResult{
				Status:            LoginSucceeded,
				UserID:            user.ID,
				PlayerName:        user.PlayerName,
				RemainingAttempts: policy.MaxFailures,
			}
			return true, nil
		}

		// 配置阈值可能降低，已达到新阈值时不再递增，避免整数溢出
		if user.LoginFailures < policy.MaxFailures {
			user.LoginFailures++
		}
		if user.LoginFailures >= policy.MaxFailures {
			deadline := now.Add(policy.Cooldown)
			user.CooldownUntil = &deadline
			result = cooldownResult(user, now)
			return true, nil
		}

		result = &LoginResult{
			Status:            LoginWrongPassword,
			UserID:            user.ID,
			PlayerName:        user.PlayerName,
			RemainingAttempts: policy.MaxFailures - user.LoginFailures,
		}
		return true, nil
	})

	if errors.Is(err, repo.ErrUserNotFound) {
		return &LoginResult{Status: LoginUnregistered}, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// cooldownResult 根据原截止时间返回踢出结果，剩余秒数向上取整以避免过早重试
func cooldownResult(user *model.User, now time.Time) *LoginResult {
	remaining := user.CooldownUntil.Sub(now)
	seconds := int64(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	return &LoginResult{
		Status:            LoginCoolingDown,
		UserID:            user.ID,
		PlayerName:        user.PlayerName,
		CooldownUntil:     user.CooldownUntil,
		RetryAfterSeconds: seconds,
		Kick:              true,
	}
}
