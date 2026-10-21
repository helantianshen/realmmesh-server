// 游戏服配置：将中心配置读取结果转换为 gRPC 响应

package handler

import (
	"context"

	configv1 "github.com/helantianshen/realmmesh-server/gen/config/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
)

type ConfigGRPCHandler struct {
	configv1.UnimplementedConfigServiceServer
	systemConfigBiz *biz.SystemConfigBiz
}

var _ configv1.ConfigServiceServer = (*ConfigGRPCHandler)(nil)

// GetConfig 返回全服共用配置，服务器身份由统一 gRPC 拦截器逐次校验
// RPC /realmmesh.config.v1.ConfigService/GetConfig
func (configGRPCHandler *ConfigGRPCHandler) GetConfig(ctx context.Context, _ *configv1.GetConfigRequest) (*configv1.GetConfigResponse, error) {
	config, err := configGRPCHandler.systemConfigBiz.GetConfig(ctx)
	if err != nil {
		return nil, rpcConfigError(err)
	}

	return &configv1.GetConfigResponse{LoginTimeoutSeconds: config.LoginTimeoutSeconds}, nil
}
