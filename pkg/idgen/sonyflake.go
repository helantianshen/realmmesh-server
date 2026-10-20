// 标识生成基础设施：Sonyflake 实例构造
// 按中心节点编号和固定时间参数创建共享 ID 生成器

// Package idgen 按中心节点配置创建 Sonyflake ID 生成器
package idgen

import (
	"fmt"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/sony/sonyflake/v2"
)

// New 创建可并发共享的雪花 id 生成器，同一节点编号只能由一个运行实例使用
func New(cfg config.IDConfig) (*sonyflake.Sonyflake, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// 时间起点和位布局属于持久化 ID 的编码约定，不能随部署变更
	settings := sonyflake.Settings{
		BitsSequence:  12,
		BitsMachineID: 10,
		TimeUnit:      time.Millisecond,
		StartTime:     time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		MachineID: func() (int, error) {
			return cfg.NodeID, nil
		},
	}
	generator, err := sonyflake.New(settings)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInitialization, err)
	}
	return generator, nil
}
