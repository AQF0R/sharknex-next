package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	runexec "autonomous-engine/pkg/runtime"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// stageIndex 一次读取并按来源分组，避免阶段数乘事实数的重复查询。
type stageIndex struct {
	Goals    []models.Goal                        // 稳定阶段序列
	ByKey    map[string]models.Goal               // 阶段依赖索引
	Specs    map[string]contracts.GoalSpec        // 阶段验收定义
	Work     map[string][]models.Task             // 阶段全部工作
	Reviews  map[string]models.Task               // 服务端创建的阶段复核
	Facts    map[string][]models.Fact             // 按来源工作归组的事实
	Evidence map[string][]contracts.StageEvidence // 按来源工作归组的证据
	Scope    contracts.Scope                      // 不可变授权
	Count    int                                  // 包含复核的任务预算使用量
}

// loadStageGate 仅批量读取阶段定义用于调度准入，不读取事实或证据。
func loadStageGate(u Unit, r models.Run) (*stageIndex, error) {
	x := &stageIndex{ByKey: map[string]models.Goal{}, Specs: map[string]contracts.GoalSpec{}}
	if err := u.List(&models.Goal{}, Filter{RunID: r.ID, Limit: 100}, &x.Goals); err != nil {
		return nil, err
	}
	for _, g := range x.Goals {
		s, err := Decode[contracts.GoalSpec](g.SuccessCriteria)
		if err != nil {
			return nil, err
		}
		x.Specs[g.ID] = s
		x.ByKey[s.Key] = g
	}
	return x, nil
}

// loadStages 以O(T+F+E+D)分组持久记录，超大快照由复核明确拒绝。
func loadStages(u Unit, r models.Run) (*stageIndex, error) {
	x := &stageIndex{ByKey: map[string]models.Goal{}, Specs: map[string]contracts.GoalSpec{}, Work: map[string][]models.Task{}, Reviews: map[string]models.Task{}, Facts: map[string][]models.Fact{}, Evidence: map[string][]contracts.StageEvidence{}}
	var err error
	x.Scope, err = scopeOf(r)
	if err != nil {
		return nil, err
	}
	if err := u.List(&models.Goal{}, Filter{RunID: r.ID, Limit: 100}, &x.Goals); err != nil {
		return nil, err
	}
	var tasks []models.Task
	if err := u.List(&models.Task{}, Filter{RunID: r.ID, Limit: 1000}, &tasks); err != nil {
		return nil, err
	}
	x.Count = len(tasks)
	taskGoal := map[string]string{}
	for _, g := range x.Goals {
		s, err := Decode[contracts.GoalSpec](g.SuccessCriteria)
		if err != nil {
			return nil, err
		}
		x.Specs[g.ID] = s
		x.ByKey[s.Key] = g
	}
	for _, t := range tasks {
		if t.GoalID == nil {
			continue
		}
		id := *t.GoalID
		if t.TaskType == "stage_review" {
			x.Reviews[id] = t
			continue
		}
		x.Work[id] = append(x.Work[id], t)
		taskGoal[t.ID] = id
	}
	var facts []models.Fact
	if err := collect(u, &models.Fact{}, Filter{RunID: r.ID}, &facts); err != nil {
		return nil, err
	}
	for _, f := range facts {
		if f.SourceTaskID != nil {
			if id := taskGoal[*f.SourceTaskID]; id != "" {
				x.Facts[id] = append(x.Facts[id], f)
			}
		}
	}
	var evidence []models.Evidence
	if err := collect(u, &models.Evidence{}, Filter{RunID: r.ID}, &evidence); err != nil {
		return nil, err
	}
	for _, e := range evidence {
		if e.TaskID != nil {
			if id := taskGoal[*e.TaskID]; id != "" {
				x.Evidence[id] = append(x.Evidence[id], contracts.StageEvidence{Evidence: e, ContentHash: e.ContentHash})
			}
		}
	}
	return x, nil
}

