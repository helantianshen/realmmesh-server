package model

import (
	"time"

	"gorm.io/gorm"
)

// TableName 指定用户模型对应的数据库表名
func (User) TableName() string {
	return "users"
}

type User struct {
	ID int64 `gorm:"type:bigint;primaryKey;autoIncrement:false;check:users_id_positive,id > 0"`
	// 名字保留原始大小写，唯一索引覆盖已逻辑删除的记录
	PlayerName    string         `gorm:"type:text;not null;uniqueIndex:users_player_name_lower_key,expression:lower(player_name)"`
	PasswordHash  string         `gorm:"type:text;not null"`
	IsAdmin       int16          `gorm:"type:smallint;not null;default:0;check:users_is_admin_valid,is_admin IN (0,1)"`
	LoginFailures int64          `gorm:"type:bigint;not null;default:0;check:users_login_failures_nonnegative,login_failures >= 0"`
	CooldownUntil *time.Time     `gorm:"type:timestamptz"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt     time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt     gorm.DeletedAt `gorm:"type:timestamptz"`
}
