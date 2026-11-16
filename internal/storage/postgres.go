package storage

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/engine"
	"context"
	"errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"time"
)

type Postgres struct {
	DB *gorm.DB // 生命周期内共享连接池
}
type unit struct {
	db *gorm.DB // 当前读取会话或事务
}

// Open 创建带连接池和参数脱敏日志的 PostgreSQL 仓库。
func Open(dsn string) (*Postgres, error) {
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.New(discardLog{}, logger.Config{LogLevel: logger.Silent, ParameterizedQueries: true})})
	if e != nil {
		return nil, errors.New("PostgreSQL 连接失败")
	}
	p, e := db.DB()
	if e != nil {
		return nil, e
	}
	p.SetMaxOpenConns(20)
	p.SetMaxIdleConns(5)
	p.SetConnMaxLifetime(30 * time.Minute)
	p.SetConnMaxIdleTime(5 * time.Minute)
	return &Postgres{DB: db}, nil
}

type discardLog struct{}

// Printf 禁止 ORM 将含凭证参数写入默认日志。
func (discardLog) Printf(string, ...any) {}

// Ping 检查唯一事实来源是否可用。
func (p *Postgres) Ping(ctx context.Context) error {
	db, e := p.DB.DB()
	if e != nil {
		return e
	}
	return db.PingContext(ctx)
}

// Close 回收底层数据库连接池。
func (p *Postgres) Close() error {
	db, e := p.DB.DB()
	if e != nil {
		return e
	}
	return db.Close()
}

// Read 创建有请求生命周期的只读仓库会话。
func (p *Postgres) Read(ctx context.Context, fn func(engine.Unit) error) error {
	return fn(&unit{db: p.DB.WithContext(ctx)})
}

// Transaction 先锁 Run，统一所有跨模块写操作的锁顺序和预算检查。
func (p *Postgres) Transaction(ctx context.Context, runID string, fn func(engine.Unit) error) error {
	return p.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if runID != "" {
			var r models.Run
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", runID).Take(&r).Error; e != nil {
				return translate(e)
			}
		}
		return fn(&unit{db: tx})
	})
}

// translate 将基础设施错误映射为稳定领域错误。
func translate(e error) error {
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return engine.ErrNotFound
	}
	if errors.Is(e, gorm.ErrDuplicatedKey) {
		return engine.ErrConflict
	}
	return e
}

// Get 通过参数化 UUID 查询单个已知实体。
func (u *unit) Get(r engine.Record, id string) error {
	return translate(u.db.Where("id = ?", id).Take(r).Error)
}

// List 只开放明确过滤项，避免服务层拼装 SQL。
func (u *unit) List(r engine.Record, f engine.Filter, out any) error {
	q := u.db.Model(r)
	if f.RunID != "" {
		if r.TableName() == "worker_capabilities" {
			q = q.Where("worker_id IN (?)", u.db.Model(&models.Worker{}).Select("id").Where("run_id = ?", f.RunID))
		} else if r.TableName() == "outbox_events" {
			q = q.Where("aggregate_id = ?", f.RunID)
		} else {
			q = q.Where("run_id = ?", f.RunID)
		}
	}
	if f.WorkerID != "" {
		if r.TableName() == "workers" {
			q = q.Where("id = ?", f.WorkerID)
		} else {
			q = q.Where("worker_id = ?", f.WorkerID)
		}
	}
	if f.TaskID != "" {
		q = q.Where("task_id = ?", f.TaskID)
	}
	if len(f.Status) > 0 {
		q = q.Where("status IN ?", f.Status)
	}
	if r.TableName() == "execution_events" {
		q = q.Where("sequence > ?", f.After).Order("sequence ASC")
	} else if r.TableName() == "task_leases" {
		q = q.Order("generation ASC")
	} else if r.TableName() != "worker_capabilities" && r.TableName() != "task_dependencies" && r.TableName() != "workers" {
		if f.Newest {
			q = q.Order("created_at DESC, id DESC")
		} else {
			q = q.Order("created_at ASC, id ASC")
		}
	}
	if r.TableName() == "workers" {
		q = u.db.Model(r)
		if f.RunID != "" {
			q = q.Where("run_id = ?", f.RunID)
		}
		if f.WorkerID != "" {
			q = q.Where("id = ?", f.WorkerID)
		}
		if len(f.Status) > 0 {
			q = q.Where("status IN ?", f.Status)
		}
		q = q.Order("registered_at ASC")
	}
	n := f.Limit
	if n <= 0 || n > 2000 {
		n = 200
	}
	if f.Lock {
		opt := ""
		if f.SkipLocked {
			opt = "SKIP LOCKED"
		}
		q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: opt})
	}
	return translate(q.Offset(f.Offset).Limit(n).Find(out).Error)
}

// Insert 保留 GORM 默认事务与数据库唯一约束。
func (u *unit) Insert(r engine.Record) error { return translate(u.db.Create(r).Error) }

// Patch 仅更新内部代码明确给出的字段与单个实体。
func (u *unit) Patch(r engine.Record, id string, values map[string]any) error {
	q := u.db.Model(r).Where("id = ?", id).Updates(values)
	if q.Error != nil {
		return translate(q.Error)
	}
	if q.RowsAffected != 1 {
		return engine.ErrNotFound
	}
	return nil
}
