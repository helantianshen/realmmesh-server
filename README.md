# RealmMesh Server

Go/Kratos 中心服务，提供账户注册、登录、JWT 身份查询、管理员服务器管理，以及带服务器凭据认证的 gRPC Echo。使用 PostgreSQL + GORM 持久化，启动时执行迁移和默认系统配置初始化。

中心已提供玩家注册与登录 RPC；Mod 的游戏内注册登录、实际踢人和资产功能尚未接入。

## 使用文档

- [构建、部署与配置](docs/DEPLOYMENT.md)
- [HTTP API](docs/HTTP_API.md)
- [玩家认证 RPC](docs/PLAYER_AUTH_RPC.md)
- [代码结构](docs/CODE_STRUCTURE.md)

## 快速开始

需要 Go 1.27.1 和已有 PostgreSQL 数据库。在仓库根目录执行：

```bash
git submodule update --init --recursive
go mod download
mkdir -p bin
go build -o bin/realmmesh ./cmd/realmmesh
cp configs/config.yaml configs/config.local.yaml
```

编辑本机配置，填写数据库连接信息、中心节点编号和至少 32 字节的 JWT 签名密钥，再启动：

```bash
./bin/realmmesh -conf configs/config.local.yaml
```

另开终端检查存活状态：

```bash
curl --fail http://127.0.0.1:8000/healthz
```

示例中的 `change-me` 不是可用的 JWT 密钥。`configs/*.local.yaml` 不纳入 Git，请勿把真实密码或密钥写入公开示例。

## 开发检查

```bash
go test -race ./...
go vet ./...
go build ./...
```

race 检查需要受支持的平台和 C 编译器。只有实际存在的测试用例参与行为验证；无测试文件的包仅完成编译检查，不能据此判断全部业务已回归。
