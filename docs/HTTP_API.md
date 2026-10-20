# HTTP API

## 账户接口

注册不会自动签发令牌，登录成功才返回 JWT。首位管理员通过普通注册后由部署者手动设置，注册请求不能指定权限。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | /api/v1/auth/register | JSON 输入 player_name、password；201 返回 id（字符串）、player_name、is_admin=false |
| POST | /api/v1/auth/login | JSON 输入 player_name、password；200 返回 access_token、token_type、expires_at、user_id（字符串）、player_name |
| POST | /api/v1/auth/me | Bearer JWT；200 返回当前 id、player_name、is_admin |
| POST | /api/v1/admin/me | Bearer JWT 且当前为管理员；返回同上 |
| GET | /healthz | 无需认证，保持原有健康检查 |

注册和登录只接受 application/json，正文最多 4096 字节，拒绝未知字段和多个 JSON 值。身份查询不使用请求正文中的身份信息。现有 Kratos 路由对错误方法返回 404，业务不会执行。

错误使用 Kratos 的 code（HTTP 状态数字）、reason（固定业务码）、message 和 metadata。参数错误 400、认证失败 401、权限不足 403、名字冲突 409、休息期 429、内部异常 500。密码错误的 metadata.remaining_attempts、休息期的 metadata.retry_after_seconds 均为十进制字符串；休息期另有 cooldown_until 和 Retry-After 响应头。HTTP 不执行游戏 kick。响应不包含密码哈希或原始数据库错误，账户接口设置 Cache-Control: no-store。

受保护请求通过 Authorization: Bearer <JWT> 认证，每次查当前未删除账户和权限。账户删除和管理员降权影响后续请求；不提供自动刷新和单令牌撤销。HTTP 请求上下文超时为 10 秒。

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

分页默认第 1 页、每页 20 条，最大 100 条；省略或 null 采用默认值，显式零值、负数及非法类型返回 400。按 ID 倒序，排除软删除记录，保留停用记录；空页 items 为 []。同次请求的总数和数据页在只读 REPEATABLE READ 事务中查询，跨请求不保留快照。

示例正文：

```json
{"page": 1, "page_size": 20}
```

错误沿用 Kratos 格式：参数 400、身份无效 401、权限不足 403、服务器不存在 404、内部失败 500。所有管理响应禁止缓存，凭证不进入业务日志。其他 HTTP 方法由现有路由拒绝，不执行业务。
