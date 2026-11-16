package pi

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// validatePlanningSpec 只接受明确目标和受支持的已授权工具，不读取环境或自动扩展范围。
func validatePlanningSpec(spec contracts.RunSpec) error {
	if !planningText(spec.Objective, 8000) || len(spec.Scope.Targets) == 0 || len(spec.Scope.Targets) > 100 || len(spec.Scope.Tools) == 0 || len(spec.Scope.Tools) > 2 || spec.MaxTasks < 1 || spec.MaxTasks > 1000 || spec.MaxWorkers < 1 || spec.MaxWorkers > 64 || spec.Deadline.IsZero() {
		return ErrPlanInput
	}
	if !spec.Deadline.After(time.Now()) || spec.MaxTasks < 2 {
		return ErrPlanBudget
	}
	if spec.Scope.Authorization == nil || !spec.Scope.Authorization.Confirmed || !planningText(spec.Scope.Authorization.Statement, 1000) {
		return ErrPlanInput
	}
	targets := map[string]bool{}
	for _, target := range spec.Scope.Targets {
		u, err := url.Parse(target)
		if err != nil || !planningText(target, 512) || targets[target] || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
			return ErrPlanInput
		}
		targets[target] = true
	}
	tools := map[string]bool{}
	for _, tool := range spec.Scope.Tools {
		if (tool != "http.observe" && tool != "tcp.discover") || tools[tool] {
			return ErrPlanInput
		}
		tools[tool] = true
	}
	if tools["tcp.discover"] {
		if len(spec.Scope.Targets) != 1 {
			return ErrPlanInput
		}
		target := spec.Scope.Targets[0]
		u, _ := url.Parse(target)
		ip, err := netip.ParseAddr(u.Hostname())
		if err != nil || !ip.Is4() || target != "http://"+ip.String()+"/" {
			return ErrPlanInput
		}
		// 规划能力不是执行授权：缺省端口由模型选择，显式限制仍保持原校验。
		if len(spec.Scope.TCPPorts) > 0 && security.ValidateDiscovery(spec.Scope, contracts.TaskSpec{Target: target, Ports: spec.Scope.TCPPorts}) != nil {
			return ErrPlanInput
		}
	} else if len(spec.Scope.TCPPorts) != 0 {
		return ErrPlanInput
	}
	return nil
}

// strictPlanningJSON 拒绝超限、未知键、大小写别名、重复JSON键和尾随值。
func strictPlanningJSON(raw []byte, out any, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return ErrPlanInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if err := planningJSONValue(tokens, 0); err != nil {
		return ErrPlanInvalid
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrPlanInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrPlanInvalid
	}
	return nil
}

// planningJSONValue 用有限深度遍历标准JSON token，阻止重复字段被后值静默覆盖。
func planningJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrPlanInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrPlanInvalid
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok || keys[key] {
				return ErrPlanInvalid
			}
			keys[key] = true
			if planningJSONValue(decoder, depth+1) != nil {
				return ErrPlanInvalid
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return ErrPlanInvalid
		}
	case '[':
		for decoder.More() {
			if planningJSONValue(decoder, depth+1) != nil {
				return ErrPlanInvalid
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return ErrPlanInvalid
		}
	default:
		return ErrPlanInvalid
	}
	return nil
}

