// gRPC 传输基础设施：服务构建与处理器注册
// 组装服务器认证和日志拦截器，并将 Echo 协议绑定到处理器

package server

import (
	"log/slog"

	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"github.com/helantianshen/realmmesh-server/internal/handler"
)

// NewGRPCServer 创建 gRPC 服务并注册接口处理器
func NewGRPCServer(
	cfg config.GRPCConfig,
	serverBiz *biz.ServerBiz,
	probeHandler *handler.ProbeGRPCHandler,
) (*kgrpc.Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	srv := kgrpc.NewServer(
		kgrpc.Address(cfg.Addr),
		kgrpc.StreamInterceptor(serverStreamAuth(serverBiz)),
		kgrpc.UnaryInterceptor(rpcLoggingInterceptor(slog.Default()), serverUnaryAuth(serverBiz)),
	)

	probev1.RegisterProbeServiceServer(srv, probeHandler)

	return srv, nil
}
