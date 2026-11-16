package models

import (
	"gorm.io/datatypes"
	"time"
)

type Fact struct {
	ID             string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`                  // id 字段
	RunID          string         `json:"run_id" gorm:"column:run_id;type:uuid"`                     // run_id 字段
	AssetID        *string        `json:"asset_id" gorm:"column:asset_id;type:uuid"`                 // asset_id 字段
	FactType       string         `json:"fact_type" gorm:"column:fact_type"`                         // fact_type 字段
	Content        datatypes.JSON `json:"content" gorm:"column:content;type:jsonb"`                  // content 字段
	Confidence     float64        `json:"confidence" gorm:"column:confidence"`                       // confidence 字段
	SourceWorkerID *string        `json:"source_worker_id" gorm:"column:source_worker_id;type:uuid"` // source_worker_id 字段
	SourceTaskID   *string        `json:"source_task_id" gorm:"column:source_task_id;type:uuid"`     // source_task_id 字段
	Status         string         `json:"status" gorm:"column:status"`                               // status 字段
	ObservedAt     time.Time      `json:"observed_at" gorm:"column:observed_at"`                     // observed_at 字段
	CreatedAt      time.Time      `json:"created_at" gorm:"column:created_at"`                       // created_at 字段
}

// TableName 固定对应数据库表名。
func (Fact) TableName() string { return "facts" }
