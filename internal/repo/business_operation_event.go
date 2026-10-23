// 中心业务审计仓储：在业务事务之外追加一条操作记录

package repo

import (
	"context"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/gorm"
)

type BusinessOperationEventRepository struct{ db *gorm.DB }

// Create 按调用上下文的期限写入，返回不包含 SQL 参数的错误摘要
func (r *BusinessOperationEventRepository) Create(ctx context.Context, event *model.BusinessOperationEvent) error {
	if err := r.db.WithContext(ctx).Create(event).Error; err != nil {
		return repositoryError("写入中心业务审计", err)
	}
	return nil
}
