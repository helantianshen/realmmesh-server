// gRPC 协议边界：认证失败状态
// 提供凭证无效和认证不可用的固定状态，避免对外暴露内部错误详情

package server

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errRPCCredentialInvalid = status.Error(codes.Unauthenticated, "服务器凭证无效")
	errRPCAuthUnavailable   = status.Error(codes.Unavailable, "服务器认证暂不可用")
)
