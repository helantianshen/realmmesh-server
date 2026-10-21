// 账户通知：向当前中心实例的在线订阅者分发已提交的账户变更
// 仅保存有限的待发送消息，不持久化、不补发，也不维护玩家会话

package biz

import (
	"log/slog"
	"sync"
)

const accountEventBufferSize = 32 // 每条在线订阅最多缓存 32 条通知，满时丢弃该订阅的新通知

// AccountEventReason 表示已经提交成功的账户变更类型
type AccountEventReason uint8

const (
	AccountPasswordChanged AccountEventReason = iota + 1 // 账户密码已变更
	AccountDeleted                                       // 账户已逻辑删除
)

// AccountEvent 保存广播所需的账户名字和原因，不携带任何认证秘密
type AccountEvent struct {
	PlayerName string
	Reason     AccountEventReason
}

// AccountEventBiz 管理本进程订阅，互斥锁只保护内存集合和非阻塞入队
type AccountEventBiz struct {
	mu          sync.Mutex
	subscribers map[chan AccountEvent]struct{}
	closed      bool
}

// Subscribe 登记独立订阅并返回只读消息通道和取消函数
// 调用方负责在流结束时取消订阅；重复取消安全，停服后不接受新订阅
func (accountEventBiz *AccountEventBiz) Subscribe() (<-chan AccountEvent, func(), error) {
	accountEventBiz.mu.Lock()
	defer accountEventBiz.mu.Unlock()
	if accountEventBiz.closed {
		return nil, nil, ErrAccountEventsClosed
	}

	events := make(chan AccountEvent, accountEventBufferSize)
	accountEventBiz.subscribers[events] = struct{}{}
	return events, func() { accountEventBiz.unsubscribe(events) }, nil
}

// unsubscribe 移除订阅并关闭通道，与广播共用锁以避免向已关闭通道发送
func (accountEventBiz *AccountEventBiz) unsubscribe(events chan AccountEvent) {
	accountEventBiz.mu.Lock()
	defer accountEventBiz.mu.Unlock()
	if _, exists := accountEventBiz.subscribers[events]; exists {
		delete(accountEventBiz.subscribers, events)
		close(events)
	}
}

// Publish 将已提交的账户变更尽力送入各订阅的缓冲，不等待游戏服接收
// 必须在账户事务提交成功后调用；返回值不表示远端已处理，通知失败不能回滚账户变更
func (accountEventBiz *AccountEventBiz) Publish(event AccountEvent) error {
	if !playerNamePattern.MatchString(event.PlayerName) {
		return ErrPlayerNameInvalid
	}
	if event.Reason != AccountPasswordChanged && event.Reason != AccountDeleted {
		return ErrAccountEventInvalid
	}

	// 所有订阅独立入队，慢订阅者只丢失自己的通知，不阻塞其余服务器
	accountEventBiz.mu.Lock()
	if accountEventBiz.closed {
		accountEventBiz.mu.Unlock()
		return ErrAccountEventsClosed
	}
	dropped := 0
	for events := range accountEventBiz.subscribers {
		select {
		case events <- event:
		default:
			dropped++
		}
	}
	accountEventBiz.mu.Unlock()

	// 日志在锁外写入，只记录丢弃数量，不输出账户或认证请求内容
	if dropped > 0 {
		slog.Warn("账户通知订阅缓冲已满，本次部分通知已丢弃", "dropped_subscriptions", dropped)
	}
	return nil
}

// Close 停止接受订阅和广播，关闭现有消息通道，重复调用不产生副作用
func (accountEventBiz *AccountEventBiz) Close() {
	accountEventBiz.mu.Lock()
	defer accountEventBiz.mu.Unlock()
	accountEventBiz.closed = true
	for events := range accountEventBiz.subscribers {
		delete(accountEventBiz.subscribers, events)
		close(events)
	}
}