// planningObjectShape 校验逐字字段名和必需字段，null不冒充空数组或零值。
func planningObjectShape(raw []byte, required, optional []string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	for name, value := range fields {
		if (!slices.Contains(required, name) && !slices.Contains(optional, name)) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

// decodePlanningOutput 使用窄输出合同解析计划，Usage只允许由父进程累计。
func decodePlanningOutput(raw []byte, spec contracts.RunSpec) (*contracts.Plan, error) {
	var output struct {
		Summary string               `json:"summary"` // 有界计划摘要
		Goals   []contracts.GoalSpec `json:"goals"`   // 待验证的阶段
		Tasks   []contracts.TaskSpec `json:"tasks"`   // 待验证的任务
	}
	if strictPlanningJSON(raw, &output, maxPlanBytes) != nil || !planningObjectShape(raw, []string{"summary", "goals", "tasks"}, nil) {
		return nil, ErrPlanInvalid
	}
	// Go默认按不区分大小写的字段名匹配，这里再次核验每层JSON的准确键名。
	var shapes struct {
		Goals []json.RawMessage `json:"goals"` // 原始阶段对象
		Tasks []json.RawMessage `json:"tasks"` // 原始任务对象
	}
	if json.Unmarshal(raw, &shapes) != nil {
		return nil, ErrPlanInvalid
	}
	for _, goal := range shapes.Goals {
		if !planningObjectShape(goal, []string{"key", "name", "required_facts", "depends_on", "targets"}, nil) {
			return nil, ErrPlanInvalid
		}
	}
	for _, task := range shapes.Tasks {
		if !planningObjectShape(task, []string{"key", "goal", "name", "target", "type", "executor", "tools", "capabilities", "priority", "dependencies", "delay_ms"}, []string{"ports"}) {
			return nil, ErrPlanInvalid
		}
		var dependencies struct {
			Items []json.RawMessage `json:"dependencies"` // 原始依赖对象
		}
		if json.Unmarshal(task, &dependencies) != nil {
			return nil, ErrPlanInvalid
		}
		for _, item := range dependencies.Items {
			if !planningObjectShape(item, []string{"key", "type"}, nil) {
				return nil, ErrPlanInvalid
			}
		}
	}
	plan := &contracts.Plan{Summary: output.Summary, Goals: output.Goals, Tasks: output.Tasks}
	if err := validatePlanningOutput(plan, spec); err != nil {
		return nil, err
	}
	return plan, nil
}

// validatePlanningOutput 保守校验授权、真实事实覆盖和两层DAG，最终落库规则仍由Engine负责。
func validatePlanningOutput(plan *contracts.Plan, spec contracts.RunSpec) error {
	if !planningText(plan.Summary, 1200) || len(plan.Goals) == 0 || len(plan.Goals) > maxPlanGoals || len(plan.Tasks) == 0 || len(plan.Tasks) > maxPlanTasks {
		return ErrPlanInvalid
	}
	if len(plan.Goals)+len(plan.Tasks) > spec.MaxTasks {
		return ErrPlanBudget
	}
	// 从模型的明确选择冻结局部执行范围，不修改调用方输入或放宽执行校验。
	scope, err := security.ResolvePlanScope(spec.Scope, plan.Tasks)
	if err != nil {
		return ErrPlanInvalid
	}
	goals := map[string]contracts.GoalSpec{}
	stages := map[string][]string{}
	for _, goal := range plan.Goals {
		if !planningKey(goal.Key) || !planningText(goal.Name, 255) || len(goal.Targets) == 0 || len(goal.Targets) > len(spec.Scope.Targets) || len(goal.RequiredFacts) == 0 || len(goal.RequiredFacts) > 2 || len(goal.DependsOn) > len(plan.Goals) {
			return ErrPlanInvalid
		}
		if _, exists := goals[goal.Key]; exists {
			return ErrPlanInvalid
		}
		if !planningSubset(goal.Targets, spec.Scope.Targets) || !planningSubset(goal.RequiredFacts, []string{"services_discovered", "analysis_completed"}) {
			return ErrPlanInvalid
		}
		goals[goal.Key] = goal
		stages[goal.Key] = goal.DependsOn
	}
	if !planningDAG(stages) {
		return ErrPlanInvalid
	}
	tasks := map[string]contracts.TaskSpec{}
	dependencies := map[string][]string{}
	produced := map[string]map[string]map[string]bool{}
	operations := map[string]map[string]bool{}
	for _, task := range plan.Tasks {
		goal, exists := goals[task.Goal]
		if !exists || !planningKey(task.Key) || !planningText(task.Name, 255) || task.Executor != "pi" || task.DelayMS != 0 || task.Priority < 0 || task.Priority > 100 || len(task.Tools) != 1 || len(task.Capabilities) != 1 || task.Capabilities[0] != task.Type || !slices.Contains(goal.Targets, task.Target) || len(task.Dependencies) > len(plan.Tasks) {
			return ErrPlanInvalid
		}
		if _, exists := tasks[task.Key]; exists {
			return ErrPlanInvalid
		}
		fact, tool := "", ""
		switch task.Type {
		case "asset_discovery":
			fact, tool = "services_discovered", "tcp.discover"
			if security.ValidateDiscovery(scope, task) != nil {
				return ErrPlanInvalid
			}
		case "web_analysis":
			fact, tool = "analysis_completed", "http.observe"
			if len(task.Ports) != 0 {
				return ErrPlanInvalid
			}
		default:
			return ErrPlanInvalid
		}
		if task.Tools[0] != tool || !slices.Contains(scope.Tools, tool) || !slices.Contains(goal.RequiredFacts, fact) {
			return ErrPlanInvalid
		}
		if operations[task.Target] == nil {
			operations[task.Target] = map[string]bool{}
		}
		if operations[task.Target][task.Type] {
			return ErrPlanInvalid
		}
		operations[task.Target][task.Type] = true
		tasks[task.Key] = task
		dependencies[task.Key] = nil
		for _, dependency := range task.Dependencies {
			if dependency.Type != "success" && dependency.Type != "completed" {
				return ErrPlanInvalid
			}
			dependencies[task.Key] = append(dependencies[task.Key], dependency.Key)
		}
		if produced[task.Goal] == nil {
			produced[task.Goal] = map[string]map[string]bool{}
		}
		if produced[task.Goal][task.Target] == nil {
			produced[task.Goal][task.Target] = map[string]bool{}
		}
		produced[task.Goal][task.Target][fact] = true
	}
	if !planningDAG(dependencies) {
		return ErrPlanInvalid
	}
	for _, task := range plan.Tasks {
		for _, dependency := range task.Dependencies {
			prior := tasks[dependency.Key]
			if prior.Goal != task.Goal && !planningAncestor(stages, task.Goal, prior.Goal, map[string]bool{}) {
				return ErrPlanInvalid
			}
		}
	}
	for _, goal := range plan.Goals {
		for _, target := range goal.Targets {
			for _, fact := range goal.RequiredFacts {
				if !produced[goal.Key][target][fact] {
					return ErrPlanInvalid
				}
			}
		}
	}
	for _, target := range spec.Scope.Targets {
		if len(operations[target]) == 0 {
			return ErrPlanInvalid
		}
	}
	return nil
}

// planningText 保持文本有界、有效且不含控制字符，绝不把原文加入错误。
func planningText(value string, limit int) bool {
	if len(value) > limit || strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 32 && char != '\n' && char != '\t' {
			return false
		}
	}
	return true
}

// planningKey 限制计划引用为有界ASCII标识，避免日志控制符及保留键歧义。
func planningKey(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for i, char := range value {
		alpha := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
		if !alpha && (i == 0 || !strings.ContainsRune("_.:-", char)) {
			return false
		}
	}
	return true
}

// planningSubset 验证不重复的授权子集，集合规模受协议上限约束。
func planningSubset(values, allowed []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] || !slices.Contains(allowed, value) {
			return false
		}
		seen[value] = true
	}
	return true
}

// planningDAG 以O(V+E)遍历验证依赖存在、无重复且无环。
func planningDAG(graph map[string][]string) bool {
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(key string) bool {
		if state[key] == 1 {
			return false
		}
		if state[key] == 2 {
			return true
		}
		edges, exists := graph[key]
		if !exists {
			return false
		}
		state[key] = 1
		seen := map[string]bool{}
		for _, dependency := range edges {
			if seen[dependency] || !visit(dependency) {
				return false
			}
			seen[dependency] = true
		}
		state[key] = 2
		return true
	}
	for key := range graph {
		if !visit(key) {
			return false
		}
	}
	return true
}

// planningAncestor 防止任务逆向跨阶段依赖导致阶段门禁死锁。
func planningAncestor(graph map[string][]string, child, ancestor string, seen map[string]bool) bool {
	if seen[child] {
		return false
	}
	seen[child] = true
	for _, parent := range graph[child] {
		if parent == ancestor || planningAncestor(graph, parent, ancestor, seen) {
			return true
		}
	}
	return false
}
