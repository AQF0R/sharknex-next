package models

import (
	"gorm.io/datatypes"
	"time"
)

type ExecutionEvent struct {
	ID        string         `json:"id" gorm:"type:uuid;primaryKey"` // 事件标识
	RunID     string         `json:"run_id" gorm:"type:uuid"`        // 所属运行
	WorkerID  *string        `json:"worker_id" gorm:"type:uuid"`     // 来源执行者
	TaskID    *string        `json:"task_id" gorm:"type:uuid"`       // 来源任务
	EventType string         `json:"event_type"`                     // 事件类别
	Payload   datatypes.JSON `json:"payload" gorm:"type:jsonb"`      // 安全事件载荷
	Sequence  int64          `json:"sequence" gorm:"->"`             // 数据库生成的恢复游标
	CreatedAt time.Time      `json:"created_at"`                     // 记录时间
}

// TableName 指定事件轨迹表。
func (ExecutionEvent) TableName() string { return "execution_events" }
