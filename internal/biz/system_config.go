// 系统配置：组织下发给游戏服的中心统一配置

package biz

import (
	"context"
	"fmt"

	"github.com/helantianshen/realmmesh-server/internal/repo"
)

type SystemConfigBiz struct {
	systemConfigRepo *repo.SystemConfigRepository
}

// GameServerConfig 只包含游戏服需要执行的配置，不携带中心连接信息或凭据
type GameServerConfig struct {
	LoginTimeoutSeconds int32
}

// GetConfig 读取当前有效配置，读取失败时保留错误类别，不生成默认响应
func (systemConfigBiz *SystemConfigBiz) GetConfig(ctx context.Context) (*GameServerConfig, error) {
	seconds, err := systemConfigBiz.systemConfigRepo.LoginTimeoutSeconds(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSystemConfigRead, err)
	}

	return &GameServerConfig{LoginTimeoutSeconds: seconds}, nil
}
