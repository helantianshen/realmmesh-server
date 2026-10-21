# 玩家认证 RPC

中心使用固定协议引用中的 [AuthService 契约](../proto/auth/v1/README.md)，提供两个 Unary RPC：

| 方法 | 用途 |
| --- | --- |
| /realmmesh.auth.v1.AuthService/Register | 创建普通账户，成功后返回用户 ID 与账户名字 |
| /realmmesh.auth.v1.AuthService/Login | 校验密码并返回成功、未注册、密码错误或休息期结果 |

调用方通过 metadata 提交单个 `x-realmmesh-server-id` 和 `authorization: Bearer <server_secret>`。中心在每次请求进入业务前读取当前服务器状态；无效凭据不能创建账户或改变登录失败计数。当前传输为明文 gRPC。

请求中的玩家名字应由游戏服从实际连接获取。注册成功不返回 JWT，不创建中心在线会话；游戏服负责将发起请求的本次连接标记为已登录。

登录复用中心数据库策略。达到配置的失败次数即进入休息期，每次休息结果都带 `kick=true`，原截止时间不延长。休息期内即使密码正确也不放行；成功登录清零计数。用户身份字段本身不表示认证成功，只有 SUCCEEDED 状态可以放行。

登录密码错误、未注册、休息期通过正常 RPC 响应返回。非法名字或注册密码返回 InvalidArgument，重名返回 AlreadyExists，服务器凭据无效返回 Unauthenticated。仓储访问失败返回 Unavailable；配置异常、损坏密码哈希及未知内部错误返回 Internal。取消和超时保留 Canceled、DeadlineExceeded，响应不包含底层诊断详情。

不要自动重放注册或登录请求：超时可能发生在事务已提交之后，重试登录可能再次计数。Mod 命令、实际 kick 和旧连接响应隔离需要由游戏侧实现，中心返回标记不等于已经执行游戏操作。

## 开发验证

协议生成命令仍为 `bash scripts/generate-proto.sh`，同时生成 probe 与 auth，输出到 `gen/`。

```bash
go test -race ./...
```

命令只执行仓库当前保留的测试。玩家认证专项测试已清理，不构成持续回归覆盖；中心编译和测试通过也不代表 Java 或游戏实机联调通过。
