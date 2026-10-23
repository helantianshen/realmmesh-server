// 数据访问组装：集中创建共享数据库连接的实体仓储

package repo

import "gorm.io/gorm"

// Repositories 保存中心服务共用的仓储对象
type Repositories struct {
	WarehouseRepo              *WarehouseRepository
	BusinessOperationEventRepo *BusinessOperationEventRepository
	PlayerConnectionEventRepo  *PlayerConnectionEventRepository
	UserRepo                   *UserRepository
	SystemConfigRepo           *SystemConfigRepository
	ServerRepo                 *ServerRepository
}

// NewRepositories 使用同一连接池创建各实体仓储
func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		WarehouseRepo:              NewWarehouseRepository(db),
		BusinessOperationEventRepo: NewBusinessOperationEventRepository(db),
		PlayerConnectionEventRepo:  NewPlayerConnectionEventRepository(db),
		UserRepo:                   NewUserRepository(db),
		SystemConfigRepo:           NewSystemConfigRepository(db),
		ServerRepo:                 NewServerRepository(db),
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

// NewPlayerConnectionEventRepository 组装共享连接池的玩家连接审计仓储
func NewPlayerConnectionEventRepository(db *gorm.DB) *PlayerConnectionEventRepository {
	return &PlayerConnectionEventRepository{db: db}
}

// NewBusinessOperationEventRepository 组装业务事务之外使用的审计仓储
func NewBusinessOperationEventRepository(db *gorm.DB) *BusinessOperationEventRepository {
	return &BusinessOperationEventRepository{db: db}
}

// NewWarehouseRepository 复用连接池，仓库写入自行控制短事务
func NewWarehouseRepository(db *gorm.DB) *WarehouseRepository { return &WarehouseRepository{db: db} }
