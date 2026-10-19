package service

import (
	"context"

	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
)

type ProbeGRPCHandler struct {
	probev1.UnimplementedProbeServiceServer
}

var _ probev1.ProbeServiceServer = (*ProbeGRPCHandler)(nil)

func (h *ProbeGRPCHandler) Echo(
	ctx context.Context,
	req *probev1.EchoRequest,
) (*probev1.EchoResponse, error) {
	return &probev1.EchoResponse{
		Message: req.GetMessage(),
	}, nil
}
