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

登录密码计算期间账户密码发生变化时，返回 409 `AUTHENTICATION_CONFLICT`，本次不计失败次数、不签发令牌。客户端不得自动重试，由用户重新提交登录。

## 管理员删除账户

`POST /api/v1/admin/users/delete` 使用 `application/json` 和 `Authorization: Bearer <JWT>`，请求示例：

```json
{"user_id": "123456789012345678"}
```

`user_id` 必须是正整数的十进制字符串。中心每次查库确认操作者仍是管理员；Minecraft OP 不作为权限依据。禁止删除任何管理员账户，包括自己。

成功返回 204，无正文。账户软删除后名字仍永久占用，后续登录、改密和受保护账户接口拒绝该账户。重复删除返回 404，不重复通知。提交成功后广播 `ACCOUNT_DELETED`，通知失败不回滚删除，不提供恢复、离线补发或管理员重置密码接口。

参数错误返回 400；令牌或操作者账户无效返回 401；非管理员返回 403 `ADMIN_REQUIRED`；目标为管理员返回 403 `ADMIN_DELETION_FORBIDDEN`；目标不存在或已删除返回 404 `ACCOUNT_NOT_FOUND`；内部失败返回 500。身份认证先于参数解析，业务权限检查在参数解析之后。响应禁止缓存，错误不包含数据库详情。

## 服务器管理 HTTP 接口

以下路径统一以 /api/v1/admin/servers 开头，全部使用 POST、application/json 和 Authorization: Bearer <JWT>。入口验证 JWT，业务层每次查询当前账户并检查管理员权限，不重复查库；正文不接受操作者 ID。服务器 ID 使用 JSON 字符串。

| 路径 | 请求正文 | 成功响应 |
| --- | --- | --- |
| /create | server_name | 201，id、secret；默认启用 |
| /list | page、page_size（可省略） | 200，items、total、page、page_size |
| /detail | server_id | 200，公开服务器信息 |
| /set-enabled | server_id、enabled | 204，无正文 |
| /rotate-secret | server_id | 200，id、secret |
| /delete | server_id | 204，无正文 |

列表与详情仅返回 id、server_name、enabled、created_at、updated_at，不读取或返回密钥摘要。enabled 表示管理员设置的启停状态，不表示在线。原始密钥只在创建或轮换成功的响应交付，响应丢失时需再次轮换。停用保留密钥，重新启用后原密钥仍可用；轮换不改变启停状态。删除为软删除，重复删除或查询已删除记录返回 404。enabled 缺失或 null 返回 400，重复设置同一状态仍返回 204。

分页默认第 1 页、每页 20 条，最大 100 条；省略或 null 采用默认值，显式零值、负数及非法类型返回 400。按 ID 倒序，排除软删除记录，保留停用记录；空页 items 为 []。总数和数据页分别查询，并发增删时可能短暂不一致，不保留分页快照。

示例正文：

```json
{"page": 1, "page_size": 20}
```

错误沿用 Kratos 格式：参数 400、身份无效 401、权限不足 403、服务器不存在 404、内部失败 500。请求先验证 JWT，再解析参数并由业务层检查权限；有效 JWT 携带格式错误的参数时可能先返回 400。所有管理响应禁止缓存，凭证不进入业务日志。其他 HTTP 方法由现有路由拒绝，不执行业务。
