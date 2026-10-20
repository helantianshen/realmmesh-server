package repo

import (
	"context"
	"errors"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"gorm.io/gorm"
)

type ServerRepository struct{ db *gorm.DB }

// NewServerRepository 创建负责服务器记录的仓储
func NewServerRepository(db *gorm.DB) *ServerRepository { return &ServerRepository{db: db} }

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
