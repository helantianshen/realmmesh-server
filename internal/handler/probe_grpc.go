// 运维诊断：gRPC Echo 协议适配
// 实现生成协议中的 Echo 处理器，原样返回已进入处理器的探测消息

package handler

import (
	"context"

	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
)

type ProbeGRPCHandler struct {
	probev1.UnimplementedProbeServiceServer
}

var _ probev1.ProbeServiceServer = (*ProbeGRPCHandler)(nil)

// Echo 原样返回探测消息，供客户端验证 RPC 通信
func (h *ProbeGRPCHandler) Echo(ctx context.Context, req *probev1.EchoRequest) (*probev1.EchoResponse, error) {
	return &probev1.EchoResponse{
		Message: req.GetMessage(),
	}, nil
}
