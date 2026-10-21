// 玩家连接审计协议入口：从可信上下文取得服务器身份，验证时间并转换错误

package handler

import (
	"context"
	"time"

	auditv1 "github.com/helantianshen/realmmesh-server/gen/audit/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
)

type AuditGRPCHandler struct {
	auditv1.UnimplementedAuditServiceServer
	playerConnectionEventBiz *biz.PlayerConnectionEventBiz
}

var _ auditv1.AuditServiceServer = (*AuditGRPCHandler)(nil)

// ReportPlayerConnectionEvent 保存单条审计事实，只有写入成功才返回空响应
// RPC /realmmesh.audit.v1.AuditService/ReportPlayerConnectionEvent
func (auditGRPCHandler *AuditGRPCHandler) ReportPlayerConnectionEvent(ctx context.Context, request *auditv1.ReportPlayerConnectionEventRequest) (*auditv1.ReportPlayerConnectionEventResponse, error) {
	receivedAt := time.Now().UTC()
	identity, ok := biz.AuthenticatedServer(ctx)
	if !ok {
		return nil, errRPCAuditUnauthenticated
	}

	// Timestamp 必须显式提供，CheckValid 检查秒数和纳秒范围，不限制时钟偏差
	if request == nil || request.OccurredAt == nil || request.OccurredAt.CheckValid() != nil {
		return nil, errRPCAuditInvalid
	}
	err := auditGRPCHandler.playerConnectionEventBiz.Record(ctx, identity.ID, int32(request.EventType),
		request.PlayerName, request.ConnectionId, request.OccurredAt.AsTime(), receivedAt)
	if err != nil {
		return nil, rpcAuditError(err)
	}
	return &auditv1.ReportPlayerConnectionEventResponse{}, nil
}
