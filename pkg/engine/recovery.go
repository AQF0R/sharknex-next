package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/planner"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"context"
	"time"
)

// Tick 从数据库推进恢复、依赖、提案审核与目标评估，Redis 不可用时仍可持续执行。
func (e *Engine) Tick(ctx context.Context) error {
	var runs []models.Run
	if er := e.Repo.Read(ctx, func(u Unit) error {
		return u.List(&models.Run{}, Filter{Status: []string{"running", "planning"}, Limit: 2000}, &runs)
	}); er != nil {
		return er
	}
	for _, r := range runs {
		if er := e.Repo.Transaction(ctx, r.ID, func(u Unit) error {
			if er := u.Get(&r, r.ID); er != nil {
				return er
			}
			if r.Status == "planning" {
				c, err := Decode[workflowConfig](r.Config)
				if err != nil {
					return err
				}
				if c.Planning == nil || !c.Planning.ExpiresAt.After(time.Now()) {
					return failPlanning(u, r, c, "规划超时或中断，请重建任务；未自动重试")
				}
				return nil
			}
			if r.Status != "running" {
				return nil
			}
			if r.DeadlineAt == nil || !r.DeadlineAt.After(time.Now()) {
				return stopRun(u, r, "timeout")
			}
			if er := e.recover(u, r); er != nil {
				return er
			}
			if er := e.advanceDependencies(u, r); er != nil {
				return er
			}
			if objectiveRun(r) {
				if err := rejectObjectiveProposals(u, r); err != nil {
					return err
				}
				return evaluateStages(u, r)
			}
			if er := e.plan(u, r); er != nil {
				return er
			}
			return evaluate(u, r)
		}); er != nil {
			return er
		}
	}
	return nil
}

// recover 失效离线 Worker 和过期租约，递增重试计数而不是复用旧代数。
func (e *Engine) recover(u Unit, r models.Run) error {
	now := time.Now().UTC()
	var workers []models.Worker
	if er := u.List(&models.Worker{}, Filter{RunID: r.ID, Limit: 1000}, &workers); er != nil {
		return er
	}
	for _, w := range workers {
		offline := w.LastHeartbeatAt == nil || now.Sub(*w.LastHeartbeatAt) > e.Offline
		var leases []models.TaskLease
		if er := u.List(&models.TaskLease{}, Filter{WorkerID: w.ID, Status: []string{"active"}, Limit: 1000}, &leases); er != nil {
			return er
		}
		active := w.ActiveTasks
		for _, l := range leases {
			if !offline && l.ExpiresAt.After(now) {
				continue
			}
			var t models.Task
			if er := u.Get(&t, l.TaskID); er != nil {
				return er
			}
			if er := u.Patch(&l, l.ID, map[string]any{"status": "expired", "released_at": now}); er != nil {
				return er
			}
			if t.Status == task.Claimed || t.Status == task.Running {
				next := task.RetryWait
				updates := map[string]any{"retry_count": t.RetryCount + 1, "available_at": now.Add(time.Second), "assigned_worker_id": nil}
				if t.RetryCount >= t.MaxRetries {
					next = task.Timeout
					updates["finished_at"] = now
				}
				updates["status"] = next
				if er := u.Patch(&t, t.ID, updates); er != nil {
					return er
				}
				if er := recordEvent(u, r.ID, "task."+next, &t.ID, &w.ID, map[string]any{"reason": "lease_expired", "generation": l.Generation}); er != nil {
					return er
				}
			}
			if active > 0 {
				active--
			}
		}
		status := w.Status
		if offline && status != "offline" && status != "draining" {
			status = "offline"
			if er := recordEvent(u, r.ID, "worker.offline", nil, &w.ID, map[string]any{}); er != nil {
				return er
			}
		} else if !offline && status == "busy" && active == 0 {
			status = "available"
		}
		if active != w.ActiveTasks || status != w.Status {
			if er := u.Patch(&w, w.ID, map[string]any{"active_tasks": active, "status": status}); er != nil {
				return er
			}
		}
	}
	return nil
}

