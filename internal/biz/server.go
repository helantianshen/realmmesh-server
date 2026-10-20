package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/serversecret"
	"github.com/sony/sonyflake/v2"
)

type Server struct {
	userRepo   *repo.UserRepository
	serverRepo *repo.ServerRepository
	ids        *sonyflake.Sonyflake
}

// ServerIdentity 是完成认证后传递给业务处理器的身份，不包含凭证
type ServerIdentity struct {
	ID   int64
	Name string
}

// IssuedServerSecret 只用于创建或轮换成功后的凭证交付，不用于普通查询
type IssuedServerSecret struct {
	ID     int64
	Secret string
}

// NewServer 组装管理员校验、服务器仓储与共享 ID 生成器
func NewServer(userRepo *repo.UserRepository, serverRepo *repo.ServerRepository, ids *sonyflake.Sonyflake) *Server {
	return &Server{userRepo: userRepo, serverRepo: serverRepo, ids: ids}
}

// Register 校验管理员及名称后生成凭证，只有数据库写入成功才交付原始密钥
// actorID 必须来自已认证的管理入口，不能直接信任请求正文中的操作者 ID
func (s *Server) Register(ctx context.Context, actorID int64, name string) (*IssuedServerSecret, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}

	// 按 Unicode 字符计数，允许中文展示名；不自动修改调用者输入
	length := utf8.RuneCountInString(name)
	if !utf8.ValidString(name) || length < 1 || length > 64 || strings.TrimSpace(name) != name {
		return nil, ErrServerNameInvalid
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return nil, ErrServerNameInvalid
		}
	}

	id, err := s.ids.NextID()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServerOperation, err)
	}
	plain, digest := serversecret.Generate()
	record := &model.Server{ID: id, ServerName: name, SecretHash: digest, Enabled: true}
	if err := s.serverRepo.Create(ctx, record); err != nil {
		return nil, serverOperationError(err)
	}
	return &IssuedServerSecret{ID: id, Secret: plain}, nil
}

// SetEnabled 校验管理员后修改服务器可用状态，不改变当前密钥
func (s *Server) SetEnabled(ctx context.Context, actorID, serverID int64, enabled bool) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return serverOperationError(s.serverRepo.SetEnabled(ctx, serverID, enabled))
}

// RotateSecret 校验管理员并替换密钥，写入失败时不向调用者交付新凭证
func (s *Server) RotateSecret(ctx context.Context, actorID, serverID int64) (*IssuedServerSecret, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	plain, digest := serversecret.Generate()
	if err := s.serverRepo.SetSecretHash(ctx, serverID, digest); err != nil {
		return nil, serverOperationError(err)
	}
	return &IssuedServerSecret{ID: serverID, Secret: plain}, nil
}

// Delete 校验管理员后软删除服务器，后续请求无法再查询到可用凭证
func (s *Server) Delete(ctx context.Context, actorID, serverID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return serverOperationError(s.serverRepo.Delete(ctx, serverID))
}

// Authenticate 每次读取数据库当前状态并校验密钥，不缓存启用状态或认证结果
// 不存在、停用及错误密钥统一返回凭证无效，数据库故障则保留独立错误类别
func (s *Server) Authenticate(ctx context.Context, serverID int64, plain string) (*ServerIdentity, error) {
	if serverID <= 0 || len(plain) != 43 {
		return nil, ErrServerCredentialInvalid
	}
	record, err := s.serverRepo.FindByID(ctx, serverID)
	if errors.Is(err, repo.ErrServerNotFound) {
		return nil, ErrServerCredentialInvalid
	}
	if err != nil {
		return nil, serverOperationError(err)
	}
	if !record.Enabled || !serversecret.Verify(plain, record.SecretHash) {
		return nil, ErrServerCredentialInvalid
	}
	return &ServerIdentity{ID: record.ID, Name: record.ServerName}, nil
}

// requireAdmin 从数据库读取当前权限，已删除或已降权账户不能继续管理服务器
func (s *Server) requireAdmin(ctx context.Context, actorID int64) error {
	allowed, err := s.userRepo.IsAdmin(ctx, actorID)
	if err != nil {
		return serverOperationError(err)
	}
	if !allowed {
		return ErrAdminRequired
	}
	return nil
}

// serverOperationError 将仓储结果转换为业务错误，并保留取消及数据库异常的错误链
func serverOperationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repo.ErrServerNotFound) {
		return ErrServerNotFound
	}
	return fmt.Errorf("%w: %w", ErrServerOperation, err)
}
