// 玩家认证：游戏服注册、登录、改密与账户变更订阅的 gRPC 协议适配
// 复用中心账户业务，将提交后的认证结果转换为协议消息，不维护玩家在线会话

package handler

import (
	"context"

	authv1 "github.com/helantianshen/realmmesh-server/gen/auth/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthGRPCHandler 接收已通过服务器凭据认证的请求，不签发 HTTP JWT
type AuthGRPCHandler struct {
	authv1.UnimplementedAuthServiceServer
	registrationBiz *biz.RegistrationBiz
	loginBiz        *biz.LoginBiz
	accountEventBiz *biz.AccountEventBiz
	accountBiz      *biz.AccountBiz
}

var _ authv1.AuthServiceServer = (*AuthGRPCHandler)(nil)

// SubscribeAccountEvents 登记订阅后发送响应头，再逐条转发已提交的账户通知
// RPC /realmmesh.auth.v1.AuthService/SubscribeAccountEvents
// 凭据校验由流拦截器执行；取消、发送失败或中心停服时释放订阅，不重试和补发
func (authGRPCHandler *AuthGRPCHandler) SubscribeAccountEvents(
	_ *authv1.SubscribeAccountEventsRequest,
	stream grpc.ServerStreamingServer[authv1.AccountEvent],
) error {
	events, unsubscribe, err := authGRPCHandler.accountEventBiz.Subscribe()
	if err != nil {
		return errRPCEventsUnavailable
	}
	defer unsubscribe()

	// 显式发送空响应头，使客户端在没有账户变更时也能确认订阅已登记
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case event, open := <-events:
			if !open {
				return errRPCEventsUnavailable
			}
			if err := ctx.Err(); err != nil {
				return status.FromContextError(err).Err()
			}

			// 业务类型在此转换为协议枚举，业务层不依赖生成代码
			var reason authv1.AccountEventReason
			switch event.Reason {
			case biz.AccountPasswordChanged:
				reason = authv1.AccountEventReason_ACCOUNT_EVENT_REASON_PASSWORD_CHANGED
			case biz.AccountDeleted:
				reason = authv1.AccountEventReason_ACCOUNT_EVENT_REASON_ACCOUNT_DELETED
			default:
				return errRPCInternal
			}
			if err := stream.Send(&authv1.AccountEvent{PlayerName: event.PlayerName, Reason: reason}); err != nil {
				return err
			}
		}
	}
}

// Register 创建普通账户，只有业务持久化成功才向游戏服返回身份
// RPC /realmmesh.auth.v1.AuthService/Register
func (authGRPCHandler *AuthGRPCHandler) Register(ctx context.Context, request *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	user, err := authGRPCHandler.registrationBiz.Register(ctx, request.GetPlayerName(), request.GetPassword())
	if err != nil {
		return nil, rpcBusinessError(err)
	}

	// 注册成功本身足以让游戏服确认本次连接，不再次验证密码或增加登录计数
	return &authv1.RegisterResponse{
		UserId:     user.ID,
		PlayerName: user.PlayerName,
	}, nil
}

// Login 先执行中心登录事务，再组织成功、未注册、错误密码或休息期响应
// RPC /realmmesh.auth.v1.AuthService/Login
// 不提前检查密码格式，休息期判断和失败计数均由 LoginBiz 决定
func (authGRPCHandler *AuthGRPCHandler) Login(ctx context.Context, request *authv1.LoginRequest) (*authv1.LoginResponse, error) {
	result, err := authGRPCHandler.loginBiz.Authenticate(ctx, request.GetPlayerName(), request.GetPassword())
	if err != nil {
		return nil, rpcBusinessError(err)
	}

	return loginRPCResponse(result)
}

// loginRPCResponse 按业务状态填充有效字段，未知结果不作为成功响应发送
// 非休息状态不设置 Timestamp，避免把零时刻误认为中心下发的截止时间
func loginRPCResponse(result *biz.LoginResult) (*authv1.LoginResponse, error) {
	if result == nil {
		return nil, errRPCInternal
	}
	response := &authv1.LoginResponse{
		UserId:     result.UserID,
		PlayerName: result.PlayerName,
	}

	switch result.Status {
	case biz.LoginSucceeded:
		response.Status = authv1.LoginStatus_LOGIN_STATUS_SUCCEEDED
		response.RemainingAttempts = result.RemainingAttempts

	case biz.LoginUnregistered:
		return &authv1.LoginResponse{Status: authv1.LoginStatus_LOGIN_STATUS_UNREGISTERED}, nil

	case biz.LoginWrongPassword:
		response.Status = authv1.LoginStatus_LOGIN_STATUS_WRONG_PASSWORD
		response.RemainingAttempts = result.RemainingAttempts

	case biz.LoginCoolingDown:
		if result.CooldownUntil == nil || result.RetryAfterSeconds <= 0 || !result.Kick {
			return nil, errRPCInternal
		}

		// New 将数据库的绝对时刻转换为标准 Timestamp，保留秒和纳秒精度
		deadline := timestamppb.New(*result.CooldownUntil)
		if err := deadline.CheckValid(); err != nil {
			return nil, errRPCInternal
		}
		response.Status = authv1.LoginStatus_LOGIN_STATUS_COOLING_DOWN
		response.CooldownUntil = deadline
		response.RetryAfterSeconds = result.RetryAfterSeconds
		response.Kick = true

	default:
		return nil, errRPCInternal
	}

	return response, nil
}

// ChangePassword 将共享密码校验结果转换为改密响应，不授予玩家登录状态
// RPC /realmmesh.auth.v1.AuthService/ChangePassword
func (authGRPCHandler *AuthGRPCHandler) ChangePassword(ctx context.Context, request *authv1.ChangePasswordRequest) (*authv1.ChangePasswordResponse, error) {
	result, err := authGRPCHandler.accountBiz.ChangePassword(ctx, request.GetPlayerName(), request.GetOldPassword(), request.GetNewPassword())
	if err != nil {
		return nil, rpcBusinessError(err)
	}

	response := &authv1.ChangePasswordResponse{
		RemainingAttempts: result.RemainingAttempts,
		RetryAfterSeconds: result.RetryAfterSeconds,
		Kick:              result.Kick,
	}
	switch result.Status {
	case biz.LoginSucceeded:
		response.Status = authv1.ChangePasswordStatus_CHANGE_PASSWORD_STATUS_SUCCEEDED
	case biz.LoginUnregistered:
		response.Status = authv1.ChangePasswordStatus_CHANGE_PASSWORD_STATUS_UNREGISTERED
	case biz.LoginWrongPassword:
		response.Status = authv1.ChangePasswordStatus_CHANGE_PASSWORD_STATUS_WRONG_PASSWORD
	case biz.LoginCoolingDown:
		response.Status = authv1.ChangePasswordStatus_CHANGE_PASSWORD_STATUS_COOLING_DOWN
		// 数据库截止时间转换为标准 Timestamp，非休息结果不设置该字段
		response.CooldownUntil = timestamppb.New(*result.CooldownUntil)
		if err := response.CooldownUntil.CheckValid(); err != nil {
			return nil, errRPCInternal
		}
	default:
		return nil, errRPCInternal
	}
	return response, nil
}
