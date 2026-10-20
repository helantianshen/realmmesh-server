# RealmMesh Server

Go/Kratos 中心，提供 HTTP 健康接口和 gRPC Echo，并已接入 PostgreSQL 用户表及启动自动迁移。注册登录业务层与服务器密钥认证已实现；HTTP 注册、登录和身份查询已实现，游戏服玩家 RPC 入口和资产功能尚未实现。

## 环境与固定版本

本流程使用 Bash、Git、Go 1.27.1 和 protoc 36.1。依赖由 go.mod/go.sum 固定：Kratos v3.0.0、grpc-go v1.81.0、protobuf-go v1.36.11、x/net v0.55.0。Java 侧使用 gRPC 1.60.2，语言间库版本不要求相同。

Go 生成插件固定为 protoc-gen-go v1.36.11、protoc-gen-go-grpc v1.6.2，安装到仓库自己的 .tools/bin，不修改全局 GOBIN 或 PATH。protoc 必须单独安装 36.1 并可从当前 PATH 调用。

## 恢复协议与生成

以下命令在仓库根目录执行。首次提交完成后，检出仓库即可恢复 Submodule：

```bash
git submodule update --init --recursive
git -C proto rev-parse HEAD
```

本阶段预期提交为 `a9daf901a4e129a4a228cd75de2b7ef7f22c9ab4`，来源记录在 .gitmodules。使用 gitlink 固定提交，不跟随协议 main 分支自动更新。不能将协议文件作为普通目录复制提交。

```bash
go version
protoc --version
mkdir -p .tools/bin
GOBIN="$PWD/.tools/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
GOBIN="$PWD/.tools/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
bash scripts/generate-proto.sh
```

脚本检查三个生成工具的精确版本，生成 gen/probe/v1 下的 Go 文件。gen 纳入 Git，不手工修改。安装工具与依赖下载需要网络或已有的模块缓存。

## 测试与构建

```bash
go mod download
go mod verify
go test -race -count=1 ./...
go vet ./...
mkdir -p bin
go build -o bin/realmmesh ./cmd/realmmesh
go build -o bin/probe ./cmd/probe
```

race 检查需要本机支持的 Go 平台和 C 编译器。本阶段实际验证环境为 Linux x86_64；其他平台尚未验证。

## 本地运行

```bash
./bin/realmmesh -conf configs/config.local.yaml
# 在另一个终端中执行
curl --fail http://127.0.0.1:8000/healthz
# 使用已登记的服务器 ID；密钥通过环境变量提供，避免出现在命令行参数中
./bin/probe -addr 127.0.0.1:9000 -server-id "$REALMMESH_SERVER_ID" -message 'RealmMesh probe'
```

configs/config.yaml 是无密钥的本地示例，仅监听回环地址。机器专用修改可以复制为 configs/config.local.yaml，再用 -conf 指定；该命名已忽略提交。不要在已跟踪的示例中填入真实密钥。

服务端 Unary 拦截器记录 request_id、方法、状态和耗时，不记录请求/响应正文、配置、完整 metadata 或异常描述。单个非空且不超过 128 字节的关联 ID 被保留，不限制 UUID 格式，调用方不得在此字段放入配置或密钥；Go probe 未提供 ID 时由中心生成。probe 是显式本地测试工具，会回显测试消息，不用于输入真实密码或密钥。

## 交付与验收边界

候选文件逐项列在 [docs/PHASE1_FILES.txt](docs/PHASE1_FILES.txt)。其中 proto 是 gitlink，不是普通文件目录。.tools、bin、IDE 配置、.codex MCP 配置及 *.local.yaml 不交付。

2026-10-19：第一阶段已关闭。用户确认当前交付无问题，决定不再执行真实克隆复现验收及最终 request ID 规则变更后的日志联调复验；两项不再作为阶段待办，也不记作已执行或测试通过。 本地首次提交为 `2870733`。此前隔离快照的生成与构建结果仍作为历史证据保留。

