package pi

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// planningRegressionSpec 使用纯范围解析构造规划输入，不访问模型、网络目标或服务。
func planningRegressionSpec(t *testing.T, target string, tools []string, ports []int) contracts.RunSpec {
	t.Helper()
	scope, err := security.NormalizeObjectiveScope(contracts.Scope{
		Targets: []string{target}, Tools: tools, TCPPorts: ports,
		Authorization: &contracts.Authorization{Confirmed: true, Statement: "离线规划范围回归，不执行网络请求"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return contracts.RunSpec{
		Name: "范围回归", Objective: "按明确目标选择已实现的有限观察能力", Scope: scope,
		MaxWorkers: 2, MaxTasks: 8, Deadline: time.Now().Add(time.Minute),
	}
}

// planningRegressionPlan 构造单目标真实工具配对，端口仅来自测试指定的模型输出。
func planningRegressionPlan(spec contracts.RunSpec, ports []int, tcp bool) *contracts.Plan {
	kind, tool, fact := "web_analysis", "http.observe", "analysis_completed"
	if tcp {
		kind, tool, fact = "asset_discovery", "tcp.discover", "services_discovered"
	}
	target := spec.Scope.Targets[0]
	return &contracts.Plan{
		Summary: "仅计划有界观察，不声称漏洞验证成功",
		Goals:   []contracts.GoalSpec{{Key: "g1", Name: "有限观察", Targets: []string{target}, RequiredFacts: []string{fact}, DependsOn: []string{}}},
		Tasks:   []contracts.TaskSpec{{Key: "t1", Goal: "g1", Name: "执行有限观察", Target: target, Type: kind, Executor: "pi", Tools: []string{tool}, Capabilities: []string{kind}, Ports: ports, Priority: 50, Dependencies: []contracts.DependencySpec{}, DelayMS: 0}},
	}
}

// planningRegressionJSON 保留工具schema要求的显式空数组，穿过实际计划解码边界。
func planningRegressionJSON(t *testing.T, plan *contracts.Plan) []byte {
	t.Helper()
	goals := make([]map[string]any, 0, len(plan.Goals))
	for _, goal := range plan.Goals {
		goals = append(goals, map[string]any{
			"key": goal.Key, "name": goal.Name, "targets": goal.Targets,
			"required_facts": goal.RequiredFacts, "depends_on": goal.DependsOn,
		})
	}
	value, err := json.Marshal(map[string]any{"summary": plan.Summary, "goals": goals, "tasks": plan.Tasks})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestPlanningObjectiveScopeOmittedTechnicalFields 复现只填目标时缺省端口被提前当作执行授权拒绝。
func TestPlanningObjectiveScopeOmittedTechnicalFields(t *testing.T) {
	for _, tc := range []struct {
		name   string   // 回归场景
		target string   // 用户原始目标
		tools  []string // 规范化后的候选能力
	}{
		{name: "explicit_url_defaults_http", target: "https://example.invalid/authorized", tools: []string{"http.observe"}},
		{name: "explicit_ip_url_defaults_http", target: "http://192.0.2.10/", tools: []string{"http.observe"}},
		{name: "bare_ipv4_allows_model_selection", target: "192.0.2.10", tools: []string{"http.observe", "tcp.discover"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := planningRegressionSpec(t, tc.target, nil, nil)
			if !reflect.DeepEqual(spec.Scope.Tools, tc.tools) || len(spec.Scope.TCPPorts) != 0 {
				t.Fatalf("候选工具或缺省端口改变：%+v", spec.Scope)
			}
			if err := validatePlanningSpec(spec); err != nil {
				t.Fatalf("缺省技术字段应允许进入模型规划：%v", err)
			}
		})
	}
}

// TestPlanningModelSelectedPortsStages 覆盖无端口输入到模型TCP及HTTP阶段再到冻结执行范围的完整纯函数链。
func TestPlanningModelSelectedPortsStages(t *testing.T) {
	spec := planningRegressionSpec(t, "192.0.2.10", nil, nil)
	plan := planningRegressionPlan(spec, []int{25432, 18081}, true)
	target := spec.Scope.Targets[0]
	plan.Goals = append(plan.Goals, contracts.GoalSpec{Key: "g2", Name: "只读HTTP观察", Targets: []string{target}, RequiredFacts: []string{"analysis_completed"}, DependsOn: []string{"g1"}})
	plan.Tasks = append(plan.Tasks, contracts.TaskSpec{Key: "t2", Goal: "g2", Name: "同IP精确根URL观察", Target: target, Type: "web_analysis", Executor: "pi", Tools: []string{"http.observe"}, Capabilities: []string{"web_analysis"}, Priority: 50, Dependencies: []contracts.DependencySpec{{Key: "t1", Type: "success"}}})
	specBefore, _ := json.Marshal(spec)
	planBefore, _ := json.Marshal(plan)
	// 执行校验不能因为规划允许缺省端口而提前放行。
	if security.ValidateDiscovery(spec.Scope, plan.Tasks[0]) == nil {
		t.Fatal("未冻结端口不应通过执行授权校验")
	}
	decoded, err := decodePlanningOutput(planningRegressionJSON(t, plan), spec)
	if err != nil {
		t.Fatalf("模型明确选择端口的两阶段计划应有效：%v", err)
	}
	resolved, err := security.ResolvePlanScope(spec.Scope, decoded.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.TCPPorts, []int{18081, 25432}) || !reflect.DeepEqual(resolved.Tools, []string{"http.observe", "tcp.discover"}) {
		t.Fatalf("冻结范围必须仅含实际模型选择：%+v", resolved)
	}
	for _, task := range decoded.Tasks {
		if err := security.ValidatePlannedTask(resolved, task); err != nil {
			t.Fatalf("冻结后执行校验失败：%v", err)
		}
	}
	specAfter, _ := json.Marshal(spec)
	planAfter, _ := json.Marshal(plan)
	if string(specBefore) != string(specAfter) || string(planBefore) != string(planAfter) {
		t.Fatal("校验不得回写调用者的候选范围或模型任务")
	}
	if !reflect.DeepEqual(decoded.Tasks[0].Ports, []int{25432, 18081}) || len(decoded.Tasks[1].Ports) != 0 {
		t.Fatal("模型端口被默认值替换、排序回写或扩展到HTTP任务")
	}
}

// TestPlanningOptionalPortsPreserveExplicitLimits 校验缺省与显式端口、工具上限及真实事实配对。
func TestPlanningOptionalPortsPreserveExplicitLimits(t *testing.T) {
	for _, tc := range []struct {
		name       string   // 回归场景
		scopeTools []string // 用户显式工具限制
		scopePorts []int    // 用户显式端口上限
		ports      []int    // 模型任务选择端口
		tcp        bool     // 是否为TCP任务
		want       error    // 预期固定错误
	}{
		{name: "implicit_ports_tcp", tcp: true, ports: []int{18081}},
		{name: "implicit_ports_http_only"},
		{name: "explicit_ports_subset", scopeTools: []string{"tcp.discover"}, scopePorts: []int{18081, 25432}, ports: []int{25432}, tcp: true},
		{name: "explicit_ports_not_used_by_http", scopeTools: []string{"http.observe", "tcp.discover"}, scopePorts: []int{18081}},
		{name: "explicit_ports_escape", scopeTools: []string{"tcp.discover"}, scopePorts: []int{18081}, ports: []int{25432}, tcp: true, want: ErrPlanInvalid},
		{name: "explicit_http_disallows_tcp", scopeTools: []string{"http.observe"}, ports: []int{18081}, tcp: true, want: ErrPlanInvalid},
		{name: "explicit_tcp_disallows_http", scopeTools: []string{"tcp.discover"}, want: ErrPlanInvalid},
		{name: "model_must_select_ports", tcp: true, want: ErrPlanInvalid},
		{name: "model_duplicate_ports", ports: []int{18081, 18081}, tcp: true, want: ErrPlanInvalid},
		{name: "model_zero_port", ports: []int{0}, tcp: true, want: ErrPlanInvalid},
		{name: "model_negative_port", ports: []int{-1}, tcp: true, want: ErrPlanInvalid},
		{name: "model_port_overflow", ports: []int{65536}, tcp: true, want: ErrPlanInvalid},
		{name: "model_ports_over_budget", ports: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}, tcp: true, want: ErrPlanInvalid},
		{name: "model_sixteen_ports", ports: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 65535}, tcp: true},
		{name: "http_cannot_smuggle_ports", ports: []int{18081}, want: ErrPlanInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := planningRegressionSpec(t, "192.0.2.10", tc.scopeTools, tc.scopePorts)
			plan := planningRegressionPlan(spec, tc.ports, tc.tcp)
			before, _ := json.Marshal(spec)
			_, err := decodePlanningOutput(planningRegressionJSON(t, plan), spec)
			if !errors.Is(err, tc.want) {
				t.Fatalf("预期%v，得到%v", tc.want, err)
			}
			after, _ := json.Marshal(spec)
			if string(before) != string(after) {
				t.Fatal("输入范围被修改")
			}
		})
	}
}

// TestPlanningSpecExplicitPortsStillStrict 确保规划端口可省略但显式非法值不能进入模型请求。
func TestPlanningSpecExplicitPortsStillStrict(t *testing.T) {
	for _, ports := range [][]int{{0}, {-1}, {65536}, {18081, 18081}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}} {
		spec := planningRegressionSpec(t, "192.0.2.10", nil, nil)
		spec.Scope.TCPPorts = ports
		if !errors.Is(validatePlanningSpec(spec), ErrPlanInput) {
			t.Fatalf("非法显式端口未拒绝：%v", ports)
		}
	}
	spec := planningRegressionSpec(t, "192.0.2.10", []string{"tcp.discover"}, []int{18081})
	if err := validatePlanningSpec(spec); err != nil {
		t.Fatalf("旧显式单端口范围应继续有效：%v", err)
	}
}
