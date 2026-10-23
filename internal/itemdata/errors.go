// 本文件定义物品 NBT 解析和名单规则的固定错误类别

package itemdata

import "errors"

var (
	ErrMalformed = errors.New("invalid item NBT")
	ErrLimit     = errors.New("item NBT resource limit exceeded")
	ErrItemID    = errors.New("item NBT identifier mismatch")
	ErrRule      = errors.New("invalid item matching rule")
)
