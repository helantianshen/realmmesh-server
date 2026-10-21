// 账户：用户仓储
// 封装账户创建与查询、实时权限读取，以及带行锁的登录状态事务

// Package repo 封装账户、服务器和系统配置的数据访问及事务边界
package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserRepository struct {
	db *gorm.DB
}

// FindByID 查询当前未删除账户的公开信息，不读取密码哈希
func (r *UserRepository) FindByID(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Select("id", "player_name", "is_admin").
		First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, repositoryError("查询账户", err)
	}
	return &user, nil
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

// UpdateLoginState 锁定用户后执行业务判断，并在同一事务中提交登录状态
//
// apply 返回是否需要保存状态及处理错误。密码错误和休息期属于正常结果，
// apply 应返回 nil 错误，以免已增加的失败次数随事务回滚
// 这里只允许保存登录计数和截止时间，不写回密码、权限或其他账户字段
func (r *UserRepository) UpdateLoginState(
	ctx context.Context,
	playerName string,
	apply func(user *model.User, now time.Time) (bool, error),
) error {
	var businessError error

	// Transaction 在回调成功后提交；任意数据库错误或业务异常都会回滚
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User

		// FOR UPDATE 将同一账户的登录请求串行化，普通查询自动排除软删除账户
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("lower(player_name) = ?", strings.ToLower(playerName)).
			First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}

		// 获取锁后读取数据库实时时钟，避免等待行锁导致事务开始时间过期
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}

		changed, err := apply(&user, now)
		if err != nil {
			businessError = err
			return err
		}
		if !changed {
			return nil
		}

		// 使用 map 明确写入零值和 NULL，确保成功登录能清零并解除已到期状态
		return tx.Model(&user).Updates(map[string]any{
			"login_failures": user.LoginFailures,
			"cooldown_until": user.CooldownUntil,
		}).Error
	})

	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUserNotFound) {
		return ErrUserNotFound
	}
	if businessError != nil {
		return businessError
	}
	return repositoryError("更新登录状态", err)
}
