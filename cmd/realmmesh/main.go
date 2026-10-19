package main

import (
	"flag"
	"log"

	"github.com/go-kratos/kratos/v3"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/server"
	"github.com/helantianshen/realmmesh-server/internal/service"
)

func main() {
	// 指定配置文件
	configPath := flag.String("conf", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// 创建服务
	probeHandler := &service.ProbeGRPCHandler{}

	httpServer := server.NewHTTPServer(cfg.Server.HTTP.Addr)
	grpcServer := server.NewGRPCServer(cfg.Server.GRPC.Addr, probeHandler)

	// 创建应用
	app := kratos.New(
		kratos.Name("realmmesh-server"),
		kratos.Server(httpServer, grpcServer),
	)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
