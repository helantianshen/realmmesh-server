// HTTP 传输基础设施：服务构建与路由组装
// 注册健康、账户和服务器管理接口，配置 JSON 请求解析、请求超时与响应缓存策略

// Package server 构建 HTTP 和 gRPC 服务，组装路由、认证边界与请求日志
package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

const (
	httpRequestTimeout = 10 * time.Second // HTTP 请求上下文超时，限制业务处理的等待时间
	maxJSONBodyBytes   = 4096             // JSON 请求正文的最大字节数
)

// NewHTTPServer 创建 HTTP 服务，将公开接口与需要账户认证的接口分别注册
// 账户接口统一使用 POST，只有健康检查使用 GET
func NewHTTPServer(
	addr string,
	authHandler *handler.AuthHandler,
	serverHandler *handler.ServerHandler,
	accountBiz *biz.AccountBiz,
	tokenManager *token.Manager,
) *khttp.Server {
	srv := khttp.NewServer(
		khttp.Address(addr),
		khttp.Timeout(httpRequestTimeout),
		khttp.RequestDecoder(decodeJSONRequest),
	)

	router := srv.Route("/")
	router.GET("/healthz", handler.HealthHandler)

	// 账户接口统一禁止缓存，注册和登录本身不要求已有令牌
	api := srv.Route("/api/v1", noStore)
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	// 当前账户路由只需身份认证；管理员路由还要求当前管理员权限
	api.POST("/auth/me", requireAccount(accountBiz, tokenManager, false, authHandler.Me))
	api.POST("/admin/me", requireAccount(accountBiz, tokenManager, true, authHandler.Me))

	// 所有服务器管理操作都要求当前管理员权限，包括列表和详情查询
	api.POST("/admin/servers/create", requireAccount(accountBiz, tokenManager, true, serverHandler.Create))
	api.POST("/admin/servers/list", requireAccount(accountBiz, tokenManager, true, serverHandler.List))
	api.POST("/admin/servers/detail", requireAccount(accountBiz, tokenManager, true, serverHandler.Detail))
	api.POST("/admin/servers/set-enabled", requireAccount(accountBiz, tokenManager, true, serverHandler.SetEnabled))
	api.POST("/admin/servers/rotate-secret", requireAccount(accountBiz, tokenManager, true, serverHandler.RotateSecret))
	api.POST("/admin/servers/delete", requireAccount(accountBiz, tokenManager, true, serverHandler.Delete))

	return srv
}

// decodeJSONRequest 使用标准库解析单个 JSON 对象，限制请求体以防无界内存占用
// 不接受表单或未知字段，避免客户端误以为注册时可指定管理员权限
func decodeJSONRequest(request *http.Request, target any) error {
	// 1. ParseMediaType 分离媒体类型和 charset 等参数，只允许 JSON 正文
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return handler.ErrInvalidRequest
	}

	// 2. MaxBytesReader 在读取超限时返回错误，不会把完整超长正文载入内存
	request.Body = http.MaxBytesReader(nil, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)

	// 3. DisallowUnknownFields 拒绝请求结构体未声明的字段，Decode 完成字段类型检查
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return handler.ErrInvalidRequest
	}

	// 4. 再次解码必须到达 EOF，确保正文只有一个 JSON 值，可带尾部空白
	var trailingValue any
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		return handler.ErrInvalidRequest
	}

	return nil
}

// noStore 禁止缓存包含令牌或账户信息的成功和失败响应
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(writer, request)
	})
}
