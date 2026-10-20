// 传输组装验证：检查统一入口的公开路由、认证边界和 gRPC 注册
// 使用内存请求和连接，不连接真实数据库或占用服务监听端口

package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	probev1 "github.com/helantianshen/realmmesh-server/gen/probe/v1"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/pkg/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// TestTransportAssembly 验证逐层组装后的路由可达性及未认证请求的拒绝边界
// 仓储不连接数据库，错误请求必须在访问仓储前被拒绝
func TestTransportAssembly(t *testing.T) {
	tokenManager, err := token.New(strings.Repeat("t", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repositories := repo.NewRepositories(nil)
	businesses := biz.NewBusinesses(repositories, nil)
	handlers := handler.NewHandlers(businesses, tokenManager)
	cfg := config.ServerConfig{
		HTTP: config.HTTPConfig{Addr: "127.0.0.1:8000"},
		GRPC: config.GRPCConfig{Addr: "127.0.0.1:9000"},
	}
	transports, err := NewTransports(cfg, handlers, businesses, tokenManager)
	if err != nil {
		t.Fatal(err)
	}

	// 真实 HTTP 路由处理内存请求，区分公开接口与受保护接口
	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodPost, "/api/v1/auth/register", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/auth/login", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/auth/me", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/me", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/create", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/list", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/detail", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/set-enabled", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/rotate-secret", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/admin/servers/delete", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/auth/login", http.StatusNotFound},
		{http.MethodGet, "/api/v1/admin/servers/list", http.StatusNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			// 使用不完整 JSON，公开认证接口应被解码器拒绝，管理接口应先拒绝身份
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader("{"))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			transports.HTTP.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d", response.Code, testCase.want)
			}
			if testCase.method == http.MethodPost && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("API 响应缺少禁止缓存标记")
			}
		})
	}

	// 通过完整 gRPC 调用链验证 Echo 已注册，且缺少凭据不能进入处理器
	listener := bufconn.Listen(1024 * 1024)
	defer listener.Close()
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- transports.GRPC.Serve(listener)
	}()
	defer func() {
		transports.GRPC.Server.Stop()
		if err := <-serveResult; err != nil {
			t.Errorf("gRPC 停止失败: %v", err)
		}
	}()

	connection, err := grpc.NewClient("passthrough:///assembly-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = probev1.NewProbeServiceClient(connection).Echo(ctx, &probev1.EchoRequest{Message: "assembly"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Echo status = %s, want Unauthenticated", status.Code(err))
	}

	// 非法监听配置不得返回可用的组装结果
	cfg.GRPC.Addr = "invalid"
	if result, err := NewTransports(cfg, handlers, businesses, tokenManager); err == nil || result != nil {
		t.Fatal("非法 gRPC 配置未阻止组装")
	}
}
