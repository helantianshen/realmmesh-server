// 账户认证：HTTP 协议适配
// 解析注册和登录请求、调用业务对象并签发 JWT，返回安全账户信息与登录结果

// Package handler 将 HTTP 和 gRPC 请求适配到业务调用，并组织对外响应
package handler

import (
	"net/http"
	"strconv"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// AuthHandler 将账户 HTTP 请求交给注册和登录业务，并负责令牌交付
type AuthHandler struct {
	registrationBiz *biz.RegistrationBiz
	loginBiz        *biz.LoginBiz
	tokenManager    *token.Manager
}

// credentialsRequest 是注册与登录共用的请求正文，不接受身份或权限字段
type credentialsRequest struct {
	PlayerName string `json:"player_name"`
	Password   string `json:"password"`
}

// accountResponse 只暴露账户公开信息，ID 使用字符串编码以保留整数精度
type accountResponse struct {
	ID         int64  `json:"id,string"`
	PlayerName string `json:"player_name"`
	IsAdmin    bool   `json:"is_admin"`
}

// loginResponse 交付访问令牌、截止时间和身份，不将管理员权限写入令牌
type loginResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	UserID      int64     `json:"user_id,string"`
	PlayerName  string    `json:"player_name"`
}

// NewAuthHandler 组装注册、登录与令牌签发依赖，不在请求中重复初始化
func NewAuthHandler(
	registrationBiz *biz.RegistrationBiz,
	loginBiz *biz.LoginBiz,
	tokenManager *token.Manager,
) *AuthHandler {
	return &AuthHandler{
		registrationBiz: registrationBiz,
		loginBiz:        loginBiz,
		tokenManager:    tokenManager,
	}
}

// Register 创建普通账户并返回安全信息，客户端随后通过登录接口取得令牌
func (h *AuthHandler) Register(ctx khttp.Context) error {
	var request credentialsRequest

	// 1. Bind 使用服务端 JSON 解码器，名字和密码的业务规则由注册业务校验
	if err := ctx.Bind(&request); err != nil {
		return ErrInvalidRequest
	}

	// 2. 业务层完成校验、哈希和持久化；错误统一转换为对外协议错误
	user, err := h.registrationBiz.Register(ctx, request.PlayerName, request.Password)
	if err != nil {
		return BusinessError(err)
	}

	// 3. 注册只返回普通账户信息，客户端通过登录接口取得令牌
	return ctx.JSON(http.StatusCreated, accountResponse{
		ID:         user.ID,
		PlayerName: user.PlayerName,
		IsAdmin:    false,
	})
}

// Login 复用中心失败计数与休息期，只有密码验证和状态提交成功后才签发 JWT
// 处理顺序为解析请求、执行登录、返回拒绝原因或签发令牌
// 失败计数和休息截止时间由业务层维护，HTTP 只组织响应，不执行游戏服踢人操作
func (h *AuthHandler) Login(ctx khttp.Context) error {
	// 1. 解析请求正文，不在此处提前判断密码格式，避免绕过业务层失败计数
	var request credentialsRequest
	if err := ctx.Bind(&request); err != nil {
		return ErrInvalidRequest
	}

	// 2. Authenticate 返回时，登录失败计数或成功清零的事务已经提交
	result, err := h.loginBiz.Authenticate(ctx, request.PlayerName, request.Password)
	if err != nil {
		return BusinessError(err)
	}

	// 3. 正常登录拒绝通过状态区分；WithMetadata 创建新错误，不修改共享错误对象
	switch result.Status {
	case biz.LoginUnregistered:
		return ErrUnregistered

	case biz.LoginWrongPassword:
		// FormatInt 将剩余次数写为 Kratos metadata 所要求的十进制字符串
		remainingAttempts := strconv.FormatInt(result.RemainingAttempts, 10)
		return ErrWrongPassword.WithMetadata(map[string]string{
			"remaining_attempts": remainingAttempts,
		})

	case biz.LoginCoolingDown:
		// FormatInt 以十进制表达等待秒数；RFC3339Nano 保留原截止时间的精度
		retryAfterSeconds := strconv.FormatInt(result.RetryAfterSeconds, 10)
		cooldownUntil := result.CooldownUntil.UTC().Format(time.RFC3339Nano)

		ctx.Response().Header().Set("Retry-After", retryAfterSeconds)
		return ErrCoolingDown.WithMetadata(map[string]string{
			"retry_after_seconds": retryAfterSeconds,
			"cooldown_until":      cooldownUntil,
		})

	case biz.LoginSucceeded:
		// 只有成功状态可以继续执行下面的令牌签发

	default:
		return ErrInternal
	}

	// 4. 使用已认证身份签发令牌，截止时间与 JWT 中的 exp 保持一致
	accessToken, err := h.tokenManager.Issue(result.UserID)
	if err != nil {
		return ErrInternal
	}

	return ctx.JSON(http.StatusOK, loginResponse{
		AccessToken: accessToken.Value,
		TokenType:   "Bearer",
		ExpiresAt:   accessToken.ExpiresAt,
		UserID:      result.UserID,
		PlayerName:  result.PlayerName,
	})
}

// Me 返回认证入口查询到的当前账户，不能用请求正文指定目标用户
func (h *AuthHandler) Me(ctx khttp.Context, account *biz.Account) error {
	return ctx.JSON(http.StatusOK, accountResponse{
		ID:         account.ID,
		PlayerName: account.PlayerName,
		IsAdmin:    account.IsAdmin,
	})
}
