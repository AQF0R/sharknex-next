package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// jsonDecode 将持久内容解码到明确接收者。
func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }

// Submit 将状态、观察、建议、审计与 Outbox 原子提交，并支持相同结果幂等重放。
func (e *Engine) Submit(ctx context.Context, id, token, status string, s contracts.Submission) error {
	if status != task.Completed && status != task.Failed && status != task.Cancelled {
		return ErrInvalid
	}
	digest := security.Hash(string(JSON(s)) + status)
	return e.Repo.Transaction(ctx, s.RunID, func(u Unit) error {
		r, t, w, l, er := leaseAuth(u, id, s.LeaseProof, token, true)
		if er != nil {
			return er
		}
		if l.Status == "released" {
			if l.SubmissionHash == digest {
				return nil
			}
			return ErrConflict
		}
		var review *contracts.StageReviewResult
		if t.TaskType == "stage_review" {
			review, er = checkReviewSubmission(u, r, t, s)
			if er != nil {
				return er
			}
			if !review.Passed {
				status = task.Failed
			}
			s.Message = review.Reason
		} else if s.Review != nil {
			return ErrInvalid
		}
		if er := e.writeObservations(ctx, u, r, t, w, s.Observations); er != nil {
			return er
		}
		if status == task.Completed && t.TaskType == "verification" {
			if er := confirmVerification(u, r, t, s.Observations); er != nil {
				return er
			}
		}
		if status == task.Completed {
			for _, sp := range s.Proposals {
				if er := insertProposal(u, r, t, w, sp, "Worker 发现后续工作"); er != nil {
					return er
				}
			}
		}
		now := time.Now().UTC()
		next := status
		values := map[string]any{"status": next, "result": JSON(map[string]any{"message": s.Message, "observation_count": len(s.Observations), "review": review}), "finished_at": now}
		if t.TaskType != "stage_review" && status == task.Failed && t.RetryCount < t.MaxRetries {
			next = task.RetryWait
			values["status"] = next
			values["retry_count"] = t.RetryCount + 1
			values["available_at"] = now.Add(time.Duration(t.RetryCount+1) * time.Second)
			values["finished_at"] = nil
		}
		if er := task.Transition(t.Status, next); er != nil {
			return er
		}
		if er := u.Patch(&t, id, values); er != nil {
			return er
		}
		if er := u.Patch(&l, l.ID, map[string]any{"status": "released", "released_at": now, "submission_hash": digest}); er != nil {
			return er
		}
		workerStatus := "available"
		if w.ActiveTasks > 1 {
			workerStatus = "busy"
		}
		if er := u.Patch(&w, w.ID, map[string]any{"active_tasks": w.ActiveTasks - 1, "status": workerStatus}); er != nil {
			return er
		}
		return recordEvent(u, r.ID, "task."+next, &id, &w.ID, map[string]any{"generation": l.Generation})
	})
}

// AppendObservations 仅允许持有效租约的 Worker 在自身任务下追加不可覆盖的观察。
func (e *Engine) AppendObservations(ctx context.Context, id, token string, s contracts.Submission) error {
	return e.Repo.Transaction(ctx, s.RunID, func(u Unit) error {
		r, t, w, _, er := leaseAuth(u, id, s.LeaseProof, token, false)
		if er != nil {
			return er
		}
		return e.writeObservations(ctx, u, r, t, w, s.Observations)
	})
}

