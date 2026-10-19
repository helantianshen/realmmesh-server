package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/go-kratos/kratos/v3"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/data"
	"github.com/helantianshen/realmmesh-server/internal/idgen"
	"github.com/helantianshen/realmmesh-server/internal/server"
	"github.com/helantianshen/realmmesh-server/internal/service"
)

func main() {
	configPath := flag.String("conf", "configs/config.yaml", "配置文件路径")
	flag.Parse()
	if err := run(*configPath); err != nil {
		log.Fatal(err)
	}
}

func run(configPath string) (err error) {
	// 加载配置文件
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// 初始化雪花 id 生成器
	generator, err := idgen.New(cfg.ID)
	if err != nil {
		return err
	}
	// 测试雪花 id 生成器
	if _, err := generator.NextID(); err != nil {
		return fmt.Errorf("检查 ID 生成器: %w", err)
	}
	log.Print("ID 生成器初始化完成")

	// 初始化数据库
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := data.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() {
		closeErr := pool.Close()
		err = errors.Join(err, closeErr)
	}()
	// 执行数据库迁移
	if err := data.Migrate(ctx, db); err != nil {
		return err
	}
	cancel()
	log.Print("数据库连接与用户表迁移完成")

	// 初始化 HTTP 和 gRPC 服务端
	probeHandler := &service.ProbeGRPCHandler{}
	httpServer := server.NewHTTPServer(cfg.Server.HTTP.Addr)
	grpcServer := server.NewGRPCServer(cfg.Server.GRPC.Addr, probeHandler)
	app := kratos.New(
		kratos.Name("realmmesh-server"),
		kratos.Server(httpServer, grpcServer),
	)
	return app.Run()
}
