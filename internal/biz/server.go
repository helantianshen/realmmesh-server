// 游戏服身份：服务器管理与凭证认证
// 校验管理员权限，协调凭证签发、轮换、停用及删除，并验证当前服务器身份

package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
	"github.com/helantianshen/realmmesh-server/pkg/serversecret"
	"github.com/sony/sonyflake/v2"
)

type ServerBiz struct {
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

// ServerInfo 描述管理员可见的服务器信息，不包含凭证或摘要
type ServerInfo struct {
	ID         int64
	ServerName string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewServerBiz 组装管理员校验、服务器仓储与共享 ID 生成器
func NewServerBiz(userRepo *repo.UserRepository, serverRepo *repo.ServerRepository, ids *sonyflake.Sonyflake) *ServerBiz {
	return &ServerBiz{userRepo: userRepo, serverRepo: serverRepo, ids: ids}
}

// Register 校验管理员及名称后生成凭证，只有数据库写入成功才交付原始密钥
// actorID 必须来自已认证的管理入口，不能直接信任请求正文中的操作者 ID
func (serverBiz *ServerBiz) Register(ctx context.Context, actorID int64, name string) (*IssuedServerSecret, error) {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
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

	id, err := serverBiz.ids.NextID()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServerOperation, err)
	}
	plain, digest := serversecret.Generate()
	record := &model.Server{ID: id, ServerName: name, SecretHash: digest, Enabled: true}
	if err := serverBiz.serverRepo.Create(ctx, record); err != nil {
		return nil, serverOperationError(err)
	}
	return &IssuedServerSecret{ID: id, Secret: plain}, nil
}

// SetEnabled 校验管理员后修改服务器可用状态，不改变当前密钥
func (serverBiz *ServerBiz) SetEnabled(ctx context.Context, actorID, serverID int64, enabled bool) error {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return serverOperationError(serverBiz.serverRepo.SetEnabled(ctx, serverID, enabled))
}

// RotateSecret 校验管理员并替换密钥，写入失败时不向调用者交付新凭证
func (serverBiz *ServerBiz) RotateSecret(ctx context.Context, actorID, serverID int64) (*IssuedServerSecret, error) {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	plain, digest := serversecret.Generate()
	if err := serverBiz.serverRepo.SetSecretHash(ctx, serverID, digest); err != nil {
		return nil, serverOperationError(err)
	}
	return &IssuedServerSecret{ID: serverID, Secret: plain}, nil
}

// Delete 校验管理员后软删除服务器，后续请求无法再查询到可用凭证
func (serverBiz *ServerBiz) Delete(ctx context.Context, actorID, serverID int64) error {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return serverOperationError(serverBiz.serverRepo.Delete(ctx, serverID))
}

// Authenticate 每次读取数据库当前状态并校验密钥，不缓存启用状态或认证结果
// 不存在、停用及错误密钥统一返回凭证无效，数据库故障则保留独立错误类别
func (serverBiz *ServerBiz) Authenticate(ctx context.Context, serverID int64, plain string) (*ServerIdentity, error) {
	if serverID <= 0 || len(plain) != 43 {
		return nil, ErrServerCredentialInvalid
	}
	record, err := serverBiz.serverRepo.FindByID(ctx, serverID)
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
func (serverBiz *ServerBiz) requireAdmin(ctx context.Context, actorID int64) error {
	allowed, err := serverBiz.userRepo.IsAdmin(ctx, actorID)
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

// List 校验当前管理员权限并返回分页结果，不把数据库模型直接交给协议层
func (serverBiz *ServerBiz) List(ctx context.Context, actorID int64, params pagination.Params) (*pagination.Result[ServerInfo], error) {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}
	records, total, err := serverBiz.serverRepo.List(ctx, params)
	if err != nil {
		return nil, serverOperationError(err)
	}

	items := make([]ServerInfo, 0, len(records))
	for _, record := range records {
		items = append(items, serverInfo(&record))
	}
	return &pagination.Result[ServerInfo]{
		Items:    items,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

// Detail 校验管理员后读取指定服务器，停用记录仍可管理，删除记录不可查询
func (serverBiz *ServerBiz) Detail(ctx context.Context, actorID, serverID int64) (*ServerInfo, error) {
	if err := serverBiz.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	record, err := serverBiz.serverRepo.FindPublicByID(ctx, serverID)
	if err != nil {
		return nil, serverOperationError(err)
	}
	info := serverInfo(record)
	return &info, nil
}

// serverInfo 将持久化记录投影为可交付的业务信息，显式排除凭证字段
func serverInfo(record *model.Server) ServerInfo {
	return ServerInfo{
		ID:         record.ID,
		ServerName: record.ServerName,
		Enabled:    record.Enabled,
		CreatedAt:  record.CreatedAt,
		UpdatedAt:  record.UpdatedAt,
	}
}
