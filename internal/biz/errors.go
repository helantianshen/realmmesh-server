// 业务层错误契约：账户认证与游戏服管理
// 集中定义可判定的业务错误，供协议层映射为对外响应

package biz

import "errors"

var (
	ErrPlayerNameInvalid  = errors.New("玩家名字必须为 3–16 位英文字母、数字或下划线")
	ErrPasswordInvalid    = errors.New("密码必须为 6–12 位英文字母、数字或 $%&@#^*! 符号")
	ErrPlayerNameTaken    = errors.New("玩家名字已被占用")
	ErrRegistrationFailed = errors.New("注册失败")
)

var ErrLoginFailed = errors.New("登录处理失败")

var (
	ErrPasswordChangeFailed   = errors.New("改密处理失败")
	ErrAccountDeleteFailed    = errors.New("删除账户失败")
	ErrAccountNotFound        = errors.New("账户不存在或已删除")
	ErrAdminDeletionForbidden = errors.New("禁止删除管理员账户")
)

var (
	ErrAdminRequired           = errors.New("需要管理员权限")
	ErrServerNameInvalid       = errors.New("服务器名称必须为 1–64 个字符且不含控制字符或首尾空白")
	ErrServerCredentialInvalid = errors.New("服务器凭证无效或已停用")
	ErrServerOperation         = errors.New("服务器操作失败")
)

var ErrServerNotFound = errors.New("服务器不存在或已删除")

var (
	ErrAccountUnavailable = errors.New("账户不可用")
	ErrAccountLookup      = errors.New("账户查询失败")
)

var ErrSystemConfigRead = errors.New("读取游戏服配置失败")

var (
	ErrAccountEventsClosed = errors.New("账户通知通道已关闭")
	ErrAccountEventInvalid = errors.New("账户通知原因无效")
)

// ErrAuthenticationConflict 表示计算期间密码已变化，调用方不得自动重试
var ErrAuthenticationConflict = errors.New("账户状态已变化，请重新尝试")

var (
	ErrPlayerConnectionEventInvalid = errors.New("玩家连接审计参数无效")
	ErrPlayerConnectionEventWrite   = errors.New("玩家连接审计写入失败")
)
