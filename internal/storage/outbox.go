package storage

import (
	"autonomous-engine/models"
	"context"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Outbox struct {
	Store *Postgres     // 可靠事件来源
	Redis *redis.Client // 可选通知通道
}

// Publish 在行锁下发布有限事件，Redis 失败仅增加重试次数，不标记成功。
func (o *Outbox) Publish(ctx context.Context) error {
	if o.Redis == nil {
		return nil
	}
	return o.Store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var events []models.OutboxEvent
		if er := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = ?", "pending").Order("created_at").Limit(100).Find(&events).Error; er != nil {
			return er
		}
		for _, ev := range events {
			sendCtx, cancel := context.WithTimeout(ctx, time.Second)
			er := o.Redis.Publish(sendCtx, "run:notify:"+ev.AggregateID, string(ev.Payload)).Err()
			if er == nil {
				er = o.Redis.Publish(sendCtx, "scheduler:wakeup", ev.AggregateID).Err()
			}
			cancel()
			updates := map[string]any{"attempts": ev.Attempts + 1}
			if er == nil {
				updates["status"] = "published"
				updates["published_at"] = time.Now().UTC()
			}
			if dbEr := tx.Model(&models.OutboxEvent{}).Where("id = ?", ev.ID).Updates(updates).Error; dbEr != nil {
				return dbEr
			}
			if er != nil {
				return nil
			}
		}
		return nil
	})
}
