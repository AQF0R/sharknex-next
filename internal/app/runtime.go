package app

import (
	"autonomous-engine/internal/config"
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/engine"
	"autonomous-engine/pkg/pi"
	runexec "autonomous-engine/pkg/runtime"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Runtime struct {
	Engine   *engine.Engine           // 服务边界
	Config   config.Config            // 唯一最终配置
	Pi       *pi.Executor             // 独立于任务类型的 Pi 适配器
	Registry runexec.Registry         // 共享只读执行器
	ctx      context.Context          // 进程根生命周期
	wg       sync.WaitGroup           // 等待全部逻辑 Worker 退出
	mu       sync.Mutex               // 保护注册与唤醒集合
	runs     map[string]bool          // 防止重复启动同一 Run
	wake     map[string]chan struct{} // 有界唤醒通知
}

// NewRuntime 创建受进程生命周期控制的 Shared Runtime。
func NewRuntime(ctx context.Context, e *engine.Engine, c config.Config) *Runtime {
	return &Runtime{Engine: e, Config: c, Registry: runexec.NewRegistry(), Pi: pi.New(c.Pi), ctx: ctx, runs: map[string]bool{}, wake: map[string]chan struct{}{}}
}

// Start 先注册固定数量逻辑 Worker，每个具有多种能力且上下文互相独立。
func (r *Runtime) Start(runID string) error {
	run, err := r.Engine.GetRun(r.ctx, runID)
	if err != nil {
		return err
	}
	if run.Status == "planning" {
		r.mu.Lock()
		if r.runs[runID] {
			r.mu.Unlock()
			return nil
		}
		r.runs[runID] = true
		r.wg.Add(1)
		r.mu.Unlock()
		go func() {
			defer r.wg.Done()
			if err := r.Engine.ExecutePlanning(r.ctx, runID); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("规划提交失败", "run_id", runID, "error", err)
			}
			r.mu.Lock()
			delete(r.runs, runID)
			r.mu.Unlock()
			current, readErr := r.Engine.GetRun(r.ctx, runID)
			if r.ctx.Err() == nil && readErr == nil && current.Status == "running" {
				if err := r.Start(runID); err != nil && !engine.IsExpected(err) {
					slog.Error("规划后启动失败", "error", err)
				}
			}
		}()
		return nil
	}
	if run.Status != "running" {
		return nil
	}
	r.mu.Lock()
	if r.runs[runID] {
		r.mu.Unlock()
		return nil
	}
	r.runs[runID] = true
	r.mu.Unlock()
	identities := []contracts.WorkerIdentity{}
	for i := 0; i < r.Config.Workers; i++ {
		identity, er := r.Engine.RegisterWorker(r.ctx, contracts.WorkerSpec{RunID: runID, Name: "local-" + security.Hash(runID)[:8] + "-" + string(rune('A'+i)), Capabilities: []string{"asset_discovery", "service_analysis", "web_analysis", "evidence_collection", "verification", "stage_review"}, MaxConcurrency: 1})
		if er != nil {
			r.mu.Lock()
			delete(r.runs, runID)
			r.mu.Unlock()
			return er
		}
		identities = append(identities, identity)
	}
	for _, identity := range identities {
		wake := make(chan struct{}, 1)
		r.mu.Lock()
		r.wake[identity.Worker.ID] = wake
		r.mu.Unlock()
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() { r.mu.Lock(); delete(r.wake, identity.Worker.ID); r.mu.Unlock() }()
			r.worker(identity, wake)
		}()
	}
	return nil
}

// Resume 为重启时仍处于运行态的 Run 恢复逻辑 Worker，旧租约由恢复器回收。
func (r *Runtime) Resume() error {
	var runs []models.Run
	if er := r.Engine.Repo.Read(r.ctx, func(u engine.Unit) error {
		return u.List(&models.Run{}, engine.Filter{Status: []string{"running", "planning"}, Limit: 2000}, &runs)
	}); er != nil {
		return er
	}
	for _, run := range runs {
		if run.Status == "planning" {
			if err := r.Engine.RecoverPlanning(r.ctx, run.ID); err != nil {
				return err
			}
		}
		if er := r.Start(run.ID); er != nil {
			return er
		}
	}
	return nil
}

// Wake 只发送可合并的通知，任务所有权仍由数据库决定。
func (r *Runtime) Wake() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.wake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Wait 等待所有执行 context 和逻辑 Worker 回收。
func (r *Runtime) Wait() { r.wg.Wait() }

// worker 主动拉取任务，心跳独立于任务执行，取消后退出而不继续认领。
func (r *Runtime) worker(w contracts.WorkerIdentity, wake <-chan struct{}) {
	heartbeat := time.NewTicker(r.Config.Heartbeat)
	defer heartbeat.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		if er := r.ctx.Err(); er != nil {
			return
		}
		run, er := r.Engine.GetRun(r.ctx, w.Worker.RunID)
		if er != nil {
			slog.Error("读取运行失败", "error", er)
			return
		}
		if run.Status != "running" {
			return
		}
		if er := r.Engine.Heartbeat(r.ctx, w.Worker.RunID, w.Worker.ID, w.Token); er != nil {
			if engine.IsExpected(er) {
				return
			}
			slog.Warn("认领前心跳失败", "error", er)
		}
		claim, er := r.Engine.ClaimTask(r.ctx, w.Worker.RunID, w.Worker.ID, w.Token)
		if er == nil {
			r.execute(w, claim)
			continue
		}
		if !engine.IsExpected(er) {
			slog.Error("认领失败", "error", er)
		}
		select {
		case <-r.ctx.Done():
			return
		case <-heartbeat.C:
			if er := r.Engine.Heartbeat(r.ctx, w.Worker.RunID, w.Worker.ID, w.Token); er != nil {
				slog.Warn("Worker 心跳失败", "error", er)
				if engine.IsExpected(er) {
					return
				}
			}
		case <-poll.C:
		case <-wake:
		}
	}
}

