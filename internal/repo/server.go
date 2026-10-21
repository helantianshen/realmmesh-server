// 游戏服身份：服务器仓储
// 封装服务器记录及凭证摘要的查询、更新和软删除，不执行协议认证

package repo

import (
	"context"
	"errors"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
	"gorm.io/gorm"
)

type ServerRepository struct{ db *gorm.DB }

// Create 写入服务器身份、启用状态和密钥摘要
func (r *ServerRepository) Create(ctx context.Context, server *model.Server) error {
	if err := r.db.WithContext(ctx).Create(server).Error; err != nil {
		return repositoryError("登记服务器", err)
	}
	return nil
}

// FindByID 按主键读取当前状态，GORM 自动排除已软删除记录
func (r *ServerRepository) FindByID(ctx context.Context, id int64) (*model.Server, error) {
	var server model.Server
	err := r.db.WithContext(ctx).First(&server, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, repositoryError("查询服务器", err)
	}
	return &server, nil
}

// SetEnabled 明确写入布尔状态，停用时 false 也必须落库
func (r *ServerRepository) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	result := r.db.WithContext(ctx).Model(&model.Server{}).Where("id = ?", id).Update("enabled", enabled)
	return serverWriteResult(result)
}

// SetSecretHash 原子替换密钥摘要，提交后旧密钥无法通过后续认证
func (r *ServerRepository) SetSecretHash(ctx context.Context, id int64, digest string) error {
	result := r.db.WithContext(ctx).Model(&model.Server{}).Where("id = ?", id).Update("secret_hash", digest)
	return serverWriteResult(result)
}

// Delete 通过 GORM 写入删除时间，保留服务器历史记录
func (r *ServerRepository) Delete(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&model.Server{}, "id = ?", id)
	return serverWriteResult(result)
}

// serverWriteResult 区分数据库失败与没有可修改的未删除记录
func serverWriteResult(result *gorm.DB) error {
	if result.Error != nil {
		return repositoryError("更新服务器", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrServerNotFound
	}
	return nil
}

// List 分别查询总数和当前页，并发增删时二者可能短暂不一致
// 分页参数由业务层校验，查询只读取公开字段并自动排除软删除记录
func (r *ServerRepository) List(ctx context.Context, params pagination.Params) ([]model.Server, int64, error) {
	records := make([]model.Server, 0)
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.Server{}).Count(&total).Error; err != nil {
		return nil, 0, repositoryError("统计服务器", err)
	}

	// 固定 ID 倒序，Offset 和 Limit 使用业务层已校验的分页值
	err := r.db.WithContext(ctx).Select("id", "server_name", "enabled", "created_at", "updated_at").
		Order("id DESC").Offset(params.Offset()).Limit(params.PageSize).Find(&records).Error
	if err != nil {
		return nil, 0, repositoryError("分页查询服务器", err)
	}
	return records, total, nil
}

// FindPublicByID 查询服务器管理信息，不读取用于认证的密钥摘要
func (r *ServerRepository) FindPublicByID(ctx context.Context, id int64) (*model.Server, error) {
	var record model.Server
	err := r.db.WithContext(ctx).
		Select("id", "server_name", "enabled", "created_at", "updated_at").
		First(&record, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, repositoryError("查询服务器详情", err)
	}
	return &record, nil
}
