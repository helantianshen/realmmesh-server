package biz

import "errors"

var (
	ErrPlayerNameInvalid  = errors.New("玩家名字必须为 3–16 位英文字母、数字或下划线")
	ErrPasswordInvalid    = errors.New("密码必须为 6–12 位英文字母、数字或 $%&@#^*! 符号")
	ErrPlayerNameTaken    = errors.New("玩家名字已被占用")
	ErrRegistrationFailed = errors.New("注册失败")
)
