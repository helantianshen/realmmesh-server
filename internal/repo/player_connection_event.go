// 玩家连接审计仓储：以单条 INSERT 追加审计事实，不查询或锁定玩家账户

package repo

import (
	"context"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/gorm"
)

type PlayerConnectionEventRepository struct{ db *gorm.DB }

// Create 绑定请求期限执行单条写入，失败返回脱敏后的仓储错误
func (r *PlayerConnectionEventRepository) Create(ctx context.Context, event *model.PlayerConnectionEvent) error {
	if err := r.db.WithContext(ctx).Create(event).Error; err != nil {
		return repositoryError("写入玩家连接审计", err)
	}
	return nil
}
