# 共享物品仓库

第三阶段实现范围是 Minecraft 1.21.1 / NeoForge 与中心之间的账户物品仓库。代码已接入，数据库、HTTP/gRPC 和独立游戏物品类验证见末尾；真实双服双客户端跨服联调已通过，第三方模组及成品 JAR 部署仍待验证。

## 存取与并发

存入由游戏服读取真实主手物品，先严格编码，再在游戏线程暂扣到有限内存。中心规范化载荷、校验名单，在短事务内争取操作编号、更新库存并写资产审计；提交成功后游戏服释放暂存。明确拒绝才归还；离线或背包不足时保留，玩家重新认证后执行 `/realmmesh warehouse return`。未知结果不能直接归还。

取出先只读查询，游戏服检查还原与容量并确认“准备好，尚未发放”。中心以 `quantity >= amount` 条件更新扣减，扣减和操作/审计共同提交后才授权发放。游戏线程再检查连接、物品和空间，整笔给予；确定未发放则按原操作一次性退库。网络等待不占用数据库事务，不锁账户行，也没有跨服全局队列。

库存有三张表：`warehouse_items` 保存规范化单件载荷及数量，`warehouse_operations` 保存永久唯一操作编号与原参数，`warehouse_events` 追加资产结果。资产审计失败必须回滚库存和操作。它与第二阶段允许失败丢弃的普通业务审计不同。

```sql
CREATE UNIQUE INDEX uq_warehouse_item
ON warehouse_items (user_id, item_id, item_data_hash)
WHERE deleted_at IS NULL AND stackable = TRUE;
```

可堆叠记录用原子 upsert 累加，冲突时仍核对完整 `item_data`；相同 SHA-256 但完整数据不同时明确拒绝并审计。不可堆叠物品逐件建行。归零记录保留并从列表隐藏，保证明确失败可以退回原行；不可堆叠有效数量只能为 0 或 1。

同编号不同参数拒绝；已成功扣库的重复提交只返回原结果，不重复扣库。相同完成结果幂等，发放与退款竞争只允许一个终态。存入的名单拒绝、数量超限和哈希碰撞持久化为 `rejected`，后续名单变化不能让同一笔拒绝过的操作入库。该拒绝记录的库存 ID 是候选 ID，不表示实际库存。

未提交成功的取出失败不会保留拒绝终态，当前 Mod 不重放资产请求，新意图使用新编号。将来若加入自动重试，必须先设计失败编号的稳定结论，不能直接重用它。

## 物品数据和限制

游戏服使用原生 `ItemStack.SINGLE_ITEM_CODEC`，保存未压缩、大端、空根名称 Compound NBT。根 count 不进入载荷，嵌套 count 保留。严格拒绝部分解码、未知组件被丢弃或无损往返不成立；没有额外组件快照、版本迁移或兼容转换。

中心解析器只排序 compound 键，保留 NBT 标签类型、列表/数组顺序及数字原始位。字符串按 Java Modified UTF-8 处理。指纹只用于定位，完整载荷复核仍保留。中心不执行模组代码，不能保证依赖外部世界状态的物品跨服可用。

| 项目 | 当前工程上限 |
| --- | --- |
| 单件未压缩载荷 | 1 MiB |
| NBT 深度 / 节点数 | 64 / 65536 |
| 单次存取 | 2304 件；存入还受主手数量约束 |
| 单条新存入累计数量 | 2147483647 |
| Mod 单服暂存 | 256 条 / 64 MiB 载荷 |
| Mod 轻量完成记录 | 1024 条 |
| 名单 JSON / 规则总数 | 64 KiB / 256 条 |
| 单规则条件 / 路径段数 | 32 / 32 |

退款不受新存入数量上限限制，但检查 int64 溢出；库存超过存入上限后仍可正常取出。没有账户总行数或数据库总容量配额。数值为工程防护默认值，不是性能测试结论。Mod RPC 使用已有有限超时；超时后的未知条目不会因 TTL 自动回滚或释放，本服该账户暂停新资产操作，直到人工核查。不会删除库存或把物品掉到地上腾空间。

## 名单配置

`system_configs` 的 `warehouse.policy` 保存一个完整 JSON 快照。启动只补缺失值，默认 `{"only_whitelist":false,"blacklist":[],"whitelist":[]}`；软删除、null 或非法规则使启动校验失败，运行期读取损坏配置则拒绝新存入。管理员整体替换配置，每次新存入读当前快照，不缓存、不下发到游戏服。已入库物品不受新名单限制。

