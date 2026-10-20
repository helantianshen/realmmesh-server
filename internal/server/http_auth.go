// 账户认证：HTTP 访问控制边界
// 验证 Bearer JWT 并读取当前账户，对管理路由检查实时管理员权限

package server

import (
	"strings"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// requireAccount 为受保护路由验证 JWT，再读取当前账户与权限
// 已验证账户通过参数传给 Handler，不接受请求正文中的用户 ID 或权限声明
// adminOnly 为 true 时要求当前账户是管理员；任何检查失败都不会调用 next
func requireAccount(
	accountBiz *biz.AccountBiz,
	tokenManager *token.Manager,
	adminOnly bool,
	next func(khttp.Context, *biz.Account) error,
) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		// 1. 只接受一个认证头，避免重复值造成身份解释不一致
		authorizationHeaders := ctx.Request().Header.Values("Authorization")
		if len(authorizationHeaders) != 1 {
			return handler.ErrUnauthorized
		}
		// Fields 拆分认证方案与令牌；EqualFold 允许标准认证方案名称大小写不同
		credentials := strings.Fields(authorizationHeaders[0])
		if len(credentials) != 2 {
			return handler.ErrUnauthorized
		}

		authScheme, encodedToken := credentials[0], credentials[1]
		if !strings.EqualFold(authScheme, "Bearer") {
			return handler.ErrUnauthorized
		}

		// 2. 验证签名和有效期，身份只取自已验证的令牌载荷
		userID, err := tokenManager.Verify(encodedToken)
		if err != nil {
			return handler.ErrUnauthorized
		}

		// 3. 每次查库取得当前账户与权限，JWT 本身不能证明账户仍然可用
		account, err := accountBiz.Current(ctx, userID)
		if err != nil {
			return handler.BusinessError(err)
		}

		// 4. 管理路由额外检查权限，然后把可信账户交给处理器
		if adminOnly && !account.IsAdmin {
			return handler.ErrForbidden
		}

		return next(ctx, account)
	}
}
