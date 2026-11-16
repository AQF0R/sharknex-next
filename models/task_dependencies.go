package models

import (
	"time"
)

type TaskDependency struct {
	ID             string    `json:"id" gorm:"column:id;primaryKey;type:uuid"`      // id 字段
	TaskID         string    `json:"task_id" gorm:"column:task_id;type:uuid"`       // task_id 字段
	DependsOn      string    `json:"depends_on" gorm:"column:depends_on;type:uuid"` // depends_on 字段
	DependencyType string    `json:"dependency_type" gorm:"column:dependency_type"` // dependency_type 字段
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at"`           // created_at 字段
}

// TableName 固定对应数据库表名。
func (TaskDependency) TableName() string { return "task_dependencies" }
