// 传输入口组装：集中注册 HTTP 路由与 gRPC 服务
// 公开接口、账户认证和管理员权限在此绑定到对应处理器

package server

import (
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	authv1 "github.com/helantianshen/realmmesh-server/gen/auth/v1"
	configv1 "github.com/helantianshen/realmmesh-server/gen/config/v1"
	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// Transports 保存交由应用统一启动和停止的传输服务
type Transports struct {
	HTTP *khttp.Server
	GRPC *kgrpc.Server
}

// NewTransports 创建传输服务并绑定全部路由，返回前不启动监听
// 配置错误直接返回，由主入口负责释放已初始化的基础设施
func NewTransports(
	cfg config.ServerConfig,
	handlers *handler.Handlers,
	businesses *biz.Businesses,
	tokenManager *token.Manager,
) (*Transports, error) {
	httpServer := NewHTTPServer(cfg.HTTP.Addr)
	grpcServer, err := NewGRPCServer(cfg.GRPC, businesses.ServerBiz)
	if err != nil {
		return nil, err
	}

	registerHTTPRoutes(httpServer, handlers, businesses.AccountBiz, tokenManager)
	probev1.RegisterProbeServiceServer(grpcServer, handlers.ProbeHandler)
	authv1.RegisterAuthServiceServer(grpcServer, handlers.AuthGRPCHandler)
	configv1.RegisterConfigServiceServer(grpcServer, handlers.ConfigGRPCHandler)
	return &Transports{HTTP: httpServer, GRPC: grpcServer}, nil
}

// registerHTTPRoutes 统一绑定路径、请求方法及当前账户和管理员认证
func registerHTTPRoutes(
	srv *khttp.Server,
	handlers *handler.Handlers,
	accountBiz *biz.AccountBiz,
	tokenManager *token.Manager,
) {
	authHandler := handlers.AuthHandler
	serverHandler := handlers.ServerHandler

	router := srv.Route("/")
	router.GET("/healthz", handler.HealthHandler)

	// 账户接口统一禁止缓存，注册和登录本身不要求已有令牌
	api := srv.Route("/api/v1", noStore)
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	// 当前账户路由只需身份认证；管理员路由还要求当前管理员权限
	api.POST("/auth/me", requireAccount(accountBiz, tokenManager, false, authHandler.Me))
	api.POST("/admin/me", requireAccount(accountBiz, tokenManager, true, authHandler.Me))

	api.POST("/admin/users/delete", requireToken(tokenManager, handlers.AccountHandler.Delete))

	// 服务器管理入口验证 JWT，业务层统一查库检查账户及管理员权限
	api.POST("/admin/servers/create", requireToken(tokenManager, serverHandler.Create))
	api.POST("/admin/servers/list", requireToken(tokenManager, serverHandler.List))
	api.POST("/admin/servers/detail", requireToken(tokenManager, serverHandler.Detail))
	api.POST("/admin/servers/set-enabled", requireToken(tokenManager, serverHandler.SetEnabled))
	api.POST("/admin/servers/rotate-secret", requireToken(tokenManager, serverHandler.RotateSecret))
	api.POST("/admin/servers/delete", requireToken(tokenManager, serverHandler.Delete))
}
