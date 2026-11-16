package models

import (
	"gorm.io/datatypes"
	"time"
)

type Asset struct {
	ID          string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`       // id 字段
	RunID       string         `json:"run_id" gorm:"column:run_id;type:uuid"`          // run_id 字段
	AssetType   string         `json:"asset_type" gorm:"column:asset_type"`            // asset_type 字段
	AssetKey    string         `json:"asset_key" gorm:"column:asset_key"`              // asset_key 字段
	Attributes  datatypes.JSON `json:"attributes" gorm:"column:attributes;type:jsonb"` // attributes 字段
	FirstSeenAt time.Time      `json:"first_seen_at" gorm:"column:first_seen_at"`      // first_seen_at 字段
	LastSeenAt  time.Time      `json:"last_seen_at" gorm:"column:last_seen_at"`        // last_seen_at 字段
	CreatedAt   time.Time      `json:"created_at" gorm:"column:created_at"`            // created_at 字段
}

// TableName 固定对应数据库表名。
func (Asset) TableName() string { return "assets" }