两个 POST 接口使用 Bearer JWT，逐次校验当前管理员权限：

- `/api/v1/admin/warehouse/policy`：返回完整策略
- `/api/v1/admin/warehouse/set-policy`：接受完整策略，成功返回 `{"updated":true}`

请求使用 application/json，设置接口正文最大 65536 字节，拒绝 null、未知字段、非法条件和多个 JSON 值。沿用项目当前 HTTP 成功 JSON / Kratos 错误契约；没有玩家 HTTP 存取接口。

以下例子展示仅 ID、仅 NBT、ID + NBT 三种规则：

```json
{
  "only_whitelist": false,
  "blacklist": [
    {"item_id": "example:world_linked_bag"},
    {"conditions": [{"path": [{"key": "components"}, {"key": "minecraft:custom_data"}, {"key": "bound"}], "operator": "exists"}]},
    {"item_id": "minecraft:diamond_sword", "conditions": [{"path": [{"key": "components"}, {"key": "minecraft:damage"}], "operator": "typed_equals", "type": "int", "value": 100}]}
  ],
  "whitelist": []
}
```

key 选择完整 compound 键（包含冒号的键无需拆分），index 选择从 0 开始的列表元素；数组不支持索引。每段二选一，路径缺失或类型不符均不命中。`exists` 不接受 type/value；`typed_equals` 支持 byte/short/int/long/float/double/string，严格匹配 NBT 类型，整数保留精度。禁止空规则、正则、通配符和脚本。

单条规则内条件 AND，规则间 OR。黑名单优先；only_whitelist=true 时必须命中白名单，空白名单拒绝全部新存入。仅 ID 规则忽略所有组件差异；通过白名单也不能绕过数据完整性与资源限制。

## 权限、失败和日志

新库存操作检查当前可信服务器和账户 ID/名字。账户删除后禁止新操作；仍有效的原服务器只能查询/结算原账户已有操作，不能创建新的发物授权。凭据撤销阻止后续 RPC，已通过校验的在途请求不回撤。

游戏服在主线程认领当前在途响应，不接受重复、迟到或状态不符的响应发物。只有确定未发放才能提交 inventory_full / player_unavailable / restore_failed / state_changed 退款原因。权限变化、超时、内部错误、响应冲突等按未知结果处理。`status` 只展示，不触发自动恢复。

通过 operation_id 关联中心三张表和游戏服日志，核对账户、服务器、库存、物品、数量、指纹及状态。未知、停服未完成和发放异常需人工诊断。没有本地 WAL、强制存档、重启扫描或自动补发；日志不证明玩家存档已经落盘。

## 生成与验证

固定 proto 子模块尚未包含本次未提交的仓库协议。本地生成使用：

```bash
REALMMESH_PROTO_DIR=../realmmesh-proto bash scripts/generate-proto.sh
go test -race ./...
go vet ./...
go build ./...
```

数据库测试须显式设置 `REALMMESH_WAREHOUSE_TEST_DSN` 后运行 `go test -race ./internal/repo ./internal/server -count=1`，否则明确 SKIP。测试在该数据库新建随机 schema，只清理自己创建的 schema。真实数据库操作仍需当次授权；不要把未设置 DSN 的通过当数据库验证。

本轮已获临时 schema 授权：12 组仓储并发/回滚测试、中心 HTTP/gRPC 流程/名单/权限集成通过；五份 NeoForge 实际导出载荷被 Go 读取并验证。Mod 独立 JVM 测试与构建结果见 Mod 任务记录。后续真实双服双客户端联调 30 项断言通过，覆盖双向转移、并发争抢、容量变化、离服退库及审计对账，见 [联调记录](../.agent/tasks/phase3-cross-server-live.md)。未执行第三方模组或成品 JAR 部署验证，未修改日常世界或配置。

中心侧压力基准已执行，覆盖真实 TCP gRPC、热点/分散存取、分页和幂等重放。存在超时与已提交未确认结果，详情见 [性能基准报告](WAREHOUSE_BENCHMARK.md)；不能把库存对账一致等同零错误或长期容量承诺。

压力超时的具体等待链见 [调查报告](WAREHOUSE_TIMEOUT_ANALYSIS.md)。前四项优化已实施，保持超时、事务与审计规则不变；三轮前后压力结果见 [优化对照报告](WAREHOUSE_OPTIMIZATION.md)，热点存入仍有波动，未宣称全面改善。
