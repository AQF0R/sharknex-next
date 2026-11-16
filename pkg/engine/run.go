package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/planner"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

// newID 使用成熟 UUID 实现生成记录标识。
func newID() string { return uuid.NewString() }

// CreateRun 校验完整任务图和授权快照后，原子创建 Run、Goal 与初始计划。
func (e *Engine) CreateRun(ctx context.Context, s contracts.RunSpec) (models.Run, error) {
	if s.Objective != "" {
		return e.createObjectiveRun(ctx, s)
	}
	if s.Name == "" || s.MaxTasks < 1 || s.MaxTasks > 1000 || s.MaxWorkers < 1 || s.MaxWorkers > 64 || len(s.Goals) == 0 || len(s.Goals) > 100 || !s.Deadline.After(time.Now()) || s.Deadline.After(time.Now().Add(24*time.Hour)) {
		return models.Run{}, ErrInvalid
	}
	if len(s.Scope.Targets) == 0 || len(s.Scope.Targets) > 100 || len(s.Scope.Tools) == 0 {
		return models.Run{}, ErrInvalid
	}
	for _, target := range s.Scope.Targets {
		if er := security.ValidateTarget(s.Scope, target); er != nil {
			return models.Run{}, fmt.Errorf("%w: %s", ErrInvalid, er)
		}
	}
	s.Tasks = planner.Initial(s)
	if len(s.Tasks) > s.MaxTasks {
		return models.Run{}, ErrInvalid
	}
	if er := task.ValidateDAG(s.Tasks); er != nil {
		return models.Run{}, fmt.Errorf("%w: %s", ErrInvalid, er)
	}
	goals := map[string]string{}
	for _, g := range s.Goals {
		if g.Key == "" || g.Name == "" || goals[g.Key] != "" {
			return models.Run{}, ErrInvalid
		}
		goals[g.Key] = newID()
	}
	for _, t := range s.Tasks {
		if t.Executor == "pi" && !e.PiEnabled {
			return models.Run{}, fmt.Errorf("%w: 服务端未启用 Pi", ErrInvalid)
		}
		if goals[t.Goal] == "" {
			return models.Run{}, ErrInvalid
		}
		if er := security.ValidateTask(s.Scope, t); er != nil {
			return models.Run{}, fmt.Errorf("%w: %s", ErrInvalid, er)
		}
	}
	now := time.Now().UTC()
	if s.Source == "" {
		s.Source = "local"
	}
	if s.Mode == "" {
		s.Mode = "normal"
	}
	if s.Mode != "normal" && s.Mode != "benchmark" {
		return models.Run{}, ErrInvalid
	}
	r := models.Run{ID: newID(), Name: s.Name, Source: s.Source, Mode: s.Mode, Status: "created", Scope: JSON(s.Scope), Config: JSON(s), MaxWorkers: s.MaxWorkers, MaxTasks: s.MaxTasks, DeadlineAt: &s.Deadline, CreatedAt: now, UpdatedAt: now}
	er := e.Repo.Transaction(ctx, "", func(u Unit) error {
		if er := u.Insert(&r); er != nil {
			return er
		}
		for _, g := range s.Goals {
			v := models.Goal{ID: goals[g.Key], RunID: r.ID, Name: g.Name, Status: "pending", Priority: 50, SuccessCriteria: JSON(g), CreatedAt: now, UpdatedAt: now}
			if er := u.Insert(&v); er != nil {
				return er
			}
		}
		ids := map[string]string{}
		for _, t := range s.Tasks {
			ids[t.Key] = newID()
		}
		for _, t := range s.Tasks {
			if er := insertTask(u, r, t, ids[t.Key], goals[t.Goal], nil); er != nil {
				return er
			}
		}
		for _, t := range s.Tasks {
			for _, d := range t.Dependencies {
				v := models.TaskDependency{ID: newID(), TaskID: ids[t.Key], DependsOn: ids[d.Key], DependencyType: d.Type, CreatedAt: now}
				if er := u.Insert(&v); er != nil {
					return er
				}
			}
		}
		return recordEvent(u, r.ID, "run.created", nil, nil, map[string]string{"name": r.Name})
	})
	return r, er
}

