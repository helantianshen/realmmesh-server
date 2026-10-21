// 账户认证：HTTP 访问控制边界
// 验证 Bearer JWT，账户信息接口读取当前账户，管理业务自行执行实时授权

package server

import (
	"strings"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/helantianshen/realmmesh-server/internal/biz"
	"github.com/helantianshen/realmmesh-server/internal/handler"
	"github.com/helantianshen/realmmesh-server/pkg/token"
)

// requireToken 验证 JWT 并传递签名中的身份，账户可用性和权限由后续业务检查
func requireToken(
	tokenManager *token.Manager,
	next func(khttp.Context, int64) error,
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

		return next(ctx, userID)
	}
}

// requireAccount 为账户信息接口读取当前账户，管理员信息接口额外检查权限
func requireAccount(
	accountBiz *biz.AccountBiz,
	tokenManager *token.Manager,
	adminOnly bool,
	next func(khttp.Context, *biz.Account) error,
) khttp.HandlerFunc {
	return requireToken(tokenManager, func(ctx khttp.Context, userID int64) error {
		account, err := accountBiz.Current(ctx, userID)
		if err != nil {
			return handler.BusinessError(err)
		}
		if adminOnly && !account.IsAdmin {
			return handler.ErrForbidden
		}
		return next(ctx, account)
	})
}
