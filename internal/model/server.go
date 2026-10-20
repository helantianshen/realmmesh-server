package model

import (
	"time"

	"gorm.io/gorm"
)

// Server 保存游戏服务器身份、凭证摘要及可用状态
// 名称仅用于展示，服务器身份由雪花主键确定，原始密钥不入库

// TableName 指定服务器记录对应的数据库表名
func (Server) TableName() string {
	return "servers"
}

type Server struct {
	ID         int64          `gorm:"type:bigint;primaryKey;autoIncrement:false;check:servers_id_positive,id > 0"`
	ServerName string         `gorm:"type:text;not null"`
	SecretHash string         `gorm:"type:text;not null" json:"-"`
	Enabled    bool           `gorm:"not null;default:false"`
	CreatedAt  time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt  time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt  gorm.DeletedAt `gorm:"type:timestamptz"`
}
