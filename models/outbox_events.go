package models

import (
	"gorm.io/datatypes"
	"time"
)

type OutboxEvent struct {
	ID            string         `json:"id" gorm:"type:uuid;primaryKey"` // 投递标识
	AggregateType string         `json:"aggregate_type"`                 // 聚合类别
	AggregateID   string         `json:"aggregate_id" gorm:"type:uuid"`  // 聚合标识
	EventType     string         `json:"event_type"`                     // 事件类别
	Payload       datatypes.JSON `json:"payload" gorm:"type:jsonb"`      // 通知载荷
	Status        string         `json:"status"`                         // 投递状态
	Attempts      int            `json:"attempts"`                       // 重试次数
	CreatedAt     time.Time      `json:"created_at"`                     // 创建时间
	PublishedAt   *time.Time     `json:"published_at"`                   // 投递时间
}

// TableName 指定可靠投递表。
func (OutboxEvent) TableName() string { return "outbox_events" }