跨仓库的交付状态与详细结果见相邻 realmmesh-mod 仓库的 docs/PHASE1_DELIVERY.md。

第一阶段以及第二阶段前两小步的临时测试已按用户要求删除。前两步的测试不再作为自动回归用例；此前实际验证结果保留在任务记录中。注册、系统配置与登录的临时测试也已清理；服务器凭证测试也已在审查通过后清理；当时清理后仅保留包编译检查和历史验证记录；JWT 历史验证及测试清理说明见文末。


## PostgreSQL 与启动迁移

依赖固定为 GORM v1.31.2、PostgreSQL 驱动 v1.6.3。数据库需预先存在；服务启动自动创建或迁移表，不负责创建数据库。

复制 `configs/config.yaml` 为 `configs/config.local.yaml`，填写已有数据库地址、账号、库名及明文 `password`。真实密码保存在被 Git 忽略的本机配置中，示例使用占位值；不要输出完整配置或连接串。本机配置已接入共享容器的独立 `realmmesh` 库，复用用户指定的现有账号和密码。`sslmode: disable` 仅对应当前回环连接环境，尚未验收远程 TLS。

启动顺序为配置校验 → 连接及 Ping → 事务内 `AutoMigrate(&User{})` → HTTP/gRPC 监听。连接和迁移共用 15 秒启动期限；连接池最多 10 个连接、2 个空闲连接，连接生命周期 30 分钟。失败会退出，正常退出关闭连接池。GORM SQL 日志关闭，数据库错误只显示阶段及可用的 SQLSTATE，避免连接串、参数和密码哈希进入日志。

`users` 保存 `id`、`player_name`、`password_hash`、`is_admin` 及三个时间字段。主键为正数 `bigint`，禁止自动增长，ID 由 Sonyflake v2 生成并在业务写入前显式赋值。`is_admin` 只允许 0/1，默认 0。密码哈希算法尚未实现，本步不创建账户。`created_at` 和 `updated_at` 由 GORM 常规创建/更新维护；原始 SQL 需要调用方维护时间。`deleted_at` 使用 `gorm.DeletedAt`，普通 GORM 查询过滤已删除记录。

`users_player_name_lower_key` 是覆盖全部记录的 `lower(player_name)` 唯一索引；名字保留原始大小写，逻辑删除后仍不能复用。没有额外添加删除字段索引。

迁移随每次服务启动运行。AutoMigrate 不负责删列、重命名或历史数据转换；本步验证单实例顺序启动，未验收多实例同时迁移。

```bash
go test ./...
go vet ./...
go build -o bin/realmmesh ./cmd/realmmesh
```

此前数据库测试在独立 schema 和事务内执行并回滚，已验证重复迁移、索引、非自增 ID、时间字段、管理员约束及软删除后名字不可复用。测试文件已在审查后清理。

本步结果与后续边界见 [.agent/tasks/phase2-database.md](.agent/tasks/phase2-database.md)。


## 雪花 ID

使用 github.com/sony/sonyflake/v2 v2.2.0，初始化位于 pkg/idgen/sonyflake.go。固定采用 1ms 时间单位、12 位序列、10 位节点，时间起点为 2026-01-01 00:00:00 UTC。已有 ID 使用期间不能修改编码布局与起点。

配置 id.node_id，范围 0–1023；本机示例为 1，未配置时 Go 零值为合法节点 0。节点编号标识中心服务实例，与游戏服务器 server_id 无关。同一编号不能同时被多个生成器使用；不从 IP 自动分配，也不自动检测编号冲突。

启动创建生成器并调用一次 NextID 自检，失败停止启动。尚未实现创建账户等业务入口，后续在启动入口将该生成器通过构造函数传给需要创建记录的组件；模型不挂自动生成 ID 的 GORM Hook，不使用全局生成器。数据库测试已覆盖生成 ID、显式赋值、写入和读取一致性。

