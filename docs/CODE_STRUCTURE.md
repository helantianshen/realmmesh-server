# 中心服务代码结构

本项目使用 Go、Kratos 与手动依赖组装。本文说明当前目录职责、启动顺序和请求处理路径，供源码阅读与维护参考。

## 分层与入口

| 位置 | 当前职责 |
| --- | --- |
| cmd/realmmesh/main.go | 加载配置，初始化 ID、数据库与 JWT，执行迁移，调用各层统一入口，管理服务和连接池生命周期 |
| internal/config | 统一配置结构、逐层 Validate 与配置文件加载 |
| internal/data | PostgreSQL 连接、表迁移、系统配置初始化 |
| internal/model | GORM 表结构和显式表名 |
| internal/repo | 按实体组织查询和写入；repo.go 集中构造并返回 Repositories |
| internal/biz | 账户和服务器业务规则；biz.go 集中构造并返回 Businesses |
| internal/handler | HTTP/gRPC 请求响应适配；handler.go 集中构造并返回 Handlers |
| internal/server | HTTP/gRPC 传输配置、认证及日志；route.go 统一绑定路由并返回 Transports |
| pkg | password、idgen、token、serversecret、pagination 工具 |
| gen、proto | 消费者生成代码和固定版本协议 Submodule，不手改生成代码 |

当前没有独立 service 层。业务对象不反向依赖 Handler 或传输服务；repo 不依赖 biz。server 中的认证中间件调用业务层验证身份，属于协议访问控制边界。

## 启动与请求

主入口完成基础设施初始化后依次调用：

```text
repo.NewRepositories(db)
    → biz.NewBusinesses(repositories, generator)
    → handler.NewHandlers(businesses, tokenManager)
    → server.NewTransports(cfg.Server, handlers, businesses, tokenManager)
    → kratos.New(...).Run()
```

各层复用相同的仓储、业务对象、ID 生成器和令牌管理器。各具体构造函数集中在所在层的组装文件，实体和业务文件保留类型与执行方法。

普通请求从 server 的路由与认证入口进入 handler，再调用 biz 和 repo。model 描述数据映射，data 负责启动基础设施，不作为请求处理的额外业务层。

HTTP 方法、路径、路由组及权限绑定集中在 server/route.go；http.go 保留超时、JSON 解码和缓存策略，grpc.go 保留监听配置及认证、日志拦截器。处理器注释标明完整路由，请求响应类型靠近首个使用的处理方法，共用类型只定义一次。

增加现有能力时修改对应业务文件及集中组装入口，不在 main.go 逐项构造。玩家认证由 handler/auth_grpc.go 复用注册和登录业务，经 handler.go 构造并由 server/route.go 注册。

游戏服配置读取经 `handler/config_grpc.go` → `biz/system_config.go` → `repo/system_config.go`，逐次服务器认证复用统一 gRPC 拦截器。默认登录时限及启动校验在 `data/system_config.go` 完成，配置键在 `model/system_config.go` 定义。

账户变更订阅由 `handler/auth_grpc.go` 的 `SubscribeAccountEvents` 接入，登记后发送响应头，再转发 `biz/account_event.go` 的通知。业务层用 `AccountEventBiz.Publish` 发布，调用位置必须在账户事务提交成功之后；当前尚未接入改密或删除入口。

通知仅覆盖当前中心进程的订阅者，每条订阅最多缓存 32 条待发送消息，满时丢弃该订阅的新消息并记录数量告警，不补发。广播不执行网络发送，网络收发仍由每条 gRPC 流独立处理并复查凭据。该机制不提供多个中心实例之间的广播。

中心停服先关闭账户通知通道，使空闲订阅退出，再停止传输服务；传输退出等待上限为五秒。测试用例、生成消息编译及 Mod 构建不代表游戏服已经接入订阅。
