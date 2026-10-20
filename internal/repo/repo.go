// 数据访问组装：集中创建共享数据库连接的实体仓储

package repo

import "gorm.io/gorm"

// Repositories 保存中心服务共用的仓储对象
type Repositories struct {
	UserRepo         *UserRepository
	SystemConfigRepo *SystemConfigRepository
	ServerRepo       *ServerRepository
}

// NewRepositories 使用同一连接池创建各实体仓储
func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		UserRepo:         NewUserRepository(db),
		SystemConfigRepo: NewSystemConfigRepository(db),
		ServerRepo:       NewServerRepository(db),
	}
}

// NewUserRepository 创建使用指定数据库连接的用户仓储
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// NewSystemConfigRepository 创建使用指定连接的系统配置仓储
func NewSystemConfigRepository(db *gorm.DB) *SystemConfigRepository {
	return &SystemConfigRepository{db: db}
}

// NewServerRepository 创建负责服务器记录的仓储
func NewServerRepository(db *gorm.DB) *ServerRepository {
	return &ServerRepository{db: db}
}
