package models

import (
	"gorm.io/datatypes"
	"time"
)

type Worker struct {
	ID              string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`                   // id 字段
	Name            string         `json:"name" gorm:"column:name"`                                    // name 字段
	RuntimeID       string         `json:"runtime_id" gorm:"column:runtime_id"`                        // runtime_id 字段
	Status          string         `json:"status" gorm:"column:status"`                                // status 字段
	MaxConcurrency  int            `json:"max_concurrency" gorm:"column:max_concurrency"`              // max_concurrency 字段
	ActiveTasks     int            `json:"active_tasks" gorm:"column:active_tasks"`                    // active_tasks 字段
	RuntimeMetadata datatypes.JSON `json:"runtime_metadata" gorm:"column:runtime_metadata;type:jsonb"` // runtime_metadata 字段
	LastHeartbeatAt *time.Time     `json:"last_heartbeat_at" gorm:"column:last_heartbeat_at"`          // last_heartbeat_at 字段
	RegisteredAt    time.Time      `json:"registered_at" gorm:"column:registered_at"`                  // registered_at 字段
	UpdatedAt       time.Time      `json:"updated_at" gorm:"column:updated_at"`                        // updated_at 字段
	RunID           string         `json:"run_id" gorm:"column:run_id;type:uuid"`                      // run_id 字段
	TokenHash       string         `json:"-" gorm:"column:token_hash"`                                 // 仅服务端保留的摘要
}

// TableName 固定对应数据库表名。
func (Worker) TableName() string { return "workers" }
