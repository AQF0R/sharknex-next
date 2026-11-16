package planner

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/task"
	"fmt"
	"slices"
	"strings"
)

// ValidatePlan 在写入前验证真实工具、阶段覆盖、双层DAG与复核预算。
func ValidatePlan(s contracts.RunSpec, p *contracts.Plan) error {
	if p == nil || strings.TrimSpace(p.Summary) == "" || len(p.Summary) > 12000 || len(p.Goals) == 0 || len(p.Goals) > 100 || len(p.Tasks) == 0 || len(p.Tasks)+len(p.Goals) > s.MaxTasks {
		return fmt.Errorf("计划为空、过大或未预留每阶段一次复核预算")
	}
	effective, err := security.ResolvePlanScope(s.Scope, p.Tasks)
	if err != nil { return err }
	s.Scope = effective
	if err := task.ValidateDAG(p.Tasks); err != nil {
		return err
	}
	goals := map[string]contracts.GoalSpec{}
	graph := []contracts.TaskSpec{}
	for _, g := range p.Goals {
		if g.Key == "" || len(g.Key) > 100 || g.Name == "" || len(g.Name) > 255 || goals[g.Key].Key != "" || len(g.Targets) == 0 || len(g.RequiredFacts) == 0 || len(g.RequiredFacts) > 32 || len(g.DependsOn) > 100 {
			return fmt.Errorf("阶段键、目标覆盖或验收事实无效")
		}
		seen := map[string]bool{}
		for _, target := range g.Targets {
			if seen[target] || security.ValidateTarget(s.Scope, target) != nil {
				return fmt.Errorf("阶段目标越界或重复")
			}
			seen[target] = true
		}
		facts := map[string]bool{}
		for _, kind := range g.RequiredFacts {
			if facts[kind] || (kind != "analysis_completed" && kind != "services_discovered") { return fmt.Errorf("阶段所需事实重复或不受真实工具支持") }
			facts[kind] = true
		}
		node := contracts.TaskSpec{Key: g.Key}
		seen = map[string]bool{}
		for _, d := range g.DependsOn {
			if seen[d] {
				return fmt.Errorf("阶段依赖重复")
			}
			seen[d] = true
			node.Dependencies = append(node.Dependencies, contracts.DependencySpec{Key: d, Type: "success"})
		}
		graph = append(graph, node)
		goals[g.Key] = g
	}
	if err := task.ValidateDAG(graph); err != nil {
		return fmt.Errorf("阶段图无效: %w", err)
	}
	byKey := map[string]contracts.TaskSpec{}
	covered := map[string]map[string]map[string]bool{}
	ports := map[string]map[string]map[int]bool{}
	allTargets := map[string]bool{}
	for _, t := range p.Tasks {
		if err := security.ValidatePlannedTask(s.Scope, t); err != nil {
			return err
		}
		g, ok := goals[t.Goal]
		if !ok || !slices.Contains(g.Targets, t.Target) {
			return fmt.Errorf("任务未绑定有效阶段及阶段目标")
		}
		kind := "analysis_completed"
		if t.Type == "asset_discovery" { kind = "services_discovered" }
		if !slices.Contains(g.RequiredFacts, kind) { return fmt.Errorf("阶段验收条件未包含任务实际产出事实") }
		if covered[g.Key] == nil {
			covered[g.Key] = map[string]map[string]bool{}
			ports[g.Key] = map[string]map[int]bool{}
		}
		if covered[g.Key][t.Target] == nil { covered[g.Key][t.Target] = map[string]bool{}; ports[g.Key][t.Target] = map[int]bool{} }
		covered[g.Key][t.Target][kind] = true
		allTargets[t.Target] = true
		for _, port := range t.Ports { ports[g.Key][t.Target][port] = true }
		byKey[t.Key] = t
	}
	for _, t := range p.Tasks {
		for _, d := range t.Dependencies {
			parent := byKey[d.Key]
			if parent.Goal != t.Goal && (!slices.Contains(goals[t.Goal].DependsOn, parent.Goal) || d.Type != "success") {
				return fmt.Errorf("跨阶段任务依赖必须对应显式阶段成功依赖")
			}
		}
	}
	for _, g := range p.Goals {
		for _, target := range g.Targets {
			for _, kind := range g.RequiredFacts {
				if !covered[g.Key][target][kind] { return fmt.Errorf("阶段目标缺少所需事实的真实生产任务") }
			}
			if slices.Contains(g.RequiredFacts, "services_discovered") {
				for _, port := range effective.TCPPorts {
					if !ports[g.Key][target][port] { return fmt.Errorf("TCP阶段目标未覆盖全部冻结端口") }
				}
			}
		}
	}
	for _, target := range s.Scope.Targets {
		if !allTargets[target] {
			return fmt.Errorf("计划未覆盖全部授权目标")
		}
	}
	return nil
}
