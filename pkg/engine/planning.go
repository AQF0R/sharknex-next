package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/planner"
	"autonomous-engine/pkg/security"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// workflowConfig 复用Run.Config保存单次规划代际，不新增数据库结构。
type workflowConfig struct {
	contracts.RunSpec                // 原始授权及任务描述
	Summary           string         `json:"summary,omitempty"`  // 已校验的计划摘要
	Planning          *planningState `json:"planning,omitempty"` // 持久化单次调用状态
}
type planningState struct {
	Nonce     string    `json:"nonce"`            // 防止迟到计划提交的代际标识
	Started   bool      `json:"started"`          // 已开始调用则恢复时不重复付费
	ExpiresAt time.Time `json:"expires_at"`       // 有界规划期限
	Reason    string    `json:"reason,omitempty"` // 明确的失败原因
}

// objectiveRun 区分新阶段化工作流与旧显式任务路径。
func objectiveRun(r models.Run) bool {
	c, err := Decode[workflowConfig](r.Config)
	return err == nil && c.Objective != ""
}

// createObjectiveRun 仅保存授权和目标描述，不预设任务或模拟计划。
func (e *Engine) createObjectiveRun(ctx context.Context, s contracts.RunSpec) (models.Run, error) {
	s.Objective = strings.TrimSpace(s.Objective)
	if s.Objective == "" || utf8.RuneCountInString(s.Objective) > 4000 || len(s.Tasks) > 0 || len(s.Goals) > 0 {
		return models.Run{}, fmt.Errorf("%w: objective不得混用显式goals/tasks", ErrInvalid)
	}
	if !e.PiEnabled || e.Planner == nil {
		return models.Run{}, fmt.Errorf("%w: 服务端未启用真实Pi规划器，不能模拟执行", ErrInvalid)
	}
	if s.Name == "" || utf8.RuneCountInString(s.Name) > 255 || s.MaxTasks < 2 || s.MaxTasks > 1000 || s.MaxWorkers < 1 || s.MaxWorkers > 64 || !s.Deadline.After(time.Now()) || s.Deadline.After(time.Now().Add(24*time.Hour)) {
		return models.Run{}, ErrInvalid
	}
	scope, err := security.NormalizeObjectiveScope(s.Scope)
	if err != nil {
		return models.Run{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	s.Scope = scope
	if s.Source == "" {
		s.Source = "objective"
	}
	if s.Mode == "" {
		s.Mode = "normal"
	}
	if s.Mode != "normal" && s.Mode != "benchmark" {
		return models.Run{}, ErrInvalid
	}
	now := time.Now().UTC()
	r := models.Run{ID: newID(), Name: s.Name, Source: s.Source, Mode: s.Mode, Status: "created", Scope: JSON(scope), Config: JSON(workflowConfig{RunSpec: s}), MaxWorkers: s.MaxWorkers, MaxTasks: s.MaxTasks, DeadlineAt: &s.Deadline, CreatedAt: now, UpdatedAt: now}
	err = e.Repo.Transaction(ctx, "", func(u Unit) error {
		if err := u.Insert(&r); err != nil {
			return err
		}
		return recordEvent(u, r.ID, "run.created", nil, nil, map[string]any{"name": r.Name, "workflow": "objective"})
	})
	return r, err
}

// beginPlanning 在启动事务内固定代际和期限，模型调用由运行时异步处理。
func beginPlanning(u Unit, r models.Run) error {
	c, err := Decode[workflowConfig](r.Config)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	expires := now.Add(90 * time.Second)
	if r.DeadlineAt.Before(expires) {
		expires = *r.DeadlineAt
	}
	c.Planning = &planningState{Nonce: newID(), ExpiresAt: expires}
	if err := u.Patch(&r, r.ID, map[string]any{"status": "planning", "started_at": now, "config": JSON(c)}); err != nil {
		return err
	}
	return recordEvent(u, r.ID, "run.planning", nil, nil, map[string]any{"nonce": c.Planning.Nonce})
}

// ExecutePlanning 在事务外最多调用模型一次，提交时重新检查状态、代际及截止时间。
func (e *Engine) ExecutePlanning(ctx context.Context, id string) error {
	var c workflowConfig
	acquired := false
	err := e.Repo.Transaction(ctx, id, func(u Unit) error {
		var r models.Run
		if err := u.Get(&r, id); err != nil {
			return err
		}
		if r.Status != "planning" {
			return nil
		}
		var err error
		c, err = Decode[workflowConfig](r.Config)
		if err != nil {
			return err
		}
		if c.Planning == nil {
			return failPlanning(u, r, c, "规划状态损坏，请重建任务")
		}
		if c.Planning.Started {
			return nil
		}
		if !c.Planning.ExpiresAt.After(time.Now()) || r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
			return failPlanning(u, r, c, "规划截止时间已到，请重建任务")
		}
		if e.Planner == nil || !e.PiEnabled {
			return failPlanning(u, r, c, "真实Pi规划器不可用")
		}
		c.Planning.Started = true
		acquired = true
		if err := u.Patch(&r, id, map[string]any{"config": JSON(c)}); err != nil {
			return err
		}
		return recordEvent(u, id, "planner.started", nil, nil, map[string]any{"nonce": c.Planning.Nonce})
	})
	if err != nil || !acquired {
		return err
	}
	callCtx, cancel := context.WithDeadline(ctx, c.Planning.ExpiresAt)
	defer cancel()
	// 数据库取消和代际失效必须传递到模型进程，不能只在迟到提交时拦截。
	watchCtx, stopWatch := context.WithCancel(callCtx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		e.watchPlanning(watchCtx, id, c.Planning.Nonce, cancel)
	}()
	defer func() { stopWatch(); <-watchDone }()
	plan, planErr := e.Planner.Plan(callCtx, c.RunSpec)
	// 正常完成也回收监视器及在途查询；先停止并等待，再判断模型调用的取消状态。
	stopWatch()
	<-watchDone
	if planErr == nil {
		planErr = callCtx.Err()
	}
	reason := ""
	if planErr != nil {
		reason = "真实模型规划失败或中断，请重建任务；未自动重试"
	} else if err := planner.ValidatePlan(c.RunSpec, plan); err != nil {
		reason = "计划校验失败：" + err.Error()
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	return e.Repo.Transaction(finishCtx, id, func(u Unit) error {
		var r models.Run
		if err := u.Get(&r, id); err != nil {
			return err
		}
		current, err := Decode[workflowConfig](r.Config)
		if err != nil {
			return err
		}
		if r.Status != "planning" || current.Planning == nil || current.Planning.Nonce != c.Planning.Nonce {
			return nil
		}
		if !current.Planning.ExpiresAt.After(time.Now()) || r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
			reason = "规划超时，请重建任务；未自动重试"
		}
		if reason != "" {
			return failPlanning(u, r, current, reason)
		}
		effective, err := security.ResolvePlanScope(current.Scope, plan.Tasks)
		if err != nil {
			return failPlanning(u, r, current, "计划校验失败："+err.Error())
		}
		r.Scope = JSON(effective)
		current.Scope = effective
		if err := persistPlan(u, r, plan); err != nil {
			return err
		}
		current.Summary = plan.Summary
		current.Goals = plan.Goals
		current.Tasks = plan.Tasks
		if err := u.Patch(&r, id, map[string]any{"status": "running", "scope": JSON(effective), "config": JSON(current)}); err != nil {
			return err
		}
		if err := recordEvent(u, id, "planner.completed", nil, nil, map[string]any{"usage": plan.Usage, "nonce": c.Planning.Nonce}); err != nil {
			return err
		}
		if err := recordEvent(u, id, "planned", nil, nil, map[string]any{"summary": plan.Summary, "stages": len(plan.Goals), "tasks": len(plan.Tasks)}); err != nil {
			return err
		}
		return recordEvent(u, id, "run.started", nil, nil, map[string]any{})
	})
}

// watchPlanning 短轮询数据库授权状态并取消模型，查询有界且不占用写事务或Run锁。
func (e *Engine) watchPlanning(ctx context.Context, id, nonce string, cancel context.CancelFunc) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		readCtx, done := context.WithTimeout(ctx, time.Second)
		r, err := e.GetRun(readCtx, id)
		done()
		// 正常模型返回触发stopWatch时，不把监视查询的取消误判成规划失败。
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// 无法确认运行仍获授权时保守中断，不允许失联后继续付费规划。
			cancel()
			return
		}
		c, err := Decode[workflowConfig](r.Config)
		if err != nil || r.Status != "planning" || c.Planning == nil || c.Planning.Nonce != nonce || !c.Planning.Started || !c.Planning.ExpiresAt.After(time.Now()) || r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// failPlanning 将失败持久化并明确禁止隐式模型重试。
