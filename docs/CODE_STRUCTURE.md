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
| internal/biz | 账户、服务器与仓库业务规则；biz.go 集中构造并返回 Businesses |
| internal/itemdata | 有界 NBT 解析、规范化和有限名单匹配，不执行游戏代码 |
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

数据库连接关闭 GORM 默认写事务包装，普通单条写入使用 PostgreSQL 自动提交。启动迁移、账户密码/失败状态更新及仓库资产操作显式使用短事务。仓库库存、操作去重和资产审计必须在同一事务提交，不在事务内等待游戏服网络响应。

HTTP 方法、路径、路由组及权限绑定集中在 server/route.go；http.go 保留超时、JSON 解码和缓存策略，grpc.go 保留监听配置及认证、日志拦截器。处理器注释标明完整路由，请求响应类型靠近首个使用的处理方法，共用类型只定义一次。

增加现有能力时修改对应业务文件及集中组装入口，不在 main.go 逐项构造。玩家认证由 handler/auth_grpc.go 复用注册和登录业务，经 handler.go 构造并由 server/route.go 注册。

游戏服配置读取经 `handler/config_grpc.go` → `biz/system_config.go` → `repo/system_config.go`，逐次服务器认证复用统一 gRPC 拦截器。默认登录时限及启动校验在 `data/system_config.go` 完成，配置键在 `model/system_config.go` 定义。

账户变更订阅由 `handler/auth_grpc.go` 的 `SubscribeAccountEvents` 接入，登记后发送响应头，再转发 `biz/account_event.go` 的通知。`AccountBiz` 在改密或删除提交成功后调用 `AccountEventBiz.Publish`，通知失败不改变已提交结果。

自助改密由 `handler/auth_grpc.go` → `biz/account.go` 发起，复用 `biz/login.go` 的密码校验事务流程；`repo/user.go` 的 `UpdateAuthenticationState` 在短事务内复核最新密码哈希及休息状态，并串行提交同账户的密码和失败次数；Argon2id 验证和新哈希生成在事务外完成。管理员删除由 `handler/account.go` → `biz/account.go` → `repo/user.go`，先检查操作者当前权限，再以 `is_admin = 0` 条件软删除目标。两个入口均不创建中心玩家会话。

通知仅覆盖当前中心进程的订阅者，每条订阅最多缓存 32 条待发送消息，满时丢弃该订阅的新消息并记录数量告警，不补发。广播不执行网络发送，网络收发仍由每条 gRPC 流独立处理并复查凭据。该机制不提供多个中心实例之间的广播。

中心停服先关闭账户通知通道，使空闲订阅退出，再停止传输服务；传输退出等待上限为五秒。Mod 已接入账户订阅与断线重连；中心停服恢复和双服账户通知已通过独立实机验收，构建检查本身不替代实机验证。

中心业务操作在各业务入口执行完成后交给 `biz/business_operation.go` 分类，由 `repo/business_operation_event.go` 在业务事务之外追加到 `model/business_operation_event.go` 对应表。HTTP 过滤器与 gRPC 日志拦截器传入同一个请求关联标识，审计不解析协议正文；单次写入最多等待一秒，失败只写安全日志。详见[中心业务操作审计](BUSINESS_OPERATION_AUDIT.md)。

共享仓库由 `handler/warehouse_grpc.go` → `biz/warehouse.go` → `repo/warehouse.go` 处理；`itemdata` 校验/规范化物品，`model/warehouse.go` 定义库存、幂等操作和资产事件。`data/warehouse.go` 建立部分唯一索引，启动初始化 `warehouse.policy`。管理员名单经 `handler/warehouse.go` 接入，完整说明见[共享物品仓库](WAREHOUSE.md)。
