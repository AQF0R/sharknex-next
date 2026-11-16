package models

import (
	"gorm.io/datatypes"
	"time"
)

type Evidence struct {
	ID           string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`   // id 字段
	RunID        string         `json:"run_id" gorm:"column:run_id;type:uuid"`      // run_id 字段
	FactID       *string        `json:"fact_id" gorm:"column:fact_id;type:uuid"`    // fact_id 字段
	TaskID       *string        `json:"task_id" gorm:"column:task_id;type:uuid"`    // task_id 字段
	EvidenceType string         `json:"evidence_type" gorm:"column:evidence_type"`  // evidence_type 字段
	ContentRef   string         `json:"content_ref" gorm:"column:content_ref"`      // content_ref 字段
	ContentHash  string         `json:"-" gorm:"column:content_hash"`               // 仅服务端保留的摘要
	Metadata     datatypes.JSON `json:"metadata" gorm:"column:metadata;type:jsonb"` // metadata 字段
	CreatedAt    time.Time      `json:"created_at" gorm:"column:created_at"`        // created_at 字段
}

// TableName 固定对应数据库表名。
func (Evidence) TableName() string { return "evidences" }
