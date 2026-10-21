// 协议处理器组装：集中注入业务对象与 HTTP 令牌管理器

package handler

import (
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// Handlers 保存 HTTP 与 gRPC 使用的协议处理器
type Handlers struct {
	AuditGRPCHandler  *AuditGRPCHandler
	AccountHandler    *AccountHandler
	ConfigGRPCHandler *ConfigGRPCHandler
	AuthHandler       *AuthHandler
	AuthGRPCHandler   *AuthGRPCHandler
	ServerHandler     *ServerHandler
	ProbeHandler      *ProbeGRPCHandler
}

// NewHandlers 复用同一组业务对象，创建账户、服务器管理和探测处理器
func NewHandlers(businesses *biz.Businesses, tokenManager *token.Manager) *Handlers {
	return &Handlers{
		AuditGRPCHandler:  NewAuditGRPCHandler(businesses.PlayerConnectionEventBiz),
		AccountHandler:    NewAccountHandler(businesses.AccountBiz),
		ConfigGRPCHandler: NewConfigGRPCHandler(businesses.SystemConfigBiz),
		AuthHandler:       NewAuthHandler(businesses.RegistrationBiz, businesses.LoginBiz, tokenManager),
		AuthGRPCHandler:   NewAuthGRPCHandler(businesses.RegistrationBiz, businesses.LoginBiz, businesses.AccountEventBiz, businesses.AccountBiz),
		ServerHandler:     NewServerHandler(businesses.ServerBiz),
		ProbeHandler:      NewProbeGRPCHandler(),
	}
}

// NewAuthGRPCHandler 注入共享认证业务和账户通知通道，不依赖 HTTP 令牌管理器
func NewAuthGRPCHandler(
	registrationBiz *biz.RegistrationBiz,
	loginBiz *biz.LoginBiz,
	accountEventBiz *biz.AccountEventBiz,
	accountBiz *biz.AccountBiz,
) *AuthGRPCHandler {
	return &AuthGRPCHandler{
		registrationBiz: registrationBiz,
		loginBiz:        loginBiz,
		accountEventBiz: accountEventBiz,
		accountBiz:      accountBiz,
	}
}

// NewProbeGRPCHandler 创建不持有业务依赖的探测处理器
func NewProbeGRPCHandler() *ProbeGRPCHandler {
	return &ProbeGRPCHandler{}
}

// NewAuthHandler 组装注册、登录与令牌签发依赖，不在请求中重复初始化
func NewAuthHandler(
	registrationBiz *biz.RegistrationBiz,
	loginBiz *biz.LoginBiz,
	tokenManager *token.Manager,
) *AuthHandler {
	return &AuthHandler{
		registrationBiz: registrationBiz,
		loginBiz:        loginBiz,
		tokenManager:    tokenManager,
	}
}

// NewServerHandler 注入共享的服务器业务对象
func NewServerHandler(serverBiz *biz.ServerBiz) *ServerHandler {
	return &ServerHandler{serverBiz: serverBiz}
}

// NewConfigGRPCHandler 注入中心配置业务，复用统一服务器凭据认证
func NewConfigGRPCHandler(systemConfigBiz *biz.SystemConfigBiz) *ConfigGRPCHandler {
	return &ConfigGRPCHandler{systemConfigBiz: systemConfigBiz}
}

// NewAccountHandler 注入账户业务，供管理员删除入口使用
func NewAccountHandler(accountBiz *biz.AccountBiz) *AccountHandler {
	return &AccountHandler{accountBiz: accountBiz}
}

// NewAuditGRPCHandler 注入玩家连接审计业务，复用统一服务器认证
func NewAuditGRPCHandler(playerConnectionEventBiz *biz.PlayerConnectionEventBiz) *AuditGRPCHandler {
	return &AuditGRPCHandler{playerConnectionEventBiz: playerConnectionEventBiz}
}
