package biz

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/password"
	"github.com/sony/sonyflake/v2"
)

var (
	playerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	passwordPattern   = regexp.MustCompile(`^[A-Za-z0-9$%&@#^*!]{6,12}$`)
)

type Registration struct {
	userRepo *repo.UserRepository
	ids      *sonyflake.Sonyflake
}

type RegisteredUser struct {
	ID         int64
	PlayerName string
}

// NewRegistration 组装用户仓储和共享 ID 生成器
func NewRegistration(userRepo *repo.UserRepository, ids *sonyflake.Sonyflake) *Registration {
	return &Registration{userRepo: userRepo, ids: ids}
}

// Register 校验注册信息并创建普通账户，返回不含密码哈希的账户信息
func (r *Registration) Register(ctx context.Context, playerName, plainPassword string) (*RegisteredUser, error) {
	if !playerNamePattern.MatchString(playerName) {
		return nil, ErrPlayerNameInvalid
	}
	if !passwordPattern.MatchString(plainPassword) {
		return nil, ErrPasswordInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hash, err := password.Hash(plainPassword)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRegistrationFailed, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := r.ids.NextID()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRegistrationFailed, err)
	}
	user := model.User{
		ID:           id,
		PlayerName:   playerName,
		PasswordHash: hash,
		IsAdmin:      0,
	}
	if err := r.userRepo.Create(ctx, &user); err != nil {
		if errors.Is(err, repo.ErrPlayerNameConflict) {
			return nil, ErrPlayerNameTaken
		}
		return nil, fmt.Errorf("%w: %w", ErrRegistrationFailed, err)
	}
	return &RegisteredUser{ID: user.ID, PlayerName: user.PlayerName}, nil
}
