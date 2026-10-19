package service

import (
	"net/http"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// 健康检查接口
type healthResponse struct {
	Status string `json:"status"`
}

func HealthHandler(ctx khttp.Context) error {
	return ctx.JSON(http.StatusOK, healthResponse{
		Status: "ok",
	})
}
