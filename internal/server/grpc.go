package server

import (
	"log/slog"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"github.com/helantianshen/realmmesh-server/internal/service"
)

// NewGRPCServer 创建 gRPC 服务并注册接口处理器
func NewGRPCServer(
	addr string,
	probeHandler *service.ProbeGRPCHandler,
) *kgrpc.Server {
	srv := kgrpc.NewServer(
		kgrpc.Address(addr),
		kgrpc.UnaryInterceptor(rpcLoggingInterceptor(slog.Default())),
	)

	probev1.RegisterProbeServiceServer(srv, probeHandler)

	return srv
}
