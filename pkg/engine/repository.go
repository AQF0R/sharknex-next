package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/datatypes"
	"time"
)

var (
	ErrInvalid   = errors.New("请求参数无效")
	ErrForbidden = errors.New("无权访问该运行或租约")
	ErrConflict  = errors.New("当前状态不允许此操作")
	ErrNotFound  = errors.New("资源不存在")
	ErrEmpty     = errors.New("暂无可执行任务")
)

type Filter struct {
	RunID      string   // 强制运行边界
	WorkerID   string   // 执行者过滤
	TaskID     string   // 任务关联
	Status     []string // 状态白名单
	After      int64    // 事件恢复游标
	Limit      int      // 有界结果上限
	Offset     int      // 内部分批扫描偏移
	Newest     bool     // 固定倒序运行导航
	Lock       bool     // 行级锁
	SkipLocked bool     // 调度跳过已锁行
}
type Record interface{ TableName() string }
type Unit interface {
	Get(Record, string) error
	List(Record, Filter, any) error
	Insert(Record) error
	Patch(Record, string, map[string]any) error
}
type Repository interface {
	Transaction(context.Context, string, func(Unit) error) error
	Read(context.Context, func(Unit) error) error
	Ping(context.Context) error
}
type ObjectStore interface {
	Put(context.Context, string, []byte) (string, error)
}

// ModelPlanner 隔离真实模型规划能力，领域引擎不依赖 Pi 实现。
type ModelPlanner interface {
	Plan(context.Context, contracts.RunSpec) (*contracts.Plan, error)
}

type Engine struct {
	Planner   ModelPlanner  // 入口装配的真实结构化规划器
	PiEnabled bool          // 是否允许创建真实 Agent 任务
	Objects   ObjectStore   // 按 Run 隔离的证据对象存储
	Repo      Repository    // 持久化边界
	Lease     time.Duration // 默认租期
	Offline   time.Duration // 心跳离线阈值
}

// JSON 将已验证的内部契约序列化为数据库 JSON。
func JSON(v any) datatypes.JSON {
	b, e := json.Marshal(v)
	if e != nil {
		panic("内部 JSON 契约不可序列化")
	}
	return b
}

// Decode 还原持久化契约并显式返回格式错误。
func Decode[T any](b []byte) (T, error) { var v T; e := json.Unmarshal(b, &v); return v, e }

// recordEvent 在状态事务中同时记录轨迹和可靠通知。
func recordEvent(u Unit, runID, kind string, taskID, workerID *string, payload any) error {
	now := time.Now().UTC()
	id := newID()
	ev := models.ExecutionEvent{ID: id, RunID: runID, TaskID: taskID, WorkerID: workerID, EventType: kind, Payload: JSON(payload), CreatedAt: now}
	if e := u.Insert(&ev); e != nil {
		return e
	}
	return u.Insert(&models.OutboxEvent{ID: id, AggregateType: "run", AggregateID: runID, EventType: kind, Payload: JSON(map[string]string{"run_id": runID, "event_id": id, "event_type": kind}), Status: "pending", CreatedAt: now})
}