基础接入不持久化生成器状态。同节点快速重启、时钟倒退后重启可能重复，运行中回拨可能等待；不承诺这些情况下的唯一性或等待上限。数据库主键拒绝重复写入，不等于跨重启生成保证。HTTP JSON 的 ID 字符串表示留到对应接口实现时处理。


## 错误处理约定

各模块将固定错误集中放在自己的 errors.go 中，以 var ErrXxx = errors.New(...) 定义。调用方通过 errors.Is 判断类别，不比较错误字符串。需要补充字段、路径、操作上下文时使用 fmt.Errorf 和 %w 包装；这类上下文信息可以保留在调用处，不为每条日志创建独立错误。

config/errors.go 定义配置校验错误，idgen/errors.go 定义初始化错误，data/errors.go 定义数据库错误及脱敏转换。数据库错误只暴露摘要和 SQLSTATE，取消/超时保留标准 context 错误身份。后续注册业务错误定义在 internal/biz/errors.go，传输层再映射 HTTP/gRPC 状态，不在业务层直接生成协议错误。


## 中心注册业务

internal/biz/registration.go 提供 NewRegistrationBiz 和 Register；传入 repo.NewUserRepository(db) 与共享 Sonyflake 实例。已接入 HTTP 注册接口，玩家 RPC 与 Mod 尚未接入。调用顺序为名字/密码校验、Argon2id 哈希、雪花 ID、数据库插入。返回值只有 ID 与原始大小写名字，不返回密码哈希或提供管理员赋值入口。

玩家名字限定 3–16 位 ASCII 英文字母、数字、下划线；密码限定 6–12 位英文字母、数字或 $%&@#^*!，不要求混合字符种类，不自动去空白或改大小写。唯一性由数据库 lower(player_name) 全表索引保障，已删除名字永久占用；只有该索引的 23505 错误映射为 ErrPlayerNameTaken。

pkg/password 使用 Argon2id，内存 19456 KiB、2 轮、并行度 1、16 字节随机盐、32 字节输出，存储带版本和参数的编码串。Verify 只接受当前固定参数并使用常量时间比较；不支持任意参数或自动升级哈希。每个并发哈希约需 19 MiB 内存，当前未对外开放入口，接入传输层前需要明确并发限制。调用前后检查 context；已经开始的 Argon2 计算不能中途取消。

注册错误位于 biz/errors.go，仓储错误位于 repo/errors.go，密码错误位于 password/errors.go，统一通过 errors.Is 判断。注册成功后的游戏连接登录状态、登录失败计数、TLS 和服务器凭证认证不在本步范围。

注册数据库测试已清理。此前使用独立 schema 完成实际 PostgreSQL 验证，历史结果保留在任务记录中。

仓储独立位于 internal/repo，拥有仓储错误及脱敏转换；internal/data 只负责数据库连接和迁移。移动后实际 PostgreSQL 注册测试、race、vet 和构建复验通过。

仓储依赖的字段及参数按职责命名，例如 userRepo、serverRepo，避免使用 users 等集合式命名。


## 中心系统配置

system_configs 由 model.SystemConfig 定义，id 使用雪花 ID，key 全表唯一。data.Migrate 在同一事务中执行表结构迁移及 data.InitSystemConfig；启动默认插入 auth.max_failures=3、auth.cooldown_seconds=300，key 冲突不覆盖已有值或更新时间。必需键被软删除、值非正整数或秒数超出 time.Duration 范围时启动失败，事务回滚。

repo.SystemConfigRepository.LoginPolicy 每次以单次查询读取中心配置，不按游戏服区分，不在 YAML 保存策略。缺失项通过下次启动补齐；已存在但非法或软删除的配置需要明确修正，启动不静默重置。模型与初始化函数均有用途注释，仓储依赖命名为 systemConfigRepo。

系统配置数据库测试已清理，历史验证使用隔离 schema，记录在事务结束后回滚。中心登录业务已接入这些配置，Mod kick 尚未实现。


接续开发前阅读 [开发约定与用户确认规则](docs/DEVELOPMENT_CONVENTIONS.md)。


## 中心登录业务

