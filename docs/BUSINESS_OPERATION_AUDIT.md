# 中心业务操作审计

中心在 `business_operation_events` 中追加账户和服务器管理操作结果。表结构与索引随普通服务启动迁移，无需新增配置或更新游戏服协议。

## 记录范围

| 操作 | operation |
| --- | --- |
| 账户注册 | `account.register` |
| 登录凭据验证 | `account.login` |
| 玩家自助改密 | `account.change_password` |
| 管理员删除普通账户 | `account.delete` |
| 服务器创建、启用、停用 | `server.create`、`server.enable`、`server.disable` |
| 服务器密钥轮换、删除 | `server.rotate_secret`、`server.delete` |

HTTP 和 gRPC 共用业务入口，一次调用只产生一次审计尝试。改密内部的密码验证不会再记录一次登录；注册成功后游戏服自动放行也不会额外生成登录记录。

健康检查、配置拉取、详情与列表查询、通知订阅不写业务审计。解码或身份校验阶段拒绝、尚未进入业务的请求也不写此表。玩家进离服单独保存在 `player_connection_events`，参见[玩家连接审计](PLAYER_CONNECTION_AUDIT.md)。

## 结果与身份

- `result` 为 `succeeded`、`rejected` 或 `failed`，分别表示业务成功、规则拒绝和执行异常。
- `reason` 为固定原因码。密码错误为 `wrong_password`，本次错误触发休息期为 `cooldown_started`，已有休息期为 `cooling_down`；两种休息结果记录原 `cooldown_until`，不延长休息时间。
- 其他拒绝包括 `unregistered`、`invalid_player_name`、`invalid_password_format`、`player_name_taken`、`authentication_conflict`、`account_not_found`、`account_unavailable`、`admin_required`、`admin_deletion_forbidden`、`invalid_server_name`、`server_not_found`。
- 异常仅记录 `internal_error`、`canceled` 或 `deadline_exceeded`，不持久化底层异常正文。
- `source` 为 `http`、`grpc`，直接从内部调用业务而没有入口上下文时为 `internal`。
- `request_id` 与入口运行日志使用同一关联值；HTTP 响应头 `X-Request-ID` 返回该值。该字段不是身份凭据、唯一键或幂等标识。
- `server_id` 只取服务器凭据认证后的上下文；HTTP 请求为空。`actor_user_id` 来自已验证 JWT，或注册、密码验证成功的账户身份。密码错误时只记录尝试对象，不认定操作者。
- `target_type` 与 `target_id` 指向账户或服务器，未知 ID 为 NULL。合法 `player_name` 可用于追踪未注册账户；非法名字不原样存储。目标软删除不删除历史审计。
- `occurred_at` 是中心记录业务结果的 UTC 时间，另有模型通用的创建、更新和删除时间字段。

登录成功表示中心验证通过及认证状态提交，不证明 JWT 已送达或玩家已经进入游戏。改密、删除已提交但通知失败时仍记录业务成功，通知失败由运行日志单独说明。

## 写入与故障

业务先完成，审计在其事务之外同步执行一次 INSERT，不新增事务或行锁。单次审计 SQL 最多等待一秒，同时遵守请求取消和更早的截止时间；雪花 ID 生成失败或写库失败均只记安全警告，不替换业务结果。

这种方式增加一次数据库写入开销。业务成功而审计缺失是允许的，包括数据库异常、请求取消、进程退出以及业务提交后尚未写审计即终止的情况。没有后台队列、自动重试、恢复补报、完整性或不可篡改保证。

审计表不保存密码、哈希、服务器密钥及摘要、JWT、完整请求、SQL 参数或完整命令。当前不提供查询 HTTP API、管理页面和自动清理任务，可直接通过数据库按目标或玩家时间线查询。