// insertTask 构造受 Planner 控制的持久任务。
func insertTask(u Unit, r models.Run, s contracts.TaskSpec, id, goalID string, parent *string) error {
	now := time.Now().UTC()
	status := task.Ready
	if len(s.Dependencies) > 0 {
		status = task.Blocked
	}
	t := models.Task{ID: id, RunID: r.ID, GoalID: &goalID, ParentID: parent, TaskType: s.Type, Name: s.Name, Status: status, Priority: s.Priority, RequiredCapabilities: JSON(s.Capabilities), Input: JSON(s), Result: JSON(map[string]any{}), CreatedBy: "planner", MaxRetries: 2, AvailableAt: now, DeadlineAt: r.DeadlineAt, IdempotencyKey: s.Key, CreatedAt: now, UpdatedAt: now}
	// 真实模型失败不自动重复付费，恢复租约仍按 fencing 处理。
	if s.Executor == "pi" {
		t.MaxRetries = 0
	}
	if er := u.Insert(&t); er != nil {
		return er
	}
	return recordEvent(u, r.ID, "task.created", &id, nil, map[string]string{"type": s.Type})
}

// StartRun 只允许已创建且未过期的运行开始执行。
func (e *Engine) StartRun(ctx context.Context, id string) error {
	return e.Repo.Transaction(ctx, id, func(u Unit) error {
		var r models.Run
		if er := u.Get(&r, id); er != nil {
			return er
		}
		if r.Status != "created" || r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
			return ErrConflict
		}
		if objectiveRun(r) {
			return beginPlanning(u, r)
		}
		now := time.Now().UTC()
		if er := u.Patch(&r, id, map[string]any{"status": "running", "started_at": now}); er != nil {
			return er
		}
		var goals []models.Goal
		if er := u.List(&models.Goal{}, Filter{RunID: id, Limit: 100}, &goals); er != nil {
			return er
		}
		for _, g := range goals {
			if er := u.Patch(&g, g.ID, map[string]any{"status": "active"}); er != nil {
				return er
			}
		}
		return recordEvent(u, id, "run.started", nil, nil, map[string]any{})
	})
}

// CancelRun 使所有未结束任务与租约在同一事务中失效。
func (e *Engine) CancelRun(ctx context.Context, id string) error {
	return e.Repo.Transaction(ctx, id, func(u Unit) error {
		var r models.Run
		if er := u.Get(&r, id); er != nil {
			return er
		}
		if r.Status == "cancelled" {
			return nil
		}
		if r.Status != "created" && r.Status != "running" && r.Status != "planning" {
			return ErrConflict
		}
		return stopRun(u, r, "cancelled")
	})
}

// stopRun 释放租约与并发计数，并阻止取消后新增规划。
func stopRun(u Unit, r models.Run, status string) error {
	now := time.Now().UTC()
	var tasks []models.Task
	if er := u.List(&models.Task{}, Filter{RunID: r.ID, Limit: 1000}, &tasks); er != nil {
		return er
	}
	for _, t := range tasks {
		if !task.Terminal(t.Status) {
			if er := u.Patch(&t, t.ID, map[string]any{"status": task.Cancelled, "finished_at": now}); er != nil {
				return er
			}
		}
	}
	var workers []models.Worker
	if er := u.List(&models.Worker{}, Filter{RunID: r.ID, Limit: 1000}, &workers); er != nil {
		return er
	}
	for _, w := range workers {
		var leases []models.TaskLease
		if er := u.List(&models.TaskLease{}, Filter{WorkerID: w.ID, Status: []string{"active"}, Limit: 1000}, &leases); er != nil {
			return er
		}
		for _, l := range leases {
			if er := u.Patch(&l, l.ID, map[string]any{"status": "cancelled", "released_at": now}); er != nil {
				return er
			}
		}
		if er := u.Patch(&w, w.ID, map[string]any{"active_tasks": 0, "status": "draining"}); er != nil {
			return er
		}
	}
	var goals []models.Goal
	if er := u.List(&models.Goal{}, Filter{RunID: r.ID, Limit: 100}, &goals); er != nil {
		return er
	}
	for _, g := range goals {
		if objectiveRun(r) {
			if g.Status == "achieved" || g.Status == "failed" || g.Status == "cancelled" {
				continue
			}
			next := "failed"
			if status == "cancelled" {
				next = "cancelled"
			}
			reason := "运行已终止：" + status
			if err := u.Patch(&g, g.ID, map[string]any{"status": next, "description": reason}); err != nil {
				return err
			}
			if err := recordEvent(u, r.ID, "stage."+next, nil, nil, map[string]any{"stage_id": g.ID, "goal_id": g.ID, "reason": reason}); err != nil {
				return err
			}
		} else if g.Status == "active" || g.Status == "pending" {
			if err := u.Patch(&g, g.ID, map[string]any{"status": "blocked"}); err != nil {
				return err
			}
		}
	}
	if er := u.Patch(&r, r.ID, map[string]any{"status": status, "finished_at": now}); er != nil {
		return er
	}
	return recordEvent(u, r.ID, "run."+status, nil, nil, map[string]any{})
}