internal/biz/login.go 的 Authenticate 读取中心策略并调用 userRepo.UpdateLoginState。仓储在事务中通过 FOR UPDATE 锁定未删除账户，获取锁后读取 PostgreSQL clock_timestamp。业务先检查休息期，再校验密码；状态通过事务提交后才返回。

users 新增 login_failures（非负 bigint，默认 0）和 cooldown_until（可空 timestamptz）。默认第 3 次错误进入 300 秒休息期，期间每次登录返回 Kick=true、原截止时间和向上取整的剩余秒数，不验证密码、不修改状态。到期后的第一次错误从 1 开始；成功清零。不存在或已删除账户返回 unregistered，不创建记录。

LoginResult.Status 包含 succeeded、unregistered、wrong_password、cooling_down。正常认证拒绝使用结果返回以便提交失败计数，数据库错误、取消或损坏哈希才返回 error。返回信息不含密码哈希；同一账户哈希计算期间持有行锁，不同账户无应用级全局锁。

每个请求单次读取中心配置，后续请求可看到修改；已有休息截止时间不追溯重算。配置缺失或非法时拒绝处理，不回退到本地默认值。HTTP 登录已接入并签发 JWT；玩家 RPC、中心在线会话与 Mod 实际 kick 尚未接入。

中心登录已通过用户审查，临时测试文件已按要求清理。清理前已在实际 PostgreSQL 的独立 schema 中验证登录边界及跨连接并发；具体历史结果见 .agent/tasks/phase2-login.md。


## 服务器凭证与逐次认证

中心 gRPC 使用明文传输，每次调用（包括 Echo、gRPC 健康检查及反射）仍要求服务器凭证；HTTP /healthz 仍可用于本机健康检查。Mod 尚未接入凭据携带，现有 Mod 请求会被拒绝认证。

用户已决定移除中心 TLS，不需要证书或证书配置。server_secret 和后续玩家密码在当前 gRPC 通道中以明文传输；SHA-256 存储和逐次认证不提供传输保密、防窃听或防冒用能力。需要传输加密的部署环境应由外部加密隧道提供，不能将本实现描述为安全加密通道。

pkg/serversecret 使用 crypto/rand 生成 32 字节密钥，Base64URL 无填充编码为 43 字符；数据库只保存完整密钥文本的 SHA-256 十六进制摘要。摘要不能替代原始密钥认证。创建及轮换成功才交付原始密钥；若响应丢失，只能重新轮换，不能从数据库恢复原值。

internal/model/server.go 定义 servers 表及显式 TableName，主键为雪花 ID；server_name 用于展示，允许重名，限制 1–64 个字符且禁止控制字符和首尾空白。enabled 与 deleted_at 分开。查询按主键进行，因此当前没有添加名称、密钥或启用状态索引。普通启动将 servers 纳入事务迁移。

internal/biz/server.go 提供 Register、SetEnabled、RotateSecret、Delete 和 Authenticate。管理操作从 userRepo 查询当前管理员权限；操作者 ID 必须由已认证的管理入口提供，不能直接采用用户提交的 ID。服务器管理 HTTP API 与管理员身份查询均已接入。

每次 RPC 的 metadata 携带单个 x-realmmesh-server-id 和 authorization: Bearer <原始密钥>。拦截器查询数据库当前状态，不缓存认证结果；认证成功后通过 AuthenticatedServer(ctx) 提供身份。不存在、停用和密钥错误统一返回 Unauthenticated，数据库异常返回 Unavailable，响应不暴露驱动详情。Go probe 通过 REALMMESH_SERVER_SECRET 环境变量读取密钥，PerRPCCredentials 在每次明文调用中附加凭据。

流式 RPC 在建流、每次发送前、每次接收前以及收到消息后检查当前凭证。收到消息后的复查覆盖等待期间的撤销；失败会取消包装上下文并返回错误。处理器必须传播收发错误并响应上下文取消。空闲流在下一次收发时发现失效，不提供后台即时断开；已经通过检查的在途操作不回撤。当前没有真实事件协议，流式行为由测试专用服务验证，不等同于 Mod 事件联调。

