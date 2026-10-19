# RealmMesh Server

第一阶段 Go/Kratos 中心，提供 HTTP 健康接口和 gRPC Echo。当前只验证测试消息，不包含账户、资产、TLS 或生产认证。

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
./bin/realmmesh -conf configs/config.yaml
# 在另一个终端中执行
curl --fail http://127.0.0.1:8000/healthz
./bin/probe -addr 127.0.0.1:9000 -message 'RealmMesh probe'
```

configs/config.yaml 是无密钥的本地示例，仅监听回环地址。机器专用修改可以复制为 configs/config.local.yaml，再用 -conf 指定；该命名已忽略提交。不要在已跟踪的示例中填入真实密钥。

服务端 Unary 拦截器记录 request_id、方法、状态和耗时，不记录请求/响应正文、配置、完整 metadata 或异常描述。单个非空且不超过 128 字节的关联 ID 被保留，不限制 UUID 格式，调用方不得在此字段放入配置或密钥；Go probe 未提供 ID 时由中心生成。probe 是显式本地测试工具，会回显测试消息，不用于输入真实密码或密钥。

## 交付与验收边界

候选文件逐项列在 [docs/PHASE1_FILES.txt](docs/PHASE1_FILES.txt)。其中 proto 是 gitlink，不是普通文件目录。.tools、bin、IDE 配置、.codex MCP 配置及 *.local.yaml 不交付。

截至第一阶段整理时，本仓库尚无首次提交。隔离快照构建只能证明候选文件及固定协议能够生成、测试和编译；首次提交后的实际克隆、Submodule 恢复与生成无差异验收仍待执行。提交与推送分别需要用户授权。

跨仓库的交付状态与详细结果见相邻 realmmesh-mod 仓库的 docs/PHASE1_DELIVERY.md。

Go 功能验证使用的临时 *_test.go 文件在验证完成后删除，不纳入提交；当前 go test 命令只检查包编译，不能视为持续回归测试覆盖。
