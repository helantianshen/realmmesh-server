// 业务组装：集中注入仓储和共享 ID 生成器

package biz

import (
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
)

// Businesses 保存账户与服务器业务对象，供协议入口共享
type Businesses struct {
	AccountBiz      *AccountBiz
	RegistrationBiz *RegistrationBiz
	LoginBiz        *LoginBiz
	ServerBiz       *ServerBiz
}

// NewBusinesses 按业务依赖组装对象，不重复创建仓储或基础设施
func NewBusinesses(repositories *repo.Repositories, ids *sonyflake.Sonyflake) *Businesses {
	return &Businesses{
		AccountBiz:      NewAccountBiz(repositories.UserRepo),
		RegistrationBiz: NewRegistrationBiz(repositories.UserRepo, ids),
		LoginBiz:        NewLoginBiz(repositories.UserRepo, repositories.SystemConfigRepo),
		ServerBiz:       NewServerBiz(repositories.UserRepo, repositories.ServerRepo, ids),
	}
}

// NewAccountBiz 组装账户查询业务
func NewAccountBiz(userRepo *repo.UserRepository) *AccountBiz {
	return &AccountBiz{userRepo: userRepo}
}

// NewRegistrationBiz 组装用户仓储和共享 ID 生成器
func NewRegistrationBiz(userRepo *repo.UserRepository, ids *sonyflake.Sonyflake) *RegistrationBiz {
	return &RegistrationBiz{userRepo: userRepo, ids: ids}
}

// NewLoginBiz 组装用户仓储和中心配置仓储，所有服务器共用中心策略
func NewLoginBiz(userRepo *repo.UserRepository, systemConfigRepo *repo.SystemConfigRepository) *LoginBiz {
	return &LoginBiz{
		userRepo:         userRepo,
		systemConfigRepo: systemConfigRepo,
	}
}

// NewServerBiz 组装管理员校验、服务器仓储与共享 ID 生成器
func NewServerBiz(userRepo *repo.UserRepository, serverRepo *repo.ServerRepository, ids *sonyflake.Sonyflake) *ServerBiz {
	return &ServerBiz{
		userRepo:   userRepo,
		serverRepo: serverRepo,
		ids:        ids,
	}
}