本步已审查通过，pkg/serversecret/secret_test.go 和 internal/server/auth_test.go 已按用户要求清理。清理前已使用独立 PostgreSQL schema 和真实回环明文 gRPC 验证凭证生命周期及流式撤销，结果见 .agent/tasks/phase2-remove-tls.md。上述旧步骤的测试已不参与自动回归；JWT 历史验证及测试清理说明见下文。


## HTTP JWT 工具

pkg/token 使用 github.com/golang-jwt/jwt/v5 v5.3.1，固定 HS256。New(signingKey, lifetime) 创建管理器；Issue(userID) 为已经认证成功的用户签发令牌及截止时间；Verify(encoded) 校验通过后返回用户 ID。工具不查询数据库、不验证密码或管理员权限，不提供刷新或单令牌撤销，HTTP 接口由 internal/handler 和 internal/server 接入。

配置示例：

```yaml
jwt:
  signing_key: "change-me"
  expires_in: "2h"
```

signing_key 的示例占位符故意无法通过校验。实际配置应使用独立随机密钥，至少 32 字节，不复用数据库密码或 server_secret；本机已生成随机密钥并保存在被忽略且权限为 0600 的 configs/config.local.yaml，不随启动重新生成。多实例需要使用同一个签名密钥才能互认令牌。

expires_in 使用 2h、30m 等时长文本，省略或空字符串时默认两小时。只接受正整数秒，0、负值、小数秒和溢出值拒绝。JWTConfig.Validate 纳入启动配置校验；Lifetime 返回实际时长。配置在启动时读取，修改后重启；更短有效期不追溯缩短旧令牌的 exp，变更密钥则使旧密钥签发的令牌失效。

载荷只写入 user_id（字符串形式的 int64 用户 ID）和 exp（过期时间）。Claims 嵌入库的 RegisteredClaims，复用 JSON 解码和 JWT 库校验，不手工解析整数或校验用途。固定 HS256，要求 exp 存在且未过期，并检查用户 ID 为正数。时间精度为秒，到达截止时间立即失效，无额外宽限期。签名不加密载荷，不放入密码或管理员权限。

接入调用顺序为 JWTConfig.Lifetime → token.New → 认证成功后 Issue；受保护接口先 Verify，再查询账户是否仍存在及当前权限。启动入口已组装共享令牌管理器与 HTTP Handler，受保护路由在 JWT 校验后查询当前账户及权限。

新增 HTTP 接口统一使用 POST，包括查询接口；已有 GET /healthz 保持不变。默认有效期两小时，无自动刷新；当前没有改密后立即撤销旧令牌的机制。

本步已审查通过，pkg/token/jwt_test.go 与 internal/config/jwt_test.go 已按用户要求清理。清理前通过 race 测试，覆盖有效期配置、签发校验、过期边界、非法声明、算法替换、篡改、密钥变更及并发使用，详情见 .agent/tasks/phase2-jwt.md。这些 JWT 工具用例已不参与回归；HTTP 历史集成验证及清理状态见下文。

## HTTP 账户接口

协议适配统一位于 internal/handler；业务对象使用 XxxBiz，依赖和变量使用 xxxBiz。注册不会自动签发令牌，登录成功才返回 JWT。首位管理员通过普通注册后由部署者手动设置，注册请求不能指定权限。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | /api/v1/auth/register | JSON 输入 player_name、password；201 返回 id（字符串）、player_name、is_admin=false |
| POST | /api/v1/auth/login | JSON 输入 player_name、password；200 返回 access_token、token_type、expires_at、user_id（字符串）、player_name |
| POST | /api/v1/auth/me | Bearer JWT；200 返回当前 id、player_name、is_admin |
| POST | /api/v1/admin/me | Bearer JWT 且当前为管理员；返回同上 |
| GET | /healthz | 无需认证，保持原有健康检查 |

