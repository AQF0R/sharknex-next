package migrations

import (
	"context"
	_ "embed"
	"gorm.io/gorm"
)

//go:embed 001_init.sql
var initial string

// Apply 通过事务和 advisory lock 原子执行版本化初始化，不使用生产自动迁移。
func Apply(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(7381024)").Error; e != nil {
			return e
		}
		if e := tx.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())").Error; e != nil {
			return e
		}
		var count int64
		if e := tx.Table("schema_migrations").Where("version = ?", 1).Count(&count).Error; e != nil {
			return e
		}
		if count == 1 {
			return nil
		}
		if e := tx.Exec(initial).Error; e != nil {
			return e
		}
		return tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", 1).Error
	})
}
