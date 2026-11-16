package runtime

import (
	"autonomous-engine/pkg/contracts"
	"context"
	"errors"
	"fmt"
	"time"
)

type TaskExecutor interface {
	Type() string
	Validate(context.Context, *contracts.TaskSpec) error
	Execute(context.Context, *contracts.Snapshot) (*contracts.Submission, error)
	Cancel(context.Context, string) error
}
type Registry struct {
	Executors map[string]TaskExecutor // 初始化后只读的能力注册表
}
type LocalExecutor struct {
	Kind string // 本地执行能力而非 Worker 固定角色
}

// Type 返回执行器匹配的任务类型。
func (l LocalExecutor) Type() string { return l.Kind }

// Validate 拒绝真实网络请求，仅使用受控本地观察来验证协同机制。
func (l LocalExecutor) Validate(ctx context.Context, s *contracts.TaskSpec) error {
	if er := ctx.Err(); er != nil {
		return er
	}
	if s.Type != l.Kind {
		return errors.New("执行器类型不匹配")
	}
	return nil
}

// Execute 在独立快照上产生可追踪的本地观察，不运行任意脚本或网络攻击。
func (l LocalExecutor) Execute(ctx context.Context, in *contracts.Snapshot) (*contracts.Submission, error) {
	s, er := decodeSpec(in.Task.Input)
	if er != nil {
		return nil, er
	}
	timer := time.NewTimer(time.Duration(s.DelayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	out := &contracts.Submission{}
	content := fmt.Sprintf("本地受控执行器 %s 完成目标 %s 的协同验收，读取相关事实 %d 条", l.Kind, s.Target, len(in.Facts))
	kind := "analysis_completed"
	switch l.Kind {
	case "asset_discovery":
		out.Observations = append(out.Observations, contracts.Observation{Kind: "asset", Type: "local_asset", Target: s.Target, Content: content, Confidence: 1})
		kind = "service_identified"
	case "evidence_collection":
		kind = "evidence_collected"
		proposal := s
		proposal.Key = "verify:" + s.Key
		proposal.Type = "verification"
		proposal.Name = "独立证据验证"
		proposal.Capabilities = []string{"verification"}
		proposal.Dependencies = nil
		out.Proposals = append(out.Proposals, proposal)
	case "verification":
		kind = "verification_completed"
	}
	out.Observations = append(out.Observations, contracts.Observation{Kind: "fact", Type: kind, Target: s.Target, Content: content, Confidence: 1})
	return out, nil
}

// Cancel 使用调用方 context 取消协议，不维护跨任务可变状态。
func (LocalExecutor) Cancel(ctx context.Context, _ string) error { return ctx.Err() }

// NewRegistry 构建初始化后不可变的执行器注册表。
func NewRegistry() Registry {
	r := Registry{Executors: map[string]TaskExecutor{}}
	for _, k := range []string{"asset_discovery", "service_analysis", "web_analysis", "evidence_collection", "verification"} {
		r.Executors[k] = LocalExecutor{Kind: k}
	}
	return r
}