// execute 管理独立任务上下文、工作目录、续租、取消和结果收集。
func (r *Runtime) execute(w contracts.WorkerIdentity, c contracts.Claim) {
	ctx, cancel := context.WithTimeout(r.ctx, 45*time.Second)
	defer cancel()
	if er := r.Engine.RenewLease(ctx, c.Task.ID, c.Proof, w.Token); er != nil {
		slog.Warn("启动租约失败", "error", er)
		return
	}
	snapshot, er := r.Engine.Snapshot(ctx, c.Task.ID, c.Proof, w.Token)
	if er != nil {
		slog.Error("读取任务快照失败", "error", er)
		return
	}
	spec, er := engine.Decode[contracts.TaskSpec](c.Task.Input)
	if er != nil {
		slog.Error("任务输入损坏", "error", er)
		return
	}
	scopeRun, er := r.Engine.GetRun(ctx, c.Proof.RunID)
	if er != nil {
		slog.Error("读取授权失败", "error", er)
		return
	}
	scope, er := engine.Decode[contracts.Scope](scopeRun.Scope)
	if er != nil {
		slog.Error("授权快照损坏", "error", er)
		return
	}
	executor, ok := r.Registry.Executors[c.Task.TaskType]
	if spec.Executor == "pi" {
		executor = r.Pi
		ok = r.Config.Pi.Enabled
	}
	if spec.Type == "stage_review" && spec.Executor == "internal_review" && c.Task.CreatedBy == "stage-reviewer" {
		executor = runexec.StageReviewer{}
		ok = true
	}
	if !ok || (spec.Type != "stage_review" && security.ValidateTask(scope, spec) != nil) {
		slog.Error("执行授权或执行器无效")
		return
	}
	if er := executor.Validate(ctx, &spec); er != nil {
		slog.Error("任务验证失败", "error", er)
		return
	}
	dir := filepath.Join(r.Config.Workspace, c.Proof.RunID, w.Worker.ID, c.Task.ID, fmt.Sprint(c.Proof.Generation))
	if er := os.MkdirAll(dir, 0700); er != nil {
		slog.Error("工作目录创建失败", "error", er)
		return
	}
	snapshot.Workspace = dir
	snapshot.Audit = func(callCtx context.Context, kind string, payload map[string]any) error {
		if kind == "tool.called" {
			tool, ok := payload["tool"].(string)
			if !ok {
				return engine.ErrInvalid
			}
			return r.Engine.RecordTool(callCtx, c.Task.ID, w.Token, tool, c.Proof)
		}
		return r.Engine.RecordAgent(callCtx, c.Task.ID, w.Token, kind, c.Proof, payload)
	}
	if spec.Executor == "pi" {
		absolute, er := pi.CheckWorkspace(dir)
		if er != nil {
			slog.Error("Pi 工作目录无效")
			return
		}
		snapshot.Workspace = absolute
	}
	if spec.Executor != "pi" && spec.Executor != "internal_review" {
		if er := r.Engine.RecordTool(ctx, c.Task.ID, w.Token, "local.observe", c.Proof); er != nil {
			slog.Error("工具审计失败", "error", er)
			return
		}
	}
	type execution struct {
		result *contracts.Submission // 独立执行结果
		err    error                 // 执行错误
	}
	done := make(chan execution, 1)
	go func() { result, err := executor.Execute(ctx, &snapshot); done <- execution{result: result, err: err} }()
	renew := time.NewTicker(r.Config.Renew)
	defer renew.Stop()
	heartbeat := time.NewTicker(r.Config.Heartbeat)
	defer heartbeat.Stop()
	check := time.NewTicker(250 * time.Millisecond)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			<-done
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && r.ctx.Err() == nil {
				finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
				if er := r.Engine.Submit(finishCtx, c.Task.ID, w.Token, task.Failed, contracts.Submission{LeaseProof: c.Proof, Message: "任务执行超时"}); er != nil && !engine.IsExpected(er) {
					slog.Warn("提交超时结果失败", "error", er)
				}
				finishCancel()
			}
			if er := executor.Cancel(context.Background(), c.Task.ID); er != nil {
				slog.Warn("执行器取消失败", "error", er)
			}
			return
		case <-heartbeat.C:
			if er := r.Engine.Heartbeat(ctx, c.Proof.RunID, w.Worker.ID, w.Token); er != nil {
				slog.Warn("执行中 Worker 心跳失败", "error", er)
				cancel()
			}
		case <-renew.C:
			if er := r.Engine.RenewLease(ctx, c.Task.ID, c.Proof, w.Token); er != nil {
				slog.Warn("任务续租失败", "error", er)
				cancel()
			}
		case <-check.C:
			run, er := r.Engine.GetRun(ctx, c.Proof.RunID)
			if er != nil || run.Status != "running" || run.DeadlineAt == nil || !run.DeadlineAt.After(time.Now()) {
				cancel()
			}
		case result := <-done:
			status := task.Completed
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) {
					return
				}
				status = task.Failed
				result.result = &contracts.Submission{Message: "受控执行失败"}
				if spec.Executor == "pi" {
					result.result.Message = pi.FailureMessage(result.err)
				}
			}
			if result.result == nil {
				slog.Error("执行器返回空结果")
				return
			}
			result.result.LeaseProof = c.Proof
			if er := r.Engine.Submit(ctx, c.Task.ID, w.Token, status, *result.result); er != nil {
				slog.Warn("提交执行结果失败", "error", er)
			}
			r.Wake()
			return
		}
	}
}