func failPlanning(u Unit, r models.Run, c workflowConfig, reason string) error {
	if c.Planning == nil {
		c.Planning = &planningState{}
	}
	c.Planning.Reason = reason
	if err := u.Patch(&r, r.ID, map[string]any{"status": "failed", "finished_at": time.Now().UTC(), "config": JSON(c)}); err != nil {
		return err
	}
	if err := recordEvent(u, r.ID, "planner.failed", nil, nil, map[string]any{"reason": reason}); err != nil {
		return err
	}
	return recordEvent(u, r.ID, "run.failed", nil, nil, map[string]any{"reason": reason})
}

// RecoverPlanning 对重启时已发起的模型调用明确失败，不重复产生付费调用。
func (e *Engine) RecoverPlanning(ctx context.Context, id string) error {
	return e.Repo.Transaction(ctx, id, func(u Unit) error {
		var r models.Run
		if err := u.Get(&r, id); err != nil {
			return err
		}
		if r.Status != "planning" {
			return nil
		}
		c, err := Decode[workflowConfig](r.Config)
		if err != nil {
			return err
		}
		if c.Planning == nil || c.Planning.Started {
			return failPlanning(u, r, c, "规划中断，请重建任务；未自动重试")
		}
		return nil
	})
}

// persistPlan 在同一Run事务写入全部阶段、工作任务及依赖后才能启用调度。
func persistPlan(u Unit, r models.Run, p *contracts.Plan) error {
	goals := map[string]string{}
	ids := map[string]string{}
	now := time.Now().UTC()
	for _, g := range p.Goals {
		id := newID()
		goals[g.Key] = id
		v := models.Goal{ID: id, RunID: r.ID, Name: g.Name, Status: "pending", Priority: 50, SuccessCriteria: JSON(g), CreatedAt: now, UpdatedAt: now}
		if err := u.Insert(&v); err != nil {
			return err
		}
	}
	for _, t := range p.Tasks {
		ids[t.Key] = newID()
	}
	for _, t := range p.Tasks {
		if err := insertTask(u, r, t, ids[t.Key], goals[t.Goal], nil); err != nil {
			return err
		}
	}
	for _, t := range p.Tasks {
		for _, d := range t.Dependencies {
			v := models.TaskDependency{ID: newID(), TaskID: ids[t.Key], DependsOn: ids[d.Key], DependencyType: d.Type, CreatedAt: now}
			if err := u.Insert(&v); err != nil {
				return err
			}
		}
	}
	return nil
}
