// 可观测性基础设施：普通 gRPC 请求日志
// 生成或沿用请求关联标识，记录方法、状态和耗时，不记录请求正文与凭据

package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// rpcRequestID 保留单个非空关联标识，128 字节上限限制日志字段大小
func rpcRequestID(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("x-request-id")
	if len(values) == 1 && len(values[0]) > 0 && len(values[0]) <= 128 {
		return values[0]
	}
	return uuid.NewString()
}

// rpcLoggingInterceptor 记录 Unary 请求的接收和处理结果，不记录正文、metadata 或异常描述
func rpcLoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		requestID := rpcRequestID(ctx)
		method := strings.TrimPrefix(info.FullMethod, "/")
		logger.InfoContext(ctx, "rpc", "event", "rpc_server_start", "request_id", requestID, "method", method)

		// 默认状态覆盖处理器异常退出；日志不吞掉 panic，也不改变原有错误返回
		code := codes.Internal
		defer func() {
			level := slog.LevelInfo
			if code != codes.OK {
				level = slog.LevelWarn
			}
			logger.Log(ctx, level, "rpc", "event", "rpc_server_end", "request_id", requestID,
				"method", method, "status", code.String(), "duration_ms", time.Since(started).Milliseconds())
		}()

		response, err := handler(ctx, req)
		code = status.Code(err)
		// 普通 context 错误需要显式转换，否则会被识别为 Unknown
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = status.FromContextError(err).Code()
		}
		return response, err
	}
}