// snapshot 固定输入摘要，使任何工作、事实、证据变化都使旧复核失效。
func (x *stageIndex) snapshot(id string) contracts.StageReviewSnapshot {
	in := contracts.StageReviewSnapshot{Goal: x.Specs[id], Scope: x.Scope, Tasks: x.Work[id], Facts: x.Facts[id], Evidence: x.Evidence[id], Complete: true}
	if len(in.Tasks) > 1000 || len(in.Facts) > 64000 || len(in.Evidence) > 128000 {
		in.Complete = false
	}
	sort.Slice(in.Tasks, func(i, j int) bool { return in.Tasks[i].ID < in.Tasks[j].ID })
	sort.Slice(in.Facts, func(i, j int) bool { return in.Facts[i].ID < in.Facts[j].ID })
	sort.Slice(in.Evidence, func(i, j int) bool { return in.Evidence[i].Evidence.ID < in.Evidence[j].Evidence.ID })
	in.Digest = security.Hash(string(JSON(in)))
	return in
}

// admitted 只检查前置阶段，不要求同阶段任务等待自身achieved。
func (x *stageIndex) admitted(goalID string) (bool, bool) {
	g, ok := x.Specs[goalID]
	if !ok {
		return false, true
	}
	own := x.ByKey[g.Key]
	if own.Status == "failed" || own.Status == "cancelled" || own.Status == "needs_work" {
		return false, true
	}
	ready := true
	for _, key := range g.DependsOn {
		dep, ok := x.ByKey[key]
		if !ok || dep.Status == "failed" || dep.Status == "cancelled" || dep.Status == "needs_work" {
			return false, true
		}
		if dep.Status != "achieved" {
			ready = false
		}
	}
	return ready, false
}

// setStage 只在真实状态改变时写阶段事件与Outbox。
func (x *stageIndex) setStage(u Unit, r models.Run, g models.Goal, status, reason string) error {
	if g.Status == status {
		return nil
	}
	if err := u.Patch(&g, g.ID, map[string]any{"status": status, "description": reason}); err != nil {
		return err
	}
	g.Status = status
	g.Description = reason
	x.ByKey[x.Specs[g.ID].Key] = g
	return recordEvent(u, r.ID, "stage."+status, nil, nil, map[string]any{"stage_id": g.ID, "goal_id": g.ID, "key": x.Specs[g.ID].Key, "reason": reason})
}

