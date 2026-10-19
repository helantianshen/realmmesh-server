package password

import "errors"

var (
	ErrHashInvalid    = errors.New("密码哈希格式无效")
	ErrMismatch       = errors.New("密码不匹配")
	ErrSaltGeneration = errors.New("生成密码盐失败")
)
