// 游戏服身份：gRPC 认证边界
// 校验请求凭证并传递可信身份，在流式消息收发时复查凭证状态

package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/biz"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type serverIdentityKey struct{}

// AuthenticatedServer 读取拦截器写入的服务器身份，处理器不能采用请求正文中的身份声明
func AuthenticatedServer(ctx context.Context) (biz.ServerIdentity, bool) {
	identity, ok := ctx.Value(serverIdentityKey{}).(biz.ServerIdentity)
	return identity, ok
}

// authenticateRPC 检查唯一 metadata 值，再委托业务层读取数据库认证
// 数据库异常只返回通用状态，密码、摘要和驱动错误不进入协议响应
func authenticateRPC(ctx context.Context, serverBiz *biz.ServerBiz) (*biz.ServerIdentity, error) {
	// 1. 每个字段只能出现一次，避免不同组件对重复值作出不同解释
	md, _ := metadata.FromIncomingContext(ctx)
	ids, secrets := md.Get("x-realmmesh-server-id"), md.Get("authorization")
	if len(ids) != 1 || len(secrets) != 1 || !strings.HasPrefix(secrets[0], "Bearer ") {
		return nil, errRPCCredentialInvalid
	}

	// ParseInt 以十进制读取正的 64 位服务器 ID，拒绝溢出和非规范编码
	id, err := strconv.ParseInt(ids[0], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != ids[0] {
		return nil, errRPCCredentialInvalid
	}

	// 2. 长连接通常没有整体截止时间，为每次数据库认证单独设置等待上限
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	identity, err := serverBiz.Authenticate(checkCtx, id, strings.TrimPrefix(secrets[0], "Bearer "))
	if errors.Is(err, biz.ErrServerCredentialInvalid) {
		return nil, errRPCCredentialInvalid
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, errRPCAuthUnavailable
	}
	return identity, nil
}

// serverUnaryAuth 对每个普通 RPC 读取最新凭证状态，通过后才执行处理器
func serverUnaryAuth(serverBiz *biz.ServerBiz) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		identity, err := authenticateRPC(ctx, serverBiz)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, serverIdentityKey{}, *identity), request)
	}
}

// serverStreamAuth 认证建流请求并包装所有收发操作，旧流不能只靠建流时的认证结果继续使用
// 空闲流在下一次收发时发现撤销；处理器必须响应 context 取消并返回收发错误
func serverStreamAuth(serverBiz *biz.ServerBiz) grpc.StreamServerInterceptor {
	return func(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		identity, err := authenticateRPC(stream.Context(), serverBiz)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.WithValue(stream.Context(), serverIdentityKey{}, *identity))
		defer cancel()
		guarded := &authenticatedStream{
			ServerStream: stream,
			ctx:          ctx,
			cancel:       cancel,
			serverBiz:    serverBiz,
		}
		return handler(service, guarded)
	}
}

type authenticatedStream struct {
	grpc.ServerStream
	ctx       context.Context
	cancel    context.CancelFunc
	serverBiz *biz.ServerBiz
}

// Context 提供已验证身份和可被凭证撤销取消的请求上下文
func (s *authenticatedStream) Context() context.Context {
	return s.ctx
}

// check 再次核验建流时的原始凭证，轮换后不会自动替换为新的密钥
func (s *authenticatedStream) check() error {
	_, err := authenticateRPC(s.ctx, s.serverBiz)
	if err != nil {
		s.cancel()
	}
	return err
}

// SendMsg 在发送每条业务消息前复查状态，失效时不把消息交给底层连接
func (s *authenticatedStream) SendMsg(message any) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.ServerStream.SendMsg(message)
}

// RecvMsg 在等待消息前和收到消息后分别复查，覆盖等待期间发生的轮换或停用
func (s *authenticatedStream) RecvMsg(message any) error {
	if err := s.check(); err != nil {
		return err
	}
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	return s.check()
}