注册和登录只接受 application/json，正文最多 4096 字节，拒绝未知字段和多个 JSON 值。身份查询不使用请求正文中的身份信息。现有 Kratos 路由对错误方法返回 404，业务不会执行。

错误使用 Kratos 的 code（HTTP 状态数字）、reason（固定业务码）、message 和 metadata。参数错误 400、认证失败 401、权限不足 403、名字冲突 409、休息期 429、内部异常 500。密码错误的 metadata.remaining_attempts、休息期的 metadata.retry_after_seconds 均为十进制字符串；休息期另有 cooldown_until 和 Retry-After 响应头。HTTP 不执行游戏 kick。响应不包含密码哈希或原始数据库错误，账户接口设置 Cache-Control: no-store。

受保护请求通过 Authorization: Bearer <JWT> 认证，每次查当前未删除账户和权限。测试覆盖同一令牌下账户删除和管理员降权立即影响后续请求；不提供自动刷新和单令牌撤销。HTTP 请求上下文超时为 10 秒。

本步已审查通过，internal/server/http_test.go 已按用户要求清理。清理前在独立 PostgreSQL schema 中通过 HTTP 集成测试及 race 检查，结果保留在 .agent/tasks/phase2-http-auth-plan.md。清理后 go test ./... 只提供包编译检查，不代表这些行为仍有自动回归覆盖。

## 服务器管理 HTTP 接口

以下路径统一以 /api/v1/admin/servers 开头，全部使用 POST、application/json 和 Authorization: Bearer <JWT>。每次读取当前账户和管理员权限，业务层继续复查权限；正文不接受操作者 ID。服务器 ID 使用 JSON 字符串。

| 路径 | 请求正文 | 成功响应 |
| --- | --- | --- |
| /create | server_name | 201，id、secret；默认启用 |
| /list | page、page_size（可省略） | 200，items、total、page、page_size |
| /detail | server_id | 200，公开服务器信息 |
| /set-enabled | server_id、enabled | 204，无正文 |
| /rotate-secret | server_id | 200，id、secret |
| /delete | server_id | 204，无正文 |

列表与详情仅返回 id、server_name、enabled、created_at、updated_at，不读取或返回密钥摘要。enabled 表示管理员设置的启停状态，不表示在线。原始密钥只在创建或轮换成功的响应交付，响应丢失时需再次轮换。停用保留密钥，重新启用后原密钥仍可用；轮换不改变启停状态。删除为软删除，重复删除或查询已删除记录返回 404。enabled 缺失或 null 返回 400，重复设置同一状态仍返回 204。

公共分页位于 pkg/pagination：Request.Resolve 补齐默认值，Params.Validate 校验范围及偏移量溢出，Params.Offset 提供数据库偏移量，Result[T] 统一结果结构。服务器 Handler 通过 serverListRequest 嵌入公共请求字段，业务与仓储复用参数，不让公共组件依赖 GORM 或 HTTP 框架。

分页默认第 1 页、每页 20 条，最大 100 条；省略或 null 采用默认值，显式零值、负数及非法类型返回 400。按 ID 倒序，排除软删除记录，保留停用记录；空页 items 为 []。同次请求的总数和数据页在只读 REPEATABLE READ 事务中查询，跨请求不保留快照。

示例正文：

```json
{"page": 1, "page_size": 20}
```

错误沿用 Kratos 格式：参数 400、身份无效 401、权限不足 403、服务器不存在 404、内部失败 500。所有管理响应禁止缓存，凭证不进入业务日志。其他 HTTP 方法由现有路由拒绝，不执行业务。

本步已审查通过，pkg/pagination/pagination_test.go 和 internal/server/server_http_test.go 已按用户要求清理。清理前分页单测、独立 PostgreSQL schema 中的真实 HTTP/gRPC 集成测试及 race 检查通过，详情见 .agent/tasks/phase2-server-http-plan.md。清理后 go test ./... 仅检查包编译，不代表仍具备对应自动回归覆盖。
