// 运行健康：HTTP 存活检查
// 返回进程存活状态，不将数据库或其他依赖状态纳入此检查

package handler

import (
	"net/http"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// 健康检查接口
type healthResponse struct {
	Status string `json:"status"`
}

// HealthHandler 返回服务存活状态，不检查数据库或其他依赖
func HealthHandler(ctx khttp.Context) error {
	return ctx.JSON(http.StatusOK, healthResponse{
		Status: "ok",
	})
}
