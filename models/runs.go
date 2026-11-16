package models

import (
	"gorm.io/datatypes"
	"time"
)

type Run struct {
	ID         string         `json:"id" gorm:"column:id;primaryKey;type:uuid"` // id 字段
	Name       string         `json:"name" gorm:"column:name"`                  // name 字段
	Source     string         `json:"source" gorm:"column:source"`              // source 字段
	Mode       string         `json:"mode" gorm:"column:mode"`                  // mode 字段
	Status     string         `json:"status" gorm:"column:status"`              // status 字段
	Scope      datatypes.JSON `json:"scope" gorm:"column:scope;type:jsonb"`     // scope 字段
	Config     datatypes.JSON `json:"config" gorm:"column:config;type:jsonb"`   // config 字段
	MaxWorkers int            `json:"max_workers" gorm:"column:max_workers"`    // max_workers 字段
	MaxTasks   int            `json:"max_tasks" gorm:"column:max_tasks"`        // max_tasks 字段
	DeadlineAt *time.Time     `json:"deadline_at" gorm:"column:deadline_at"`    // deadline_at 字段
	StartedAt  *time.Time     `json:"started_at" gorm:"column:started_at"`      // started_at 字段
	FinishedAt *time.Time     `json:"finished_at" gorm:"column:finished_at"`    // finished_at 字段
	CreatedAt  time.Time      `json:"created_at" gorm:"column:created_at"`      // created_at 字段
	UpdatedAt  time.Time      `json:"updated_at" gorm:"column:updated_at"`      // updated_at 字段
}

// TableName 固定对应数据库表名。
func (Run) TableName() string { return "runs" }
