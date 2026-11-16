package runtime

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
)

// StageReviewer 只核验共享数据，不调用模型、网络工具或漏洞判断。
type StageReviewer struct{}

// Type 返回仅由服务端创建的复核任务类型。
func (StageReviewer) Type() string { return "stage_review" }

// Validate 防止普通本地执行器被冒充为真实阶段复核。
func (StageReviewer) Validate(ctx context.Context, s *contracts.TaskSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Type != "stage_review" || s.Executor != "internal_review" || len(s.Tools) != 0 {
		return errors.New("内部复核任务无效")
	}
	return nil
}

// Execute 从完整快照计算复核结论，结果仍须服务端复算。
func (StageReviewer) Execute(ctx context.Context, in *contracts.Snapshot) (*contracts.Submission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Review == nil {
		return nil, errors.New("缺少完整阶段快照")
	}
	result := CheckStage(*in.Review)
	return &contracts.Submission{Review: &result, Message: result.Reason}, nil
}

// Cancel 不持有任务外资源，遵循调用方生命周期。
func (StageReviewer) Cancel(ctx context.Context, _ string) error { return ctx.Err() }

// CheckStage 线性核验每个mandatory任务及每目标事实、证据关联和TCP端口覆盖。
func CheckStage(in contracts.StageReviewSnapshot) contracts.StageReviewResult {
	out := contracts.StageReviewResult{Digest: in.Digest}
	if !in.Complete || len(in.Tasks) == 0 || len(in.Goal.Targets) == 0 || len(in.Goal.RequiredFacts) == 0 {
		out.Reason = "复核快照不完整或阶段无可验收工作"
		return out
	}
	byID := map[string]contracts.TaskSpec{}
	workers := map[string]string{}
	for _, t := range in.Tasks {
		if t.Status != task.Completed {
			out.Reason = "阶段存在失败、取消或未完成的必要工作"
			return out
		}
		var s contracts.TaskSpec
		if json.Unmarshal(t.Input, &s) != nil || security.ValidatePlannedTask(in.Scope, s) != nil || s.Goal != in.Goal.Key || !slices.Contains(in.Goal.Targets, s.Target) {
			out.Reason = "阶段工作授权或执行器不可信"
			return out
		}
		byID[t.ID] = s
		if t.AssignedWorkerID != nil {
			workers[t.ID] = *t.AssignedWorkerID
		}
	}
	evidence := map[string][]contracts.StageEvidence{}
	for _, e := range in.Evidence {
		if e.Evidence.FactID != nil {
			evidence[*e.Evidence.FactID] = append(evidence[*e.Evidence.FactID], e)
		}
	}
	taskCovered := map[string]bool{}
	coverage := map[string]map[string]bool{}
	portCoverage := map[string]map[int]bool{}
	for _, f := range in.Facts {
		if f.SourceTaskID == nil || f.SourceWorkerID == nil || f.Status == "rejected" || f.Status == "stale" {
			continue
		}
		id := *f.SourceTaskID
		s, ok := byID[id]
		if !ok || workers[id] == "" || workers[id] != *f.SourceWorkerID {
			continue
		}
		var content struct {
			Target      string `json:"target"`
			Observation string `json:"observation"`
		}
		if json.Unmarshal(f.Content, &content) != nil || content.Target != s.Target || content.Observation == "" {
			continue
		}
		linked := false
		for _, e := range evidence[f.ID] {
			v := e.Evidence
			if v.RunID == f.RunID && v.TaskID != nil && *v.TaskID == id && v.ContentRef != "" && e.ContentHash == security.Hash(content.Observation) {
				linked = true
				break
			}
		}
		if !linked {
			continue
		}
		expected := "analysis_completed"
		if s.Type == "asset_discovery" {
			expected = "services_discovered"
		}
		if f.FactType != expected {
			continue
		}
		if expected == "analysis_completed" {
			var report struct {
				Status int `json:"status"`
			}
			if json.Unmarshal([]byte(content.Observation), &report) != nil || report.Status < 100 || report.Status > 599 {
				continue
			}
		} else {
			ports, ok := completePorts(s, content.Observation)
			if !ok {
				continue
			}
			if portCoverage[s.Target] == nil {
				portCoverage[s.Target] = map[int]bool{}
			}
			for _, p := range ports {
				portCoverage[s.Target][p] = true
			}
		}
		taskCovered[id] = true
		if coverage[s.Target] == nil {
			coverage[s.Target] = map[string]bool{}
		}
		coverage[s.Target][expected] = true
	}
	for id := range byID {
		if !taskCovered[id] {
			out.Reason = "必要任务缺少对应真实事实及完整关联证据；不自动重扫"
			return out
		}
	}
	for _, target := range in.Goal.Targets {
		for _, kind := range in.Goal.RequiredFacts {
			if !coverage[target][kind] {
				out.Reason = "阶段目标缺少所需事实及证据覆盖"
				return out
			}
		}
		if slices.Contains(in.Goal.RequiredFacts, "services_discovered") {
			for _, p := range in.Scope.TCPPorts {
				if !portCoverage[target][p] {
					out.Reason = "阶段未完整覆盖全部授权端口"
					return out
				}
			}
		}
	}
	out.Passed = true
	out.Reason = "只读复核通过：工作完成且逐目标事实、证据和覆盖一致；不代表漏洞判定"
	return out
}

// completePorts 核验原始TCP报告中实际观察全集，partial或缺端口均不能通过。
func completePorts(s contracts.TaskSpec, raw string) ([]int, bool) {
	var report struct {
		IP        string `json:"ip"`              // 实际报告目标
		Requested []int  `json:"requested_ports"` // 报告声明端口
		Complete  bool   `json:"complete"`        // 是否完成观察
		Ports     []struct {
			Port  int    `json:"port"`
			State string `json:"state"`
		} `json:"ports"` // 各端口实际状态
	}
	u, err := url.Parse(s.Target)
	if err != nil || json.Unmarshal([]byte(raw), &report) != nil || !report.Complete || report.IP != u.Hostname() || len(report.Requested) != len(s.Ports) || len(report.Ports) != len(s.Ports) {
		return nil, false
	}
	requested := map[int]bool{}
	observed := map[int]bool{}
	for _, p := range report.Requested {
		if requested[p] || !slices.Contains(s.Ports, p) {
			return nil, false
		}
		requested[p] = true
	}
	for _, p := range report.Ports {
		if observed[p.Port] || !requested[p.Port] || !slices.Contains([]string{"open", "closed", "timeout", "error"}, p.State) {
			return nil, false
		}
		observed[p.Port] = true
	}
	return report.Requested, true
}
