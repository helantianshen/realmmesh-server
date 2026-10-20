// 账户认证：密码登录与休息期策略
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

// NewLoginBiz 组装用户仓储和中心配置仓储，所有服务器共用中心策略
func NewLoginBiz(userRepo *repo.UserRepository, systemConfigRepo *repo.SystemConfigRepository) *LoginBiz {
	return &LoginBiz{userRepo: userRepo, systemConfigRepo: systemConfigRepo}
}

// Authenticate 校验登录并持久化失败状态，正常拒绝通过 LoginResult 返回
//
// 先读取配置，再锁定账户检查休息期。未到期时直接返回 kick，不能校验密码或延长时间
// 到期后重置状态，再验证密码；成功清零，失败达到阈值则进入新的休息期
// 只有数据库事务提交成功后才返回结果，基础设施异常不视为密码错误
func (loginBiz *LoginBiz) Authenticate(ctx context.Context, playerName, plainPassword string) (*LoginResult, error) {
	if !playerNamePattern.MatchString(playerName) {
		return nil, ErrPlayerNameInvalid
	}

	policy, err := loginBiz.systemConfigRepo.LoginPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoginFailed, err)
	}

	var result *LoginResult
	err = loginBiz.userRepo.UpdateLoginState(ctx, playerName, func(user *model.User, now time.Time) (bool, error) {
		// 休息判断必须在密码格式和哈希校验之前，每次请求都返回踢出结果
		if user.CooldownUntil != nil && now.Before(*user.CooldownUntil) {
			result = cooldownResult(user, now)
			return false, nil
		}

		if user.CooldownUntil != nil {
			user.LoginFailures = 0
			user.CooldownUntil = nil
		}

		// 格式不符的输入也属于错误密码，但不必执行昂贵的 Argon2id 计算
		verifyErr := password.ErrMismatch
		if passwordPattern.MatchString(plainPassword) {
			verifyErr = password.Verify(plainPassword, user.PasswordHash)
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if verifyErr != nil && !errors.Is(verifyErr, password.ErrMismatch) {
			return false, verifyErr
		}

		if verifyErr == nil {
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
		return nil, fmt.Errorf("%w: %w", ErrLoginFailed, err)
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