// evaluateStages 只有普通只读复核及服务端重新核验全部通过才能验收阶段。
func evaluateStages(u Unit, r models.Run) error {
	x, err := loadStages(u, r)
	if err != nil {
		return err
	}
	all := true
	achieved := 0
	for _, original := range x.Goals {
		g := x.ByKey[x.Specs[original.ID].Key]
		if g.Status == "achieved" {
			in := x.snapshot(g.ID)
			review := x.Reviews[g.ID]
			var previous struct {
				Review *contracts.StageReviewResult `json:"review"`
			} // 已验收的复核输入
			if err := json.Unmarshal(review.Result, &previous); err != nil {
				return err
			}
			if previous.Review != nil && previous.Review.Passed && previous.Review.Digest == in.Digest && runexec.CheckStage(in).Passed {
				achieved++
				continue
			}
			all = false
			if err := x.setStage(u, r, g, "needs_work", "已验收阶段输入发生变化，旧复核已失效"); err != nil {
				return err
			}
			continue
		}
		if g.Status == "failed" || g.Status == "cancelled" {
			continue
		}
		if g.Status == "needs_work" {
			if err := x.setStage(u, r, g, "failed", g.Description); err != nil {
				return err
			}
			continue
		}
		ready, impossible := x.admitted(g.ID)
		if impossible {
			if err := x.setStage(u, r, g, "failed", "前置阶段未通过验收"); err != nil {
				return err
			}
			continue
		}
		if !ready {
			all = false
			continue
		}
		open := 0
		started := false
		for _, t := range x.Work[g.ID] {
			if !task.Terminal(t.Status) {
				open++
			}
			if t.Status != task.Blocked && t.Status != task.Ready && t.Status != task.Pending {
				started = true
			}
		}
		if open > 0 {
			all = false
			if started && g.Status == "pending" {
				if err := x.setStage(u, r, g, "running", ""); err != nil {
					return err
				}
			}
			continue
		}
		review := x.Reviews[g.ID]
		if review.ID == "" {
			all = false
			if len(x.Work[g.ID]) == 0 || x.Count >= r.MaxTasks {
				if err := x.setStage(u, r, g, "needs_work", "阶段无工作或复核预算不足；不自动付费补充"); err != nil {
					return err
				}
				continue
			}
			in := x.snapshot(g.ID)
			s := contracts.StageReviewInput{TaskSpec: contracts.TaskSpec{Key: "stage-review:" + g.ID, Goal: x.Specs[g.ID].Key, Type: "stage_review", Name: "阶段只读复核", Target: x.Specs[g.ID].Targets[0], Executor: "internal_review", Tools: []string{}, Dependencies: []contracts.DependencySpec{}, Capabilities: []string{"stage_review"}, Priority: 100}, Digest: in.Digest}
			now := time.Now().UTC()
			t := models.Task{ID: newID(), RunID: r.ID, GoalID: &g.ID, TaskType: "stage_review", Name: s.Name, Status: task.Ready, Priority: 100, RequiredCapabilities: JSON(s.Capabilities), Input: JSON(s), Result: JSON(map[string]any{}), CreatedBy: "stage-reviewer", MaxRetries: 1, AvailableAt: now, DeadlineAt: r.DeadlineAt, IdempotencyKey: s.Key, CreatedAt: now, UpdatedAt: now}
			if err := u.Insert(&t); err != nil {
				return err
			}
			x.Count++
			if err := recordEvent(u, r.ID, "task.created", &t.ID, nil, map[string]any{"type": "stage_review", "goal_id": g.ID}); err != nil {
				return err
			}
			if err := x.setStage(u, r, g, "reviewing", ""); err != nil {
				return err
			}
			continue
		}
		if !task.Terminal(review.Status) {
			all = false
			continue
		}
		in := x.snapshot(g.ID)
		check := runexec.CheckStage(in)
		var result struct {
			Review *contracts.StageReviewResult `json:"review"`
		}
		if err := json.Unmarshal(review.Result, &result); err != nil {
			return err
		}
		if review.Status == task.Completed && result.Review != nil && result.Review.Digest == in.Digest && result.Review.Passed && check.Passed {
			if err := x.setStage(u, r, g, "achieved", "只读复核与服务端验收通过"); err != nil {
				return err
			}
			achieved++
		} else {
			all = false
			reason := check.Reason
			if check.Passed {
				reason = "复核失败或输入已变化；不自动重扫或付费重试"
			}
			if err := x.setStage(u, r, g, "needs_work", reason); err != nil {
				return err
			}
		}
	}
	if !all {
		return nil
	}
	status := "failed"
	if achieved == len(x.Goals) && achieved > 0 {
		status = "completed"
	} else if achieved > 0 {
		status = "partially_completed"
	}
	return stopRun(u, r, status)
}

// validReview 复核能力不能经外部TaskSpec或提案获得，只承认服务端创建记录。
func validReview(t models.Task, s contracts.TaskSpec) bool {
	return t.TaskType == "stage_review" && t.CreatedBy == "stage-reviewer" && s.Executor == "internal_review" && s.Type == "stage_review" && len(s.Tools) == 0 && t.GoalID != nil
}

// checkReviewSubmission 复算当前输入并核对创建时与提交时摘要，不信Worker单方通过声明。
func checkReviewSubmission(u Unit, r models.Run, t models.Task, s contracts.Submission) (*contracts.StageReviewResult, error) {
	if len(s.Observations) > 0 || len(s.Proposals) > 0 {
		return nil, ErrInvalid
	}
	sp, err := specOf(t)
	if err != nil {
		return nil, err
	}
	if !validReview(t, sp) {
		return nil, ErrForbidden
	}
	x, err := loadStages(u, r)
	if err != nil {
		return nil, err
	}
	in := x.snapshot(*t.GoalID)
	result := runexec.CheckStage(in)
	var original contracts.StageReviewInput
	if err := json.Unmarshal(t.Input, &original); err != nil {
		return nil, err
	}
	if s.Review == nil || s.Review.Digest != in.Digest || original.Digest != in.Digest || s.Review.Passed != result.Passed {
		result.Passed = false
		result.Reason = "复核输入已变化或提交结论不一致；需要重新创建任务"
	}
	return &result, nil
}

