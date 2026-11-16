package models

import (
	"time"
)

type TaskLease struct {
	ID             string     `json:"id" gorm:"column:id;primaryKey;type:uuid"`    // id 字段
	TaskID         string     `json:"task_id" gorm:"column:task_id;type:uuid"`     // task_id 字段
	WorkerID       string     `json:"worker_id" gorm:"column:worker_id;type:uuid"` // worker_id 字段
	LeaseTokenHash string     `json:"-" gorm:"column:lease_token_hash"`            // 仅服务端保留的摘要
	Generation     int64      `json:"generation" gorm:"column:generation"`         // generation 字段
	Status         string     `json:"status" gorm:"column:status"`                 // status 字段
	AcquiredAt     time.Time  `json:"acquired_at" gorm:"column:acquired_at"`       // acquired_at 字段
	ExpiresAt      time.Time  `json:"expires_at" gorm:"column:expires_at"`         // expires_at 字段
	RenewedAt      *time.Time `json:"renewed_at" gorm:"column:renewed_at"`         // renewed_at 字段
	ReleasedAt     *time.Time `json:"released_at" gorm:"column:released_at"`       // released_at 字段
	SubmissionHash string     `json:"-" gorm:"column:submission_hash"`             // 仅服务端保留的摘要
}

// TableName 固定对应数据库表名。
func (TaskLease) TableName() string { return "task_leases" }
