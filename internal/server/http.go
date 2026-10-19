package server

import (
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/service"
)

// NewHTTPServer 创建 HTTP 服务、注册路由
func NewHTTPServer(addr string) *khttp.Server {
	srv := khttp.NewServer(
		khttp.Address(addr),
	)

	router := srv.Route("/")
	router.GET("/healthz", service.HealthHandler)

	return srv
}
