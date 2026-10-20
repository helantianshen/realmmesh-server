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
	ErrAdminRequired           = errors.New("需要管理员权限")
	ErrServerNameInvalid       = errors.New("服务器名称必须为 1–64 个字符且不含控制字符或首尾空白")
	ErrServerCredentialInvalid = errors.New("服务器凭证无效或已停用")
	ErrServerOperation         = errors.New("服务器操作失败")
)

var ErrServerNotFound = errors.New("服务器不存在或已删除")
