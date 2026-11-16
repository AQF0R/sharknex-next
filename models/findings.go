package models

import (
	"gorm.io/datatypes"
	"time"
)

type Finding struct {
	ID           string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`              // id 字段
	RunID        string         `json:"run_id" gorm:"column:run_id;type:uuid"`                 // run_id 字段
	Title        string         `json:"title" gorm:"column:title"`                             // title 字段
	Description  string         `json:"description" gorm:"column:description"`                 // description 字段
	Severity     string         `json:"severity" gorm:"column:severity"`                       // severity 字段
	Status       string         `json:"status" gorm:"column:status"`                           // status 字段
	Confidence   float64        `json:"confidence" gorm:"column:confidence"`                   // confidence 字段
	SourceTaskID *string        `json:"source_task_id" gorm:"column:source_task_id;type:uuid"` // source_task_id 字段
	CreatedAt    time.Time      `json:"created_at" gorm:"column:created_at"`                   // created_at 字段
	UpdatedAt    time.Time      `json:"updated_at" gorm:"column:updated_at"`                   // updated_at 字段
	EvidenceRefs datatypes.JSON `json:"evidence_refs" gorm:"column:evidence_refs;type:jsonb"`  // evidence_refs 字段
}

// TableName 固定对应数据库表名。
func (Finding) TableName() string { return "findings" }
