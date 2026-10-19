# RealmMesh Server

Go/Kratos 中心，提供 HTTP 健康接口和 gRPC Echo，并已接入 PostgreSQL 用户表及启动自动迁移。注册登录、资产、TLS 和生产认证尚未实现。

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
./bin/probe -addr 127.0.0.1:9000 -message 'RealmMesh probe'
```

configs/config.yaml 是无密钥的本地示例，仅监听回环地址。机器专用修改可以复制为 configs/config.local.yaml，再用 -conf 指定；该命名已忽略提交。不要在已跟踪的示例中填入真实密钥。

服务端 Unary 拦截器记录 request_id、方法、状态和耗时，不记录请求/响应正文、配置、完整 metadata 或异常描述。单个非空且不超过 128 字节的关联 ID 被保留，不限制 UUID 格式，调用方不得在此字段放入配置或密钥；Go probe 未提供 ID 时由中心生成。probe 是显式本地测试工具，会回显测试消息，不用于输入真实密码或密钥。

## 交付与验收边界

候选文件逐项列在 [docs/PHASE1_FILES.txt](docs/PHASE1_FILES.txt)。其中 proto 是 gitlink，不是普通文件目录。.tools、bin、IDE 配置、.codex MCP 配置及 *.local.yaml 不交付。

2026-10-19：第一阶段已关闭。用户确认当前交付无问题，决定不再执行真实克隆复现验收及最终 request ID 规则变更后的日志联调复验；两项不再作为阶段待办，也不记作已执行或测试通过。 本地首次提交为 `2870733`。此前隔离快照的生成与构建结果仍作为历史证据保留。

跨仓库的交付状态与详细结果见相邻 realmmesh-mod 仓库的 docs/PHASE1_DELIVERY.md。

第一阶段以及第二阶段前两小步的临时测试已按用户要求删除。当前 go test 仅检查包编译，不代表仍有自动回归用例；此前实际验证结果保留在任务记录中。


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

使用 github.com/sony/sonyflake/v2 v2.2.0，初始化位于 internal/idgen/sonyflake.go。固定采用 1ms 时间单位、12 位序列、10 位节点，时间起点为 2026-01-01 00:00:00 UTC。已有 ID 使用期间不能修改编码布局与起点。

配置 id.node_id，范围 0–1023；本机示例为 1，未配置时 Go 零值为合法节点 0。节点编号标识中心服务实例，与游戏服务器 server_id 无关。同一编号不能同时被多个生成器使用；不从 IP 自动分配，也不自动检测编号冲突。

启动创建生成器并调用一次 NextID 自检，失败停止启动。尚未实现创建账户等业务入口，后续在启动入口将该生成器通过构造函数传给需要创建记录的组件；模型不挂自动生成 ID 的 GORM Hook，不使用全局生成器。数据库测试已覆盖生成 ID、显式赋值、写入和读取一致性。

基础接入不持久化生成器状态。同节点快速重启、时钟倒退后重启可能重复，运行中回拨可能等待；不承诺这些情况下的唯一性或等待上限。数据库主键拒绝重复写入，不等于跨重启生成保证。HTTP JSON 的 ID 字符串表示留到对应接口实现时处理。
