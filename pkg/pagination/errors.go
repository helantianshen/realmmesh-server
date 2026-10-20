// 分页参数：公共错误契约
// 对外提供统一的非法分页参数错误类别

package pagination

import "errors"

var ErrInvalid = errors.New("页码必须为正整数，每页条数必须为 1–100，分页偏移量不能溢出")
