package models

import (
	"gorm.io/datatypes"
	"time"
)

type TaskProposal struct {
	ID           string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`              // id 字段
	RunID        string         `json:"run_id" gorm:"column:run_id;type:uuid"`                 // run_id 字段
	WorkerID     string         `json:"worker_id" gorm:"column:worker_id;type:uuid"`           // worker_id 字段
	ParentTaskID *string        `json:"parent_task_id" gorm:"column:parent_task_id;type:uuid"` // parent_task_id 字段
	Reason       string         `json:"reason" gorm:"column:reason"`                           // reason 字段
	ProposedTask datatypes.JSON `json:"proposed_task" gorm:"column:proposed_task;type:jsonb"`  // proposed_task 字段
	Status       string         `json:"status" gorm:"column:status"`                           // status 字段
	ReviewReason string         `json:"review_reason" gorm:"column:review_reason"`             // review_reason 字段
	ReviewedBy   string         `json:"reviewed_by" gorm:"column:reviewed_by"`                 // reviewed_by 字段
	ReviewedAt   *time.Time     `json:"reviewed_at" gorm:"column:reviewed_at"`                 // reviewed_at 字段
	CreatedAt    time.Time      `json:"created_at" gorm:"column:created_at"`                   // created_at 字段
}

// TableName 固定对应数据库表名。
func (TaskProposal) TableName() string { return "task_proposals" }