// writeObservations 保留独立观察并校验目标，证据仅允许有限文本引用，不接受任意文件路径。
func (e *Engine) writeObservations(ctx context.Context, u Unit, r models.Run, t models.Task, w models.Worker, observations []contracts.Observation) error {
	if len(observations) > 64 {
		return ErrInvalid
	}
	if len(observations) == 0 {
		return nil
	}
	if t.TaskType == "stage_review" {
		return ErrForbidden
	}
	if objectiveRun(r) {
		if t.GoalID == nil {
			return ErrInvalid
		}
		var goal models.Goal
		if err := u.Get(&goal, *t.GoalID); err != nil {
			return err
		}
		if goal.Status == "achieved" || goal.Status == "failed" || goal.Status == "cancelled" || goal.Status == "reviewing" {
			return ErrConflict
		}
	}
	scope, er := scopeOf(r)
	if er != nil {
		return er
	}
	spec, er := specOf(t)
	if er != nil {
		return er
	}
	for _, o := range observations {
		if o.Target != spec.Target || security.ValidateTarget(scope, o.Target) != nil || o.Type == "" || len(o.Content) > 16384 || o.Confidence < 0 || o.Confidence > 1 {
			return ErrInvalid
		}
		now := time.Now().UTC()
		ref := ""
		if o.Kind == "fact" || o.Kind == "evidence" || o.Kind == "finding" {
			if e.Objects == nil {
				return fmt.Errorf("证据对象存储未配置")
			}
			ref, er = e.Objects.Put(ctx, r.ID, []byte(o.Content))
			if er != nil {
				return er
			}
		}
		switch o.Kind {
		case "asset":
			var assets []models.Asset
			if er := u.List(&models.Asset{}, Filter{RunID: r.ID, Limit: 2000}, &assets); er != nil {
				return er
			}
			found := false
			for _, a := range assets {
				if a.AssetType == o.Type && a.AssetKey == o.Target {
					found = true
					if er := u.Patch(&a, a.ID, map[string]any{"last_seen_at": now}); er != nil {
						return er
					}
					break
				}
			}
			if !found {
				a := models.Asset{ID: newID(), RunID: r.ID, AssetType: o.Type, AssetKey: o.Target, Attributes: JSON(map[string]any{"observation": o.Content}), FirstSeenAt: now, LastSeenAt: now, CreatedAt: now}
				if er := u.Insert(&a); er != nil {
					return er
				}
			}
		case "fact":
			f := models.Fact{ID: newID(), RunID: r.ID, FactType: o.Type, Content: JSON(map[string]any{"target": o.Target, "observation": o.Content}), Confidence: o.Confidence, SourceWorkerID: &w.ID, SourceTaskID: &t.ID, Status: "observed", ObservedAt: now, CreatedAt: now}
			if er := u.Insert(&f); er != nil {
				return er
			}
			ev := models.Evidence{ID: newID(), RunID: r.ID, FactID: &f.ID, TaskID: &t.ID, EvidenceType: "observation", ContentRef: ref, ContentHash: security.Hash(o.Content), Metadata: JSON(map[string]any{"size": len(o.Content)}), CreatedAt: now}
			if er := u.Insert(&ev); er != nil {
				return er
			}
			if er := recordEvent(u, r.ID, "fact.created", &t.ID, &w.ID, map[string]any{"fact_id": f.ID, "type": o.Type}); er != nil {
				return er
			}
		case "evidence":
			ev := models.Evidence{ID: newID(), RunID: r.ID, TaskID: &t.ID, EvidenceType: o.Type, ContentRef: ref, ContentHash: security.Hash(o.Content), Metadata: JSON(map[string]any{"size": len(o.Content)}), CreatedAt: now}
			if er := u.Insert(&ev); er != nil {
				return er
			}
		case "finding":
			var existing []models.Finding
			if er := u.List(&models.Finding{}, Filter{RunID: r.ID, Limit: 2000}, &existing); er != nil {
				return er
			}
			duplicate := false
			for _, v := range existing {
				if v.Title == o.Type && v.Description == o.Content && v.SourceTaskID != nil && *v.SourceTaskID == t.ID {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			evidence := models.Evidence{ID: newID(), RunID: r.ID, TaskID: &t.ID, EvidenceType: "finding_observation", ContentRef: ref, ContentHash: security.Hash(o.Content), Metadata: JSON(map[string]any{"size": len(o.Content)}), CreatedAt: now}
			if er := u.Insert(&evidence); er != nil {
				return er
			}
			f := models.Finding{ID: newID(), RunID: r.ID, Title: o.Type, Description: o.Content, Severity: "informational", Status: "unverified", Confidence: o.Confidence, SourceTaskID: &t.ID, EvidenceRefs: JSON([]string{evidence.ID}), CreatedAt: now, UpdatedAt: now}
			if er := u.Insert(&f); er != nil {
				return er
			}
			if er := recordEvent(u, r.ID, "finding.created", &t.ID, &w.ID, map[string]any{"finding_id": f.ID}); er != nil {
				return er
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

// Propose 接收 Worker 建议，不直接创建可执行任务。
func (e *Engine) Propose(ctx context.Context, id, token string, s contracts.Submission) error {
	if len(s.Proposals) == 0 || len(s.Proposals) > 16 {
		return ErrInvalid
	}
	return e.Repo.Transaction(ctx, s.RunID, func(u Unit) error {
		r, t, w, _, er := leaseAuth(u, id, s.LeaseProof, token, false)
		if er != nil {
			return er
		}
		for _, sp := range s.Proposals {
			if er := insertProposal(u, r, t, w, sp, "Worker 提交建议"); er != nil {
				return er
			}
		}
		return nil
	})
}

// insertProposal 保留待审核建议，并将父任务和身份绑定到服务端上下文。
func insertProposal(u Unit, r models.Run, t models.Task, w models.Worker, sp contracts.TaskSpec, reason string) error {
	if t.TaskType == "stage_review" {
		return ErrForbidden
	}
	if objectiveRun(r) {
		if t.GoalID == nil {
			return ErrInvalid
		}
		var goal models.Goal
		if err := u.Get(&goal, *t.GoalID); err != nil {
			return err
		}
		if goal.Status == "achieved" || goal.Status == "reviewing" || goal.Status == "failed" || goal.Status == "cancelled" {
			return ErrConflict
		}
	}
	if er := running(r); er != nil {
		return er
	}
	scope, er := scopeOf(r)
	if er != nil {
		return er
	}
	if er := security.ValidateTask(scope, sp); er != nil {
		return er
	}
	if sp.Target == "" || t.GoalID == nil {
		return ErrInvalid
	}
	var proposals []models.TaskProposal
	if er := u.List(&models.TaskProposal{}, Filter{RunID: r.ID, Limit: 2000}, &proposals); er != nil {
		return er
	}
	if len(proposals) >= r.MaxTasks*4 {
		return ErrConflict
	}
	for _, p := range proposals {
		v, er := Decode[contracts.TaskSpec](p.ProposedTask)
		if er != nil {
			return er
		}
		if v.Key == sp.Key {
			return nil
		}
	}
	p := models.TaskProposal{ID: newID(), RunID: r.ID, WorkerID: w.ID, ParentTaskID: &t.ID, Reason: reason, ProposedTask: JSON(sp), Status: "pending", CreatedAt: time.Now().UTC()}
	if er := u.Insert(&p); er != nil {
		return er
	}
	return recordEvent(u, r.ID, "proposal.created", &t.ID, &w.ID, map[string]any{"proposal_id": p.ID})
}

// RequireRunAccess 校验 Blackboard 查询必须属于 Worker 已授权的 Run。
func (e *Engine) RequireRunAccess(ctx context.Context, runID, workerID, token string) error {
	if runID == "" {
		return fmt.Errorf("%w: 缺少 run_id", ErrInvalid)
	}
	return e.AuthenticateWorker(ctx, runID, workerID, token)
}
