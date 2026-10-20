// gRPC 传输基础设施：服务构建与认证拦截
// 组装逐次认证、流式认证和请求日志拦截器

package server

import (
	"log/slog"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"
)

// NewGRPCServer 校验监听配置并创建带服务器认证的 gRPC 服务
func NewGRPCServer(
	cfg config.GRPCConfig,
	serverBiz *biz.ServerBiz,
) (*kgrpc.Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	srv := kgrpc.NewServer(
		kgrpc.Address(cfg.Addr),
		kgrpc.StreamInterceptor(serverStreamAuth(serverBiz)),
		kgrpc.UnaryInterceptor(rpcLoggingInterceptor(slog.Default()), serverUnaryAuth(serverBiz)),
	)

	return srv, nil
}
