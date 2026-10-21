// 应用启动：中心服务组装入口
// 加载配置、初始化 ID 与数据库、执行迁移，并管理 HTTP 和 gRPC 服务生命周期

// Realmmesh 启动 RealmMesh 中心服务，组装持久化、业务对象和协议入口
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"time"

	"github.com/go-kratos/kratos/v3"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/config"
	"github.com/helantianshen/realmmesh-server/internal/data"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/helantianshen/realmmesh-server/internal/server"
	"github.com/helantianshen/realmmesh-server/pkg/idgen"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// main 解析配置路径并在服务运行失败时退出
func main() {
	configPath := flag.String("conf", "configs/config.yaml", "配置文件路径")
	flag.Parse()
	if err := run(*configPath); err != nil {
		log.Fatal(err)
	}
}

// run 初始化 ID 与数据库、执行迁移并运行服务，退出时关闭连接池
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
	if err := data.Migrate(ctx, db, generator); err != nil {
		return err
	}
	cancel()
	log.Print("数据库连接、表结构迁移与系统配置初始化完成")

	// 创建共享令牌管理器，所有 HTTP 登录请求使用同一份签名配置
	lifetime, err := cfg.JWT.Lifetime()
	if err != nil {
		return err
	}
	tokenManager, err := token.New(cfg.JWT.SigningKey, lifetime)
	if err != nil {
		return err
	}

	// 按依赖顺序组装各层，所有入口复用同一组基础设施与业务对象
	repositories := repo.NewRepositories(db)
	businesses := biz.NewBusinesses(repositories, generator)
	handlers := handler.NewHandlers(businesses, tokenManager)
	transports, err := server.NewTransports(cfg.Server, handlers, businesses, tokenManager)
	if err != nil {
		return err
	}

	app := kratos.New(
		kratos.Name("realmmesh-server"),
		kratos.Server(transports.HTTP, transports.GRPC),
	)
	return app.Run()
}