// advanceDependencies 按已持久化任务输入线性检查依赖，并传播阶段失败而不留下永久blocked。
func (e *Engine) advanceDependencies(u Unit, r models.Run) error {
	var tasks []models.Task
	if err := u.List(&models.Task{}, Filter{RunID: r.ID, Limit: 1000}, &tasks); err != nil {
		return err
	}
	states := map[string]string{}
	for _, t := range tasks {
		states[t.IdempotencyKey] = t.Status
	}
	var gates *stageIndex
	var err error
	if objectiveRun(r) {
		gates, err = loadStageGate(u, r)
		if err != nil {
			return err
		}
	}
	for _, t := range tasks {
		if t.Status != task.Blocked && t.Status != task.Ready && t.Status != task.RetryWait {
			continue
		}
		if t.Status == task.RetryWait && t.AvailableAt.After(time.Now()) {
			continue
		}
		spec, err := specOf(t)
		if err != nil {
			return err
		}
		ready, impossible := true, false
		if gates != nil {
			if t.GoalID == nil {
				ready = false
				impossible = true
			} else {
				ready, impossible = gates.admitted(*t.GoalID)
			}
		}
		for _, d := range spec.Dependencies {
			st, ok := states[d.Key]
			if !ok {
				ready = false
				impossible = true
				continue
			}
			if d.Type == "success" && st != task.Completed {
				ready = false
				if task.Terminal(st) {
					impossible = true
				}
			}
			if d.Type == "completed" && !task.Terminal(st) {
				ready = false
			}
		}
		next := task.Blocked
		if ready {
			next = task.Ready
		}
		if impossible {
			next = task.Failed
		}
		if next == t.Status {
			continue
		}
		values := map[string]any{"status": next}
		if impossible {
			values["finished_at"] = time.Now().UTC()
			values["result"] = JSON(map[string]any{"message": "前置任务或阶段无法通过验收"})
		}
		if err := u.Patch(&t, t.ID, values); err != nil {
			return err
		}
		states[t.IdempotencyKey] = next
		if err := recordEvent(u, r.ID, "task."+next, &t.ID, nil, map[string]any{"reason": "dependency_or_stage"}); err != nil {
			return err
		}
	}
	return nil
}

// plan 先进行廉价规则过滤，再以幂等键执行提案审核及增量规划。
func (e *Engine) plan(u Unit, r models.Run) error {
	var tasks []models.Task
	if er := u.List(&models.Task{}, Filter{RunID: r.ID, Limit: 1000}, &tasks); er != nil {
		return er
	}
	byID := map[string]models.Task{}
	keys := map[string]bool{}
	for _, t := range tasks {
		byID[t.ID] = t
		keys[t.IdempotencyKey] = true
	}
	scope, er := scopeOf(r)
	if er != nil {
		return er
	}
	var proposals []models.TaskProposal
	if er := collect(u, &models.TaskProposal{}, Filter{RunID: r.ID, Status: []string{"pending"}}, &proposals); er != nil {
		return er
	}
	for _, p := range proposals {
		sp, er := Decode[contracts.TaskSpec](p.ProposedTask)
		if er != nil {
			return er
		}
		reason := "符合授权、父任务、能力和预算策略"
		status := "approved"
		parent := models.Task{}
		if p.ParentTaskID != nil {
			parent = byID[*p.ParentTaskID]
		}
		if parent.ID == "" || parent.RunID != r.ID || parent.AssignedWorkerID == nil || *parent.AssignedWorkerID != p.WorkerID || parent.GoalID == nil {
			status = "rejected"
			reason = "父任务或执行者无效"
		}
		if security.ValidateTask(scope, sp) != nil || (sp.Executor == "pi" && !e.PiEnabled) {
			status = "rejected"
			reason = "授权或类型策略不满足"
		}
		if keys[sp.Key] {
			status = "rejected"
			reason = "相同任务已存在"
		}
		if len(tasks) >= r.MaxTasks {
			status = "rejected"
			reason = "任务预算已用尽"
		}
		if len(sp.Dependencies) > 0 {
			status = "rejected"
			reason = "建议不得修改已有任务图"
		}
		if status == "approved" {
			var goal models.Goal
			if er := u.Get(&goal, *parent.GoalID); er != nil {
				return er
			}
			gs, er := Decode[contracts.GoalSpec](goal.SuccessCriteria)
			if er != nil {
				return er
			}
			if sp.Goal != gs.Key {
				status = "rejected"
				reason = "不能更改父任务所属目标"
			}
		}
		if status == "approved" {
			id := newID()
			if er := insertTask(u, r, sp, id, *parent.GoalID, &parent.ID); er != nil {
				return er
			}
			keys[sp.Key] = true
			tasks = append(tasks, models.Task{ID: id})
		}
		now := time.Now().UTC()
		if er := u.Patch(&p, p.ID, map[string]any{"status": status, "review_reason": reason, "reviewed_by": "rule-planner", "reviewed_at": now}); er != nil {
			return er
		}
		if er := recordEvent(u, r.ID, "proposal.reviewed", p.ParentTaskID, &p.WorkerID, map[string]any{"proposal_id": p.ID, "status": status, "reason": reason}); er != nil {
			return er
		}
	}
	var facts []models.Fact
	if er := collect(u, &models.Fact{}, Filter{RunID: r.ID}, &facts); er != nil {
		return er
	}
	for _, f := range facts {
		if f.FactType != "service_identified" || f.SourceTaskID == nil {
			continue
		}
		parent, ok := byID[*f.SourceTaskID]
		if !ok || parent.GoalID == nil {
			continue
		}
		ps, er := specOf(parent)
		if er != nil {
			return er
		}
		sp := planner.Followup(ps)
		if keys[sp.Key] || len(tasks) >= r.MaxTasks {
			continue
		}
		if er := security.ValidateTask(scope, sp); er != nil {
			continue
		}
		id := newID()
		if er := insertTask(u, r, sp, id, *parent.GoalID, &parent.ID); er != nil {
			return er
		}
		keys[sp.Key] = true
		tasks = append(tasks, models.Task{ID: id})
	}
	var findings []models.Finding
	if er := collect(u, &models.Finding{}, Filter{RunID: r.ID, Status: []string{"unverified"}}, &findings); er != nil {
		return er
	}
	for _, f := range findings {
		if f.SourceTaskID == nil {
			continue
		}
		parent, ok := byID[*f.SourceTaskID]
		if !ok || parent.GoalID == nil || parent.Status != task.Completed {
			continue
		}
		sp, er := specOf(parent)
		if er != nil {
			return er
		}
		sp.Key = "verify-finding:" + f.ID
		sp.Type = "verification"
		sp.Name = "独立发现验证"
		sp.Capabilities = []string{"verification"}
		sp.Dependencies = nil
		if keys[sp.Key] || len(tasks) >= r.MaxTasks {
			continue
		}
		if security.ValidateTask(scope, sp) != nil {
			continue
		}
		id := newID()
		if er := insertTask(u, r, sp, id, *parent.GoalID, &parent.ID); er != nil {
			return er
		}
		keys[sp.Key] = true
		tasks = append(tasks, models.Task{ID: id})
	}
	return nil
}

