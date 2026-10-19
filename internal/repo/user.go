package repo

import (
	"context"
	"errors"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建使用指定数据库连接的用户仓储
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 写入用户，并将名字唯一索引冲突转换为仓储错误
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := r.db.WithContext(ctx).Create(user).Error
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "users_player_name_lower_key" {
		return ErrPlayerNameConflict
	}
	return repositoryError("创建用户", err)
}
