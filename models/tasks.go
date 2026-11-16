package models

import (
	"gorm.io/datatypes"
	"time"
)

type Task struct {
	ID                   string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`                             // id 字段
	RunID                string         `json:"run_id" gorm:"column:run_id;type:uuid"`                                // run_id 字段
	GoalID               *string        `json:"goal_id" gorm:"column:goal_id;type:uuid"`                              // goal_id 字段
	ParentID             *string        `json:"parent_id" gorm:"column:parent_id;type:uuid"`                          // parent_id 字段
	TaskType             string         `json:"task_type" gorm:"column:task_type"`                                    // task_type 字段
	Name                 string         `json:"name" gorm:"column:name"`                                              // name 字段
	Description          string         `json:"description" gorm:"column:description"`                                // description 字段
	Status               string         `json:"status" gorm:"column:status"`                                          // status 字段
	Priority             int            `json:"priority" gorm:"column:priority"`                                      // priority 字段
	RequiredCapabilities datatypes.JSON `json:"required_capabilities" gorm:"column:required_capabilities;type:jsonb"` // required_capabilities 字段
	Input                datatypes.JSON `json:"input" gorm:"column:input;type:jsonb"`                                 // input 字段
	Result               datatypes.JSON `json:"result" gorm:"column:result;type:jsonb"`                               // result 字段
	CreatedBy            string         `json:"created_by" gorm:"column:created_by"`                                  // created_by 字段
	RetryCount           int            `json:"retry_count" gorm:"column:retry_count"`                                // retry_count 字段
	MaxRetries           int            `json:"max_retries" gorm:"column:max_retries"`                                // max_retries 字段
	AvailableAt          time.Time      `json:"available_at" gorm:"column:available_at"`                              // available_at 字段
	DeadlineAt           *time.Time     `json:"deadline_at" gorm:"column:deadline_at"`                                // deadline_at 字段
	AssignedWorkerID     *string        `json:"assigned_worker_id" gorm:"column:assigned_worker_id;type:uuid"`        // assigned_worker_id 字段
	IdempotencyKey       string         `json:"idempotency_key" gorm:"column:idempotency_key"`                        // idempotency_key 字段
	CreatedAt            time.Time      `json:"created_at" gorm:"column:created_at"`                                  // created_at 字段
	StartedAt            *time.Time     `json:"started_at" gorm:"column:started_at"`                                  // started_at 字段
	FinishedAt           *time.Time     `json:"finished_at" gorm:"column:finished_at"`                                // finished_at 字段
	UpdatedAt            time.Time      `json:"updated_at" gorm:"column:updated_at"`                                  // updated_at 字段
}

// TableName 固定对应数据库表名。
func (Task) TableName() string { return "tasks" }
