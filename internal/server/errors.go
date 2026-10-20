package server

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errRPCCredentialInvalid = status.Error(codes.Unauthenticated, "服务器凭证无效")
	errRPCAuthUnavailable   = status.Error(codes.Unavailable, "服务器认证暂不可用")
)