// evaluate 按目标所需事实、失败任务和未验证发现作终止判定。
func evaluate(u Unit, r models.Run) error {
	var tasks []models.Task
	if er := u.List(&models.Task{}, Filter{RunID: r.ID, Limit: 1000}, &tasks); er != nil {
		return er
	}
	var goals []models.Goal
	if er := u.List(&models.Goal{}, Filter{RunID: r.ID, Limit: 100}, &goals); er != nil {
		return er
	}
	var facts []models.Fact
	if er := collect(u, &models.Fact{}, Filter{RunID: r.ID}, &facts); er != nil {
		return er
	}
	var findings []models.Finding
	if er := collect(u, &models.Finding{}, Filter{RunID: r.ID}, &findings); er != nil {
		return er
	}
	all := true
	achieved := 0
	partial := 0
	for _, g := range goals {
		complete, failed, open := 0, 0, 0
		ids := map[string]bool{}
		for _, t := range tasks {
			if t.GoalID == nil || *t.GoalID != g.ID {
				continue
			}
			ids[t.ID] = true
			if t.Status == task.Completed {
				complete++
			} else if task.Terminal(t.Status) {
				failed++
			} else {
				open++
			}
		}
		factTypes := []string{}
		for _, f := range facts {
			if f.SourceTaskID != nil && ids[*f.SourceTaskID] && f.Status != "rejected" && f.Status != "stale" {
				factTypes = append(factTypes, f.FactType)
			}
		}
		unverified := false
		for _, f := range findings {
			if f.SourceTaskID != nil && ids[*f.SourceTaskID] && f.Status == "unverified" {
				unverified = true
			}
		}
		spec, er := Decode[contracts.GoalSpec](g.SuccessCriteria)
		if er != nil {
			return er
		}
		status := planner.GoalStatus(spec.RequiredFacts, factTypes, complete, failed, open, unverified)
		g.Status = status
		if er := u.Patch(&g, g.ID, map[string]any{"status": status}); er != nil {
			return er
		}
		if status == "active" {
			all = false
		}
		if status == "partially_achieved" {
			partial++
		}
		if status == "achieved" {
			achieved++
		}
	}
	if !all {
		return nil
	}
	status := "failed"
	if achieved == len(goals) {
		status = "completed"
	} else if achieved > 0 {
		status = "partially_completed"
	} else if partial > 0 {
		status = "partially_completed"
	}
	now := time.Now().UTC()
	if er := u.Patch(&r, r.ID, map[string]any{"status": status, "finished_at": now}); er != nil {
		return er
	}
	return recordEvent(u, r.ID, "run."+status, nil, nil, map[string]any{"achieved_goals": achieved})
}
