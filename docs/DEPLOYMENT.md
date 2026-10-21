# 构建、部署与配置

## 工具与协议

使用 Go 1.27.1。依赖版本由 go.mod/go.sum 固定；HTTP/gRPC 使用 Kratos，数据库使用 PostgreSQL + GORM。

`proto/` 是固定提交的 Git Submodule。首次检出后执行 `git submodule update --init --recursive`，不要复制协议文件或自动跟随远端分支。正常 Go 构建使用仓库中的 `gen/`；只有修改协议或核验生成结果时需要生成工具。

生成工具固定为 protoc 36.1、protoc-gen-go v1.36.11、protoc-gen-go-grpc v1.6.2。准备好 protoc 后，在仓库根目录执行：

```bash
mkdir -p .tools/bin
GOBIN="$PWD/.tools/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
GOBIN="$PWD/.tools/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
bash scripts/generate-proto.sh
```

生成文件不手工修改。安装工具及下载依赖需要网络或已有缓存。

## 配置与启动

复制 `configs/config.yaml` 为 `configs/config.local.yaml`，通过 `-conf` 指定。配置字段以仓库示例为准：

| 配置 | 用途 |
| --- | --- |
| server.http.addr | HTTP 监听地址，示例为 127.0.0.1:8000 |
| server.grpc.addr | gRPC 监听地址，示例为 127.0.0.1:9000 |
| postgres | 已存在的数据库地址、账号、数据库名、明文密码和 sslmode |
| id.node_id | 中心实例编号，范围 0–1023，不同同时运行实例不能重复 |
| jwt.signing_key | 独立的 JWT 签名密钥，至少 32 字节 |
| jwt.expires_in | JWT 有效期，默认 2h，必须为正整数秒的时长 |

配置在启动时读取，修改后重启。真实本机配置不纳入 Git；示例值需要替换。JWT 使用 HS256，不自动刷新，不提供单令牌撤销；更换签名密钥使旧令牌失效，调整有效期只影响随后签发的令牌。多实例互认令牌需使用同一签名密钥。

数据库必须预先存在。服务连接并 Ping 数据库后，在同一事务内迁移 users、servers、system_configs 并初始化默认配置，失败则退出，不启动 HTTP/gRPC 监听。连接和迁移共用 15 秒启动期限；连接池最多 10 个连接、2 个空闲连接，连接生命周期 30 分钟。

`system_configs` 中的 `auth.login_timeout_seconds` 是全服共用的玩家登录时限，单位为秒，默认 60，合法范围为 1–2147483647。启动初始化只补齐缺失项，不覆盖已有值；软删除或非法值会阻止启动。`ConfigService.GetConfig` 每次验证服务器凭据后读取数据库当前值，修改该项无需重启中心。运行期间缺失或非法配置返回 `Internal`，不会回退为默认值。游戏服轮询和登录超时执行需由 Mod 接入，中心提供接口不代表 Mod 已生效。

启动迁移不会自动删除列、重命名列或转换历史业务数据。模型主键由 Sonyflake v2 生成；固定 1ms 时间单位、12 位序列、10 位节点，起点为 2026-01-01 UTC。当前无节点占用检测或跨重启生成状态持久化，同节点快速重启及回拨后的唯一性不作保证。

## 账户与中心策略

名字为 3–16 位 ASCII 字母、数字或下划线，忽略大小写唯一；逻辑删除后名字仍被占用。密码为 6–12 位字母、数字或 `$%&@#^*!`，使用 Argon2id 保存，不要求混合字符种类。

先通过普通注册创建账户；首位管理员由部署者确认账户后在数据库中设置 users.is_admin=1。没有默认管理员或管理员初始化接口。

system_configs 保存所有游戏服共用的登录策略：

| 键 | 默认值 | 含义 |
| --- | --- | --- |
| auth.max_failures | 3 | 达到该失败次数立即进入休息期 |
| auth.cooldown_seconds | 300 | 休息时长，单位为秒 |

启动只补齐缺失项，不覆盖已有值；非法值或必需项被软删除会导致启动失败。登录每次读取中心策略。休息期内不校验密码且不延长截止时间，到期恢复尝试；不存在账户和注册失败不计数。HTTP 登录返回休息信息，不执行游戏踢人。

## 游戏服务器凭据与探测

管理员通过 [HTTP API](HTTP_API.md) 登记游戏服务器。创建和轮换仅在成功响应中交付原始密钥，中心保存 SHA-256 摘要，无法恢复原值。响应丢失后需再次轮换。

每次 gRPC 请求携带单个 `x-realmmesh-server-id` 和 `authorization: Bearer <原始密钥>`。中心逐次查库；停用、删除或轮换影响后续认证。空闲流在下次收发时发现凭据失效，已通过校验的在途操作不回撤。

中心当前没有 TLS，服务器密钥通过明文 gRPC 传输；认证和摘要存储不提供链路加密。HTTP JWT 与服务器凭据分别使用，不能互换。

```bash
go build -o bin/probe ./cmd/probe
# 预先设置 REALMMESH_SERVER_ID 与 REALMMESH_SERVER_SECRET
./bin/probe -addr 127.0.0.1:9000 -server-id "$REALMMESH_SERVER_ID" -message 'RealmMesh probe'
```

`GET /healthz` 仅表示进程存活，不检查数据库。RPC 日志记录 request_id、方法、状态和耗时，不记录正文、密钥或完整 metadata；探测工具会回显消息，不要将真实凭据作为探测消息。
