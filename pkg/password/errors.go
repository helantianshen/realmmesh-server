// 密码安全基础设施：哈希操作错误契约
// 区分编码无效、密码不匹配

package password

import "errors"

var (
	ErrHashInvalid = errors.New("密码哈希格式无效")
	ErrMismatch    = errors.New("密码不匹配")
)
