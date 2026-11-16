package security

import (
	"encoding/json"
	"reflect"
	"testing"

	"autonomous-engine/pkg/contracts"
)

// objectiveTestTask 构造仅使用真实受支持工具的规划测试任务。
func objectiveTestTask(key, target, tool string, ports ...int) contracts.TaskSpec {
	kind := "web_analysis"
	if tool == "tcp.discover" {
		kind = "asset_discovery"
	}
	return contracts.TaskSpec{Key: key, Goal: "stage", Name: key, Type: kind, Executor: "pi", Target: target, Tools: []string{tool}, Capabilities: []string{kind}, Ports: ports}
}

// TestNormalizeObjectiveScopeDefaults 验证缺省技术字段只提供能力目录且不会猜测端口。
func TestNormalizeObjectiveScopeDefaults(t *testing.T) {
	tests := []struct {
		name       string   // 子场景名称
		target     string   // 用户原始目标
		wantTarget string   // 规范化后的目标
		wantTools  []string // 可供规划的能力目录
	}{
		{"url", "https://example.invalid/path?q=1", "https://example.invalid/path?q=1", []string{"http.observe"}},
		{"bare_ipv4", "192.0.2.1", "http://192.0.2.1/", []string{"http.observe", "tcp.discover"}},
		{"explicit_ipv4_url", "http://192.0.2.1/", "http://192.0.2.1/", []string{"http.observe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := contracts.Scope{Targets: []string{tt.target}}
			got, err := NormalizeObjectiveScope(input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Targets, []string{tt.wantTarget}) || !reflect.DeepEqual(got.Tools, tt.wantTools) || len(got.TCPPorts) != 0 {
				t.Fatalf("unexpected normalized scope: %+v", got)
			}
			if input.Targets[0] != tt.target || len(input.Tools) != 0 || len(input.TCPPorts) != 0 {
				t.Fatalf("input mutated: %+v", input)
			}
		})
	}
}

// TestResolvePlanScopeFreezesActualTools 验证执行范围来自实际任务并独立复制输入。
func TestResolvePlanScopeFreezesActualTools(t *testing.T) {
	scope, err := NormalizeObjectiveScope(contracts.Scope{Targets: []string{"192.0.2.1"}, Authorization: &contracts.Authorization{Confirmed: true, Statement: "isolated regression fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	tasks := []contracts.TaskSpec{objectiveTestTask("tcp", scope.Targets[0], "tcp.discover", 443, 80), objectiveTestTask("http", scope.Targets[0], "http.observe")}
	before, _ := json.Marshal(struct {
		Scope contracts.Scope
		Tasks []contracts.TaskSpec
	}{scope, tasks})
	got, err := ResolvePlanScope(scope, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tools, []string{"http.observe", "tcp.discover"}) || !reflect.DeepEqual(got.TCPPorts, []int{80, 443}) {
		t.Fatalf("unexpected effective scope: %+v", got)
	}
	got.Targets[0] = "https://changed.invalid/"
	got.Tools[0] = "changed"
	got.Authorization.Statement = "changed"
	after, _ := json.Marshal(struct {
		Scope contracts.Scope
		Tasks []contracts.TaskSpec
	}{scope, tasks})
	if string(before) != string(after) {
		t.Fatal("scope or tasks mutated through returned scope")
	}
	httpOnly, err := ResolvePlanScope(scope, tasks[1:])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(httpOnly.Tools, []string{"http.observe"}) || len(httpOnly.TCPPorts) != 0 {
		t.Fatalf("unused capabilities retained: %+v", httpOnly)
	}
	explicit := scope
	explicit.TCPPorts = []int{443, 80}
	frozen, err := ResolvePlanScope(explicit, tasks[:1])
	if err != nil {
		t.Fatal(err)
	}
	frozen.TCPPorts[0] = 22
	if !reflect.DeepEqual(explicit.TCPPorts, []int{443, 80}) {
		t.Fatal("explicit ports aliased or mutated")
	}
}

// TestResolvePlanScopeRejectsInvalidSelection 验证显式授权、合法端口及目标边界不可扩张。
func TestResolvePlanScopeRejectsInvalidSelection(t *testing.T) {
	base, err := NormalizeObjectiveScope(contracts.Scope{Targets: []string{"192.0.2.1"}, Authorization: &contracts.Authorization{Confirmed: true, Statement: "isolated regression fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string   // 拒绝场景名称
		tools   []string // 显式工具限制
		allowed []int    // 显式端口限制
		ports   []int    // 模型选择的端口
		target  string   // 模型选择的目标
	}{
		{name: "explicit_tool", tools: []string{"http.observe"}, ports: []int{80}},
		{name: "explicit_port", allowed: []int{80}, ports: []int{443}},
		{name: "missing_ports"},
		{name: "zero_port", ports: []int{0}},
		{name: "negative_port", ports: []int{-1}},
		{name: "large_port", ports: []int{65536}},
		{name: "duplicate_port", ports: []int{80, 80}},
		{name: "target_expansion", ports: []int{80}, target: "http://192.0.2.2/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := base
			if tt.tools != nil {
				scope.Tools = tt.tools
			}
			scope.TCPPorts = tt.allowed
			target := base.Targets[0]
			if tt.target != "" {
				target = tt.target
			}
			if _, err := ResolvePlanScope(scope, []contracts.TaskSpec{objectiveTestTask("tcp", target, "tcp.discover", tt.ports...)}); err == nil {
				t.Fatal("invalid plan selection accepted")
			}
		})
	}
	first := []int{1, 2, 3, 4, 5, 6, 7, 8}
	second := []int{9, 10, 11, 12, 13, 14, 15, 16, 17}
	if _, err := ResolvePlanScope(base, []contracts.TaskSpec{objectiveTestTask("a", base.Targets[0], "tcp.discover", first...), objectiveTestTask("b", base.Targets[0], "tcp.discover", second...)}); err == nil {
		t.Fatal("port union above sixteen accepted")
	}
	got, err := ResolvePlanScope(base, []contracts.TaskSpec{objectiveTestTask("a", base.Targets[0], "tcp.discover", 80), objectiveTestTask("b", base.Targets[0], "tcp.discover", 80, 443)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.TCPPorts, []int{80, 443}) {
		t.Fatalf("union not deduplicated: %v", got.TCPPorts)
	}
	if err := ValidateDiscovery(base, objectiveTestTask("tcp", base.Targets[0], "tcp.discover", 80)); err == nil {
		t.Fatal("execution accepted an unfrozen port scope")
	}
}
