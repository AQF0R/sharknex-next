package planner

import (
	"autonomous-engine/pkg/contracts"
	"slices"
)

// Initial 生成可审计的本地基线计划，不假装接入未配置的 LLM。
func Initial(s contracts.RunSpec) []contracts.TaskSpec {
	if len(s.Tasks) > 0 {
		return s.Tasks
	}
	var out []contracts.TaskSpec
	for i, g := range s.Goals {
		_ = i
		for _, target := range s.Scope.Targets {
			key := g.Key + ":" + target
			out = append(out, contracts.TaskSpec{Key: key, Goal: g.Key, Type: "asset_discovery", Name: "授权资产观察", Target: target, Tools: []string{"local.observe"}, Capabilities: []string{"asset_discovery"}, Priority: 60, DelayMS: 100})
		}
	}
	return out
}

// Followup 为新服务事实提出增量任务，幂等键不依赖事件投递次数。
func Followup(parent contracts.TaskSpec) contracts.TaskSpec {
	parent.Key = "followup:" + parent.Key
	parent.Type = "service_analysis"
	parent.Name = "新服务分析"
	parent.Capabilities = []string{"service_analysis"}
	parent.Dependencies = nil
	return parent
}

// GoalStatus 根据显式事实条件及任务结果判断目标，而非把所有终态当成成功。
func GoalStatus(required []string, facts []string, completed, failed, open int, unverified bool) string {
	if open > 0 {
		return "active"
	}
	if unverified {
		return "blocked"
	}
	for _, r := range required {
		if !slices.Contains(facts, r) {
			if completed > 0 {
				return "partially_achieved"
			}
			return "failed"
		}
	}
	if len(required) > 0 {
		return "achieved"
	}
	if failed == 0 && completed > 0 {
		return "achieved"
	}
	if completed > 0 {
		return "partially_achieved"
	}
	return "failed"
}
