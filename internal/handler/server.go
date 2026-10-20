// 游戏服管理：管理员 HTTP 协议适配
// 解析服务器管理请求，调用业务对象并返回公开信息或一次性交付的凭证

package handler

import (
	"net/http"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/pkg/pagination"
)

// ServerHandler 将管理员请求交给服务器业务，操作者身份由认证入口提供
type ServerHandler struct {
	serverBiz *biz.ServerBiz
}

// createServerRequest 仅接受展示名，身份和凭证由中心生成
type createServerRequest struct {
	ServerName string `json:"server_name"`
}

// serverIDRequest 使用字符串编码读取雪花 ID，避免前端浮点数精度损失
type serverIDRequest struct {
	ServerID int64 `json:"server_id,string"`
}

// setServerEnabledRequest 要求显式提供 enabled，缺失或 null 不代表停用
type setServerEnabledRequest struct {
	serverIDRequest
	Enabled *bool `json:"enabled"`
}

// serverListRequest 复用公共分页字段，服务器查询条件在此结构中扩展
type serverListRequest struct {
	pagination.Request
}

// serverResponse 是管理查询的字段白名单，不包含凭证摘要
type serverResponse struct {
	ID         int64     `json:"id,string"`
	ServerName string    `json:"server_name"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// serverSecretResponse 仅用于创建和轮换成功后的凭证交付
type serverSecretResponse struct {
	ID     int64  `json:"id,string"`
	Secret string `json:"secret"`
}

// NewServerHandler 注入共享的服务器业务对象
func NewServerHandler(serverBiz *biz.ServerBiz) *ServerHandler {
	return &ServerHandler{serverBiz: serverBiz}
}

// Create 创建启用的服务器记录，只有持久化成功才交付原始密钥
func (h *ServerHandler) Create(ctx khttp.Context, account *biz.Account) error {
	var request createServerRequest
	if err := ctx.Bind(&request); err != nil {
		return ErrInvalidRequest
	}

	// 认证结果决定操作者，正文不能声明管理员身份
	issued, err := h.serverBiz.Register(ctx, account.ID, request.ServerName)
	if err != nil {
		return BusinessError(err)
	}
	return ctx.JSON(http.StatusCreated, serverSecretResponse{
		ID:     issued.ID,
		Secret: issued.Secret,
	})
}

// List 解析公共分页参数，取得业务结果后逐项映射为安全 HTTP 响应
func (h *ServerHandler) List(ctx khttp.Context, account *biz.Account) error {
	var request serverListRequest
	if err := ctx.Bind(&request); err != nil {
		return ErrInvalidRequest
	}

	// Resolve 补齐默认分页值，并拒绝显式零值、负数、超限和偏移量溢出
	params, err := request.Resolve()
	if err != nil {
		return BusinessError(err)
	}

	// 业务层复查管理员权限，仓储按固定顺序查询当前页
	page, err := h.serverBiz.List(ctx, account.ID, params)
	if err != nil {
		return BusinessError(err)
	}

	// 空页使用 []，客户端无需额外区分 null 和数组
	items := make([]serverResponse, 0, len(page.Items))
	for _, info := range page.Items {
		items = append(items, toServerResponse(info))
	}
	return ctx.JSON(http.StatusOK, pagination.Result[serverResponse]{
		Items:    items,
		Total:    page.Total,
		Page:     page.Page,
		PageSize: page.PageSize,
	})
}

// Detail 返回指定未删除服务器的管理信息，停用服务器仍可查询
func (h *ServerHandler) Detail(ctx khttp.Context, account *biz.Account) error {
	var request serverIDRequest
	if err := ctx.Bind(&request); err != nil || request.ServerID <= 0 {
		return ErrInvalidRequest
	}

	info, err := h.serverBiz.Detail(ctx, account.ID, request.ServerID)
	if err != nil {
		return BusinessError(err)
	}
	return ctx.JSON(http.StatusOK, toServerResponse(*info))
}

// SetEnabled 修改管理启停状态，重复设置同一状态仍成功，不改变密钥
func (h *ServerHandler) SetEnabled(ctx khttp.Context, account *biz.Account) error {
	var request setServerEnabledRequest
	if err := ctx.Bind(&request); err != nil {
		return ErrInvalidRequest
	}
	if request.ServerID <= 0 || request.Enabled == nil {
		return ErrInvalidRequest
	}

	if err := h.serverBiz.SetEnabled(ctx, account.ID, request.ServerID, *request.Enabled); err != nil {
		return BusinessError(err)
	}
	return ctx.Blob(http.StatusNoContent, "", nil)
}

// RotateSecret 替换存储摘要并交付新密钥，启停状态不随轮换改变
func (h *ServerHandler) RotateSecret(ctx khttp.Context, account *biz.Account) error {
	var request serverIDRequest
	if err := ctx.Bind(&request); err != nil || request.ServerID <= 0 {
		return ErrInvalidRequest
	}

	issued, err := h.serverBiz.RotateSecret(ctx, account.ID, request.ServerID)
	if err != nil {
		return BusinessError(err)
	}
	return ctx.JSON(http.StatusOK, serverSecretResponse{
		ID:     issued.ID,
		Secret: issued.Secret,
	})
}

// Delete 软删除指定服务器，记录不存在或已经删除时返回 404
func (h *ServerHandler) Delete(ctx khttp.Context, account *biz.Account) error {
	var request serverIDRequest
	if err := ctx.Bind(&request); err != nil || request.ServerID <= 0 {
		return ErrInvalidRequest
	}

	if err := h.serverBiz.Delete(ctx, account.ID, request.ServerID); err != nil {
		return BusinessError(err)
	}
	return ctx.Blob(http.StatusNoContent, "", nil)
}

// toServerResponse 显式映射可对外查询的字段，ID 由 JSON string 标签编码
func toServerResponse(info biz.ServerInfo) serverResponse {
	return serverResponse{
		ID:         info.ID,
		ServerName: info.ServerName,
		Enabled:    info.Enabled,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
	}
}