// GetRun 读取运行快照。
func (e *Engine) GetRun(ctx context.Context, id string) (models.Run, error) {
	var r models.Run
	er := e.Repo.Read(ctx, func(u Unit) error { return u.Get(&r, id) })
	return r, er
}

// ListResource 提供受运行边界与分页限制的查询，不对外开放任意表名。
func (e *Engine) ListResource(ctx context.Context, id, kind string, after int64, limit int) (any, error) {
	var out any
	er := e.Repo.Read(ctx, func(u Unit) error {
		var r models.Run
		if er := u.Get(&r, id); er != nil {
			return er
		}
		f := Filter{RunID: id, After: after, Limit: limit}
		switch kind {
		case "tasks":
			v := []models.Task{}
			er := u.List(&models.Task{}, f, &v)
			out = v
			return er
		case "goals":
			v := []models.Goal{}
			er := u.List(&models.Goal{}, f, &v)
			out = v
			return er
		case "workers":
			v := []models.Worker{}
			er := u.List(&models.Worker{}, f, &v)
			out = v
			return er
		case "facts":
			v := []models.Fact{}
			er := u.List(&models.Fact{}, f, &v)
			out = v
			return er
		case "assets":
			v := []models.Asset{}
			er := u.List(&models.Asset{}, f, &v)
			out = v
			return er
		case "evidence":
			v := []models.Evidence{}
			er := u.List(&models.Evidence{}, f, &v)
			out = v
			return er
		case "findings":
			v := []models.Finding{}
			er := u.List(&models.Finding{}, f, &v)
			out = v
			return er
		case "proposals":
			v := []models.TaskProposal{}
			er := u.List(&models.TaskProposal{}, f, &v)
			out = v
			return er
		case "events":
			v := []models.ExecutionEvent{}
			er := u.List(&models.ExecutionEvent{}, f, &v)
			out = v
			return er
		default:
			return ErrInvalid
		}
	})
	return out, er
}

// Result 导出有明确版本的完整结果契约，事件通过游标逐批补齐。
func (e *Engine) Result(ctx context.Context, id string) (contracts.RunResult, error) {
	r, er := e.GetRun(ctx, id)
	if er != nil {
		return contracts.RunResult{}, er
	}
	out := contracts.RunResult{Contract: "autonomous-engine.result/v1", Run: r}
	er = e.Repo.Read(ctx, func(u Unit) error {
		f := Filter{RunID: id, Limit: 1000}
		if er := u.List(&models.Goal{}, f, &out.Goals); er != nil {
			return er
		}
		if er := u.List(&models.Task{}, f, &out.Tasks); er != nil {
			return er
		}
		if er := collect(u, &models.Fact{}, Filter{RunID: id}, &out.Facts); er != nil {
			return er
		}
		if er := collect(u, &models.Finding{}, Filter{RunID: id}, &out.Findings); er != nil {
			return er
		}
		for {
			batch := []models.ExecutionEvent{}
			f.Limit = 2000
			if er := u.List(&models.ExecutionEvent{}, f, &batch); er != nil {
				return er
			}
			out.Events = append(out.Events, batch...)
			if len(batch) < 2000 {
				break
			}
			f.After = batch[len(batch)-1].Sequence
		}
		return nil
	})
	if er == nil && objectiveRun(r) {
		out.Stages, er = stageSummaries(r, out.Goals, out.Tasks)
	}
	return out, er
}

// specOf 读取数据库中的不可变任务输入。
func specOf(t models.Task) (contracts.TaskSpec, error) {
	var s contracts.TaskSpec
	er := json.Unmarshal(t.Input, &s)
	return s, er
}

// scopeOf 还原运行授权快照。
func scopeOf(r models.Run) (contracts.Scope, error) { return Decode[contracts.Scope](r.Scope) }

// running 验证运行状态及截止时间。
func running(r models.Run) error {
	if r.Status != "running" || r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
		return ErrConflict
	}
	return nil
}

// validUUID 校验外部引用，避免无效主键传入数据库。
func validUUID(id string) bool { _, er := uuid.Parse(id); return er == nil }

// IsExpected 区分业务冲突与需要记录的基础设施错误。
func IsExpected(er error) bool {
	return errors.Is(er, ErrEmpty) || errors.Is(er, ErrConflict) || errors.Is(er, ErrForbidden) || errors.Is(er, ErrInvalid) || errors.Is(er, ErrNotFound)
}
