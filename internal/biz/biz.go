// 业务组装：集中注入仓储和共享 ID 生成器

package biz

import (
	"github.com/helantianshen/realmmesh-server/internal/repo"
	"github.com/sony/sonyflake/v2"
)

// Businesses 保存账户与服务器业务对象，供协议入口共享
type Businesses struct {
	WarehouseBiz             *WarehouseBiz
	BusinessOperationBiz     *BusinessOperationBiz
	PlayerConnectionEventBiz *PlayerConnectionEventBiz
	AccountEventBiz          *AccountEventBiz
	SystemConfigBiz          *SystemConfigBiz
	AccountBiz               *AccountBiz
	RegistrationBiz          *RegistrationBiz
	LoginBiz                 *LoginBiz
	ServerBiz                *ServerBiz
}

// NewBusinesses 按业务依赖组装对象，不重复创建仓储或基础设施
func NewBusinesses(repositories *repo.Repositories, ids *sonyflake.Sonyflake) *Businesses {
	businessOperationBiz := NewBusinessOperationBiz(repositories.BusinessOperationEventRepo, ids)
	accountEventBiz := NewAccountEventBiz()
	loginBiz := NewLoginBiz(repositories.UserRepo, repositories.SystemConfigRepo, businessOperationBiz)
	return &Businesses{
		WarehouseBiz:             NewWarehouseBiz(repositories.WarehouseRepo, repositories.UserRepo, repositories.SystemConfigRepo, ids),
		BusinessOperationBiz:     businessOperationBiz,
		PlayerConnectionEventBiz: NewPlayerConnectionEventBiz(repositories.PlayerConnectionEventRepo, ids),
		AccountEventBiz:          accountEventBiz,
		SystemConfigBiz:          NewSystemConfigBiz(repositories.SystemConfigRepo),
		AccountBiz:               NewAccountBiz(repositories.UserRepo, loginBiz, accountEventBiz, businessOperationBiz),
		RegistrationBiz:          NewRegistrationBiz(repositories.UserRepo, ids, businessOperationBiz),
		LoginBiz:                 loginBiz,
		ServerBiz:                NewServerBiz(repositories.UserRepo, repositories.ServerRepo, ids, businessOperationBiz),
	}
}

// NewAccountEventBiz 创建进程内账户通知通道，供业务发布和协议订阅共享
func NewAccountEventBiz() *AccountEventBiz {
	return &AccountEventBiz{subscribers: make(map[chan AccountEvent]struct{})}
}

// NewAccountBiz 组装账户查询、密码验证与提交后通知依赖
func NewAccountBiz(
	userRepo *repo.UserRepository,
	loginBiz *LoginBiz,
	accountEventBiz *AccountEventBiz,
	businessOperationBiz *BusinessOperationBiz,
) *AccountBiz {
	return &AccountBiz{
		userRepo:             userRepo,
		loginBiz:             loginBiz,
		accountEventBiz:      accountEventBiz,
		businessOperationBiz: businessOperationBiz,
	}
}

// NewRegistrationBiz 组装用户仓储和共享 ID 生成器
func NewRegistrationBiz(
	userRepo *repo.UserRepository,
	ids *sonyflake.Sonyflake,
	businessOperationBiz *BusinessOperationBiz,
) *RegistrationBiz {
	return &RegistrationBiz{
		userRepo:             userRepo,
		ids:                  ids,
		businessOperationBiz: businessOperationBiz,
	}
}

// NewLoginBiz 组装用户仓储和中心配置仓储，所有服务器共用中心策略
func NewLoginBiz(
	userRepo *repo.UserRepository,
	systemConfigRepo *repo.SystemConfigRepository,
	businessOperationBiz *BusinessOperationBiz,
) *LoginBiz {
	return &LoginBiz{
		businessOperationBiz: businessOperationBiz,
		userRepo:             userRepo,
		systemConfigRepo:     systemConfigRepo,
	}
}

// NewServerBiz 组装管理员校验、服务器仓储与共享 ID 生成器
func NewServerBiz(
	userRepo *repo.UserRepository,
	serverRepo *repo.ServerRepository,
	ids *sonyflake.Sonyflake,
	businessOperationBiz *BusinessOperationBiz,
) *ServerBiz {
	return &ServerBiz{
		businessOperationBiz: businessOperationBiz,
		userRepo:             userRepo,
		serverRepo:           serverRepo,
		ids:                  ids,
	}
}

// NewSystemConfigBiz 注入中心配置仓储，供游戏服读取统一配置
func NewSystemConfigBiz(systemConfigRepo *repo.SystemConfigRepository) *SystemConfigBiz {
	return &SystemConfigBiz{systemConfigRepo: systemConfigRepo}
}

// NewPlayerConnectionEventBiz 注入审计仓储与共享 ID 生成器
func NewPlayerConnectionEventBiz(playerConnectionEventRepo *repo.PlayerConnectionEventRepository, ids *sonyflake.Sonyflake) *PlayerConnectionEventBiz {
	return &PlayerConnectionEventBiz{playerConnectionEventRepo: playerConnectionEventRepo, ids: ids}
}

// NewBusinessOperationBiz 注入审计仓储与共享 ID 生成器，不创建独立队列或事务
func NewBusinessOperationBiz(businessOperationEventRepo *repo.BusinessOperationEventRepository, ids *sonyflake.Sonyflake) *BusinessOperationBiz {
	return &BusinessOperationBiz{businessOperationEventRepo: businessOperationEventRepo, ids: ids}
}

// NewWarehouseBiz 组装仓库、账户和策略依赖，复用中心 ID 生成器
func NewWarehouseBiz(warehouseRepo *repo.WarehouseRepository, userRepo *repo.UserRepository, systemConfigRepo *repo.SystemConfigRepository, ids *sonyflake.Sonyflake) *WarehouseBiz {
	return &WarehouseBiz{warehouseRepo: warehouseRepo, userRepo: userRepo, systemConfigRepo: systemConfigRepo, ids: ids}
}
