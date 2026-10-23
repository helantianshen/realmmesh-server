// 中心业务审计：分类操作结果并尽力持久化，不改变原业务的返回结果

package biz

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
)

// BusinessOperation 是中心可审计的固定操作，不接受客户端自由指定
type BusinessOperation string

const (
	OperationRegister       BusinessOperation = "account.register"        // 创建普通账户
	OperationLogin          BusinessOperation = "account.login"           // 验证登录凭据并提交认证状态
	OperationChangePassword BusinessOperation = "account.change_password" // 自助修改密码
	OperationDeleteAccount  BusinessOperation = "account.delete"          // 管理员删除普通账户
	OperationCreateServer   BusinessOperation = "server.create"           // 创建服务器并签发凭据
	OperationEnableServer   BusinessOperation = "server.enable"           // 启用服务器
	OperationDisableServer  BusinessOperation = "server.disable"          // 停用服务器
	OperationRotateSecret   BusinessOperation = "server.rotate_secret"    // 轮换服务器密钥
	OperationDeleteServer   BusinessOperation = "server.delete"           // 删除服务器
)

const businessAuditTimeout = time.Second // 单次审计写入最多等待一秒，同时遵守请求取消与截止时间

type BusinessOperationBiz struct {
	businessOperationEventRepo *repo.BusinessOperationEventRepository
	ids                        *sonyflake.Sonyflake
}

// OperationEvent 由业务入口显式组织安全字段，零 ID 表示尚未确认对象身份
type OperationEvent struct {
	Operation     BusinessOperation
	ActorUserID   int64
	TargetType    string
	TargetID      int64
	PlayerName    string
	CooldownUntil *time.Time
	Reason        string
}

// OperationRequest 保存传输层提供的来源与日志关联标识，不承载用户身份声明
type OperationRequest struct {
	Source    string
	RequestID string
}

type operationRequestKey struct{}

// WithOperationRequest 传递入口确定的关联信息，不让业务层依赖 HTTP 或 gRPC 类型
func WithOperationRequest(ctx context.Context, request OperationRequest) context.Context {
	return context.WithValue(ctx, operationRequestKey{}, request)
}

// Record 在业务结果确定之后、业务事务之外尝试一次审计写入
// 写入失败仅输出安全分类，不返回错误，不重试，不脱离请求生命周期启动后台任务
func (businessOperationBiz *BusinessOperationBiz) Record(ctx context.Context, event OperationEvent, businessErr error) {
	request, ok := ctx.Value(operationRequestKey{}).(OperationRequest)
	if !ok {
		request.Source = "internal"
	}
	result, reason := operationOutcome(event.Reason, businessErr)
	record := &model.BusinessOperationEvent{
		Operation:     string(event.Operation),
		Result:        result,
		Reason:        reason,
		Source:        request.Source,
		RequestID:     request.RequestID,
		ActorUserID:   optionalOperationID(event.ActorUserID),
		TargetType:    event.TargetType,
		TargetID:      optionalOperationID(event.TargetID),
		CooldownUntil: event.CooldownUntil,
		OccurredAt:    time.Now().UTC(),
	}

	// 失败认证的名字仅用于追踪尝试对象，不作为已验证的操作者身份
	if playerNamePattern.MatchString(event.PlayerName) {
		record.PlayerName = event.PlayerName
	}
	if identity, ok := AuthenticatedServer(ctx); ok {
		record.ServerID = optionalOperationID(identity.ID)
	}

	id, err := businessOperationBiz.ids.NextID()
	if err != nil {
		businessOperationBiz.logFailure(ctx, record, "id_generation_failed")
		return
	}
	record.ID = id

	// WithTimeout 继承请求取消信号，并将审计额外等待限制在一秒以内
	auditCtx, cancel := context.WithTimeout(ctx, businessAuditTimeout)
	defer cancel()
	if err := businessOperationBiz.businessOperationEventRepo.Create(auditCtx, record); err != nil {
		category := "write_failed"
		if errors.Is(err, context.Canceled) {
			category = "canceled"
		} else if errors.Is(err, context.DeadlineExceeded) {
			category = "deadline_exceeded"
		}
		businessOperationBiz.logFailure(ctx, record, category)
	}
}

// RecordAuthentication 将正常返回的认证拒绝和异常分开，不把 nil 错误误判为验证成功
// 改密使用同一分类入口，但由调用者指定改密操作，内部密码验证不重复写登录审计
func (businessOperationBiz *BusinessOperationBiz) RecordAuthentication(
	ctx context.Context,
	operation BusinessOperation,
	playerName string,
	result *LoginResult,
	err error,
) {
	event := OperationEvent{Operation: operation, TargetType: "account", PlayerName: playerName}
	if result != nil {
		event.TargetID = result.UserID
		event.CooldownUntil = result.CooldownUntil
		if result.PlayerName != "" {
			event.PlayerName = result.PlayerName
		}
		event.Reason = string(result.Status)
		if result.EnteredCooldown {
			event.Reason = "cooldown_started"
		}
		if err == nil && result.Status == LoginSucceeded {
			event.ActorUserID = result.UserID
		}
	}
	businessOperationBiz.Record(ctx, event, err)
}

// operationOutcome 只输出固定分类，保留包装错误中的业务原因，不持久化异常正文
func operationOutcome(reason string, err error) (string, string) {
	if err == nil {
		if reason == "" || reason == string(LoginSucceeded) {
			return "succeeded", "succeeded"
		}
		return "rejected", reason
	}

	switch {
	case errors.Is(err, ErrPlayerNameInvalid):
		return "rejected", "invalid_player_name"
	case errors.Is(err, ErrPasswordInvalid):
		return "rejected", "invalid_password_format"
	case errors.Is(err, ErrPlayerNameTaken):
		return "rejected", "player_name_taken"
	case errors.Is(err, ErrAuthenticationConflict):
		return "rejected", "authentication_conflict"
	case errors.Is(err, ErrAccountNotFound):
		return "rejected", "account_not_found"
	case errors.Is(err, ErrAccountUnavailable):
		return "rejected", "account_unavailable"
	case errors.Is(err, ErrAdminRequired):
		return "rejected", "admin_required"
	case errors.Is(err, ErrAdminDeletionForbidden):
		return "rejected", "admin_deletion_forbidden"
	case errors.Is(err, ErrServerNameInvalid):
		return "rejected", "invalid_server_name"
	case errors.Is(err, ErrServerNotFound):
		return "rejected", "server_not_found"
	case errors.Is(err, context.Canceled):
		return "failed", "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "failed", "deadline_exceeded"
	default:
		return "failed", "internal_error"
	}
}

// optionalOperationID 将未知或非法 ID 留空，避免用零值伪造关联对象
func optionalOperationID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// logFailure 只记录操作、关联标识和固定分类，不输出业务参数或底层异常正文
func (businessOperationBiz *BusinessOperationBiz) logFailure(ctx context.Context, record *model.BusinessOperationEvent, category string) {
	slog.WarnContext(ctx, "业务审计未写入", "operation", record.Operation,
		"request_id", record.RequestID, "reason", category)
}
