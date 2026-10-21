// 玩家连接审计业务：验证连接事实并持久化，不改变认证状态或维护在线会话

package biz

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/helantianshen/realmmesh-server/internal/model"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
)

const (
	PlayerJoined int32 = 1 // 玩家进入游戏服，不表示账户认证成功
	PlayerLeft   int32 = 2 // 玩家离开游戏服，不推断其他服务器的连接状态
)

type PlayerConnectionEventBiz struct {
	playerConnectionEventRepo *repo.PlayerConnectionEventRepository
	ids                       *sonyflake.Sonyflake
}

// Record 校验事件与关联 ID，再生成中心记录 ID 并追加一条记录
// 允许尚未注册的玩家与乱序记录，不检查账户、匹配事件或游戏服时钟偏差
func (playerConnectionEventBiz *PlayerConnectionEventBiz) Record(
	ctx context.Context,
	serverID int64,
	eventType int32,
	playerName, connectionID string,
	occurredAt, receivedAt time.Time,
) error {
	if !playerNamePattern.MatchString(playerName) {
		return ErrPlayerNameInvalid
	}
	if eventType != PlayerJoined && eventType != PlayerLeft {
		return ErrPlayerConnectionEventInvalid
	}

	// Parse 读取 UUID；再次比较标准字符串，拒绝非标准形式和全零关联 ID
	id, err := uuid.Parse(connectionID)
	if err != nil || id == uuid.Nil || id.String() != connectionID {
		return ErrPlayerConnectionEventInvalid
	}

	recordID, err := playerConnectionEventBiz.ids.NextID()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPlayerConnectionEventWrite, err)
	}
	event := model.PlayerConnectionEvent{
		ID:           recordID,
		ServerID:     serverID,
		EventType:    eventType,
		PlayerName:   playerName,
		ConnectionID: connectionID,
		OccurredAt:   occurredAt.UTC(),
		ReceivedAt:   receivedAt.UTC(),
	}
	if err := playerConnectionEventBiz.playerConnectionEventRepo.Create(ctx, &event); err != nil {
		return fmt.Errorf("%w: %w", ErrPlayerConnectionEventWrite, err)
	}
	return nil
}