// stageSummaries 由同一快照构建稳定阶段进度，不把复核计入工作进度。
func stageSummaries(r models.Run, goals []models.Goal, tasks []models.Task) ([]contracts.StageSummary, error) {
	out := make([]contracts.StageSummary, 0, len(goals))
	indices := map[string]int{}
	authors := map[string]map[string]bool{}
	reviewers := map[string]string{}
	for _, g := range goals {
		s, err := Decode[contracts.GoalSpec](g.SuccessCriteria)
		if err != nil {
			return nil, err
		}
		v := contracts.StageSummary{ID: g.ID, Key: s.Key, Name: g.Name, Status: g.Status, DependsOn: append([]string{}, s.DependsOn...), Targets: append([]string{}, s.Targets...), RequiredFacts: append([]string{}, s.RequiredFacts...), Reason: g.Description}
		indices[g.ID] = len(out)
		out = append(out, v)
		authors[g.ID] = map[string]bool{}
	}
	for _, t := range tasks {
		if t.GoalID == nil {
			continue
		}
		i, ok := indices[*t.GoalID]
		if !ok {
			continue
		}
		if t.TaskType == "stage_review" {
			out[i].ReviewTaskID = t.ID
			if t.AssignedWorkerID != nil {
				out[i].ReviewerID = *t.AssignedWorkerID
				reviewers[*t.GoalID] = *t.AssignedWorkerID
			}
			continue
		}
		out[i].TotalTasks++
		if t.Status == task.Completed {
			out[i].CompletedTasks++
		}
		if t.AssignedWorkerID != nil {
			authors[*t.GoalID][*t.AssignedWorkerID] = true
		}
	}
	for id, reviewer := range reviewers {
		i := indices[id]
		out[i].ReviewMode = "cross_review"
		if authors[id][reviewer] {
			out[i].ReviewMode = "self_review"
		}
	}
	byKey := map[string]contracts.StageSummary{}
	for _, v := range out {
		byKey[v.Key] = v
	}
	config, err := Decode[workflowConfig](r.Config)
	if err != nil {
		return nil, err
	}
	ordered := make([]contracts.StageSummary, 0, len(out))
	colors := map[string]int{}
	var visit func(string) error
	visit = func(key string) error {
		if colors[key] == 2 {
			return nil
		}
		if colors[key] == 1 {
			return fmt.Errorf("阶段依赖存在环路")
		}
		v, ok := byKey[key]
		if !ok {
			return fmt.Errorf("阶段引用缺失")
		}
		colors[key] = 1
		for _, d := range v.DependsOn {
			if err := visit(d); err != nil {
				return err
			}
		}
		colors[key] = 2
		ordered = append(ordered, v)
		return nil
	}
	for _, g := range config.Goals {
		if err := visit(g.Key); err != nil {
			return nil, err
		}
	}
	for _, v := range out {
		if err := visit(v.Key); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// rejectObjectiveProposals 保留建议审计但不因模型建议自动追加付费任务。
func rejectObjectiveProposals(u Unit, r models.Run) error {
	var proposals []models.TaskProposal
	if err := collect(u, &models.TaskProposal{}, Filter{RunID: r.ID, Status: []string{"pending"}}, &proposals); err != nil {
		return err
	}
	for _, p := range proposals {
		reason := "本次计划预留只读复核，不自动付费补充或扩展阶段；缺口需重建任务"
		if err := u.Patch(&p, p.ID, map[string]any{"status": "rejected", "review_reason": reason, "reviewed_by": "stage-policy", "reviewed_at": time.Now().UTC()}); err != nil {
			return err
		}
		if err := recordEvent(u, r.ID, "proposal.reviewed", p.ParentTaskID, &p.WorkerID, map[string]any{"proposal_id": p.ID, "status": "rejected", "reason": reason}); err != nil {
			return err
		}
	}
	return nil
}

// reviewContext 返回内部只读复核数据，缺少目标绑定时拒绝。
func reviewContext(u Unit, r models.Run, t models.Task) (*contracts.StageReviewSnapshot, error) {
	if t.GoalID == nil {
		return nil, fmt.Errorf("复核缺少阶段")
	}
	x, err := loadStages(u, r)
	if err != nil {
		return nil, err
	}
	in := x.snapshot(*t.GoalID)
	return &in, nil
}
