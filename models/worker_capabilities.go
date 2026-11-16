package models

import (
	"gorm.io/datatypes"
)

type WorkerCapability struct {
	WorkerID   string         `json:"worker_id" gorm:"column:worker_id;type:uuid"` // worker_id 字段
	Capability string         `json:"capability" gorm:"column:capability"`         // capability 字段
	Enabled    bool           `json:"enabled" gorm:"column:enabled"`               // enabled 字段
	Metadata   datatypes.JSON `json:"metadata" gorm:"column:metadata;type:jsonb"`  // metadata 字段
}

// TableName 固定对应数据库表名。
func (WorkerCapability) TableName() string { return "worker_capabilities" }
