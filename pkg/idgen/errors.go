// 标识生成基础设施：初始化错误契约
// 定义 ID 生成器创建失败的错误类别

package idgen

import "errors"

var ErrInitialization = errors.New("初始化 ID 生成器失败")
