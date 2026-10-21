// 分页基础设施：公共参数与结果结构
// 统一默认值、范围校验和偏移量计算，业务查询条件由各仓储负责

// Package pagination 提供与数据库及 HTTP 框架无关的分页参数和泛型结果
package pagination

import "math"

const (
	DefaultPage     = 1   // 默认从第一页开始查询
	DefaultPageSize = 20  // 默认每页返回 20 条记录
	MaxPageSize     = 100 // 单页最多返回 100 条记录
)

// Request 接收可选分页字段，省略或 null 使用默认值，显式零值不作为默认值
// 指针用于区分未提供参数与明确传入 0
type Request struct {
	Page     *int `json:"page"`
	PageSize *int `json:"page_size"`
}

// Params 保存补齐默认值后的分页参数，业务层通过 Validate 后交给仓储
type Params struct {
	Page     int
	PageSize int
}

// Result 统一分页响应结构，空页的 Items 应使用空切片而不是 nil
type Result[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// Resolve 只补齐省略的默认值，保留显式输入供业务层校验
func (request Request) Resolve() Params {
	params := Params{
		Page:     DefaultPage,
		PageSize: DefaultPageSize,
	}
	if request.Page != nil {
		params.Page = *request.Page
	}
	if request.PageSize != nil {
		params.PageSize = *request.PageSize
	}

	return params
}

// Validate 检查页码、单页上限及偏移量溢出，避免整数溢出绕过数据库分页
func (params Params) Validate() error {
	if params.Page < 1 || params.PageSize < 1 || params.PageSize > MaxPageSize {
		return ErrInvalid
	}

	// 先用除法判断范围，再由 Offset 执行乘法，避免检查前已经发生溢出
	if params.Page-1 > math.MaxInt/params.PageSize {
		return ErrInvalid
	}
	return nil
}

// Offset 返回从零开始的记录偏移量，调用者必须先通过 Validate
func (params Params) Offset() int {
	return (params.Page - 1) * params.PageSize
}
