package security

import (
	"autonomous-engine/pkg/contracts"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

// NormalizeObjectiveScope 只形成规划能力目录；具体执行端口须待模型计划校验后冻结。
func NormalizeObjectiveScope(scope contracts.Scope) (contracts.Scope, error) {
	scope = cloneScope(scope)
	if len(scope.Targets) < 1 || len(scope.Targets) > 10 || len(scope.Tools) > 2 {
		return scope, errors.New("统一任务仅支持1至10个精确URL或单个IPv4及已实现工具")
	}
	bareIP := false
	seen := map[string]bool{}
	for i, raw := range scope.Targets {
		target := strings.TrimSpace(raw)
		if ip, err := netip.ParseAddr(target); err == nil && ip.Is4() {
			if len(scope.Targets) != 1 { return scope, errors.New("裸IPv4不能与其他目标混合") }
			bareIP = true
			target = "http://" + ip.String() + "/"
		}
		u, err := url.Parse(target)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || len(target) > 512 || seen[target] {
			return scope, errors.New("目标必须是不重复的精确HTTP(S) URL或单个IPv4")
		}
		seen[target] = true
		scope.Targets[i] = target
	}
	if len(scope.Tools) == 0 {
		scope.Tools = []string{"http.observe"}
		if bareIP { scope.Tools = append(scope.Tools, "tcp.discover") }
	}
	seen = map[string]bool{}
	for i, raw := range scope.Tools {
		tool := strings.TrimSpace(raw)
		if seen[tool] || (tool != "http.observe" && tool != "tcp.discover") { return scope, errors.New("规划工具重复或未实现") }
		seen[tool] = true
		scope.Tools[i] = tool
	}
	if seen["tcp.discover"] {
		if len(scope.Targets) != 1 || !planningTCPTarget(scope.Targets[0]) { return scope, errors.New("TCP规划仅支持单IPv4根目标，不扩展域名、路径或关联资产") }
	} else if len(scope.TCPPorts) > 0 {
		return scope, errors.New("TCP端口限制需要允许tcp.discover能力")
	}
	if err := validatePlanningPorts(scope.TCPPorts); err != nil { return scope, err }
	slices.Sort(scope.Tools)
	slices.Sort(scope.TCPPorts)
	return scope, nil
}

// cloneScope 复制集合和授权声明，保证解析结果不与调用者共享可变输入。
func cloneScope(scope contracts.Scope) contracts.Scope {
	scope.Targets = slices.Clone(scope.Targets)
	scope.Tools = slices.Clone(scope.Tools)
	scope.TCPPorts = slices.Clone(scope.TCPPorts)
	if scope.Authorization != nil { authorization := *scope.Authorization; scope.Authorization = &authorization }
	return scope
}

// planningTCPTarget 验证仅供规划选择TCP能力的单IPv4根URL，不产生执行端口授权。
func planningTCPTarget(target string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") { return false }
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && ip.Is4()
}

// validatePlanningPorts 校验可选显式端口限制，不把缺省清单解释为可执行任务。
func validatePlanningPorts(ports []int) error {
	if len(ports) > 16 { return errors.New("TCP端口并集不得超过16个") }
	seen := map[int]bool{}
	for _, port := range ports {
		if port < 1 || port > 65535 || seen[port] { return errors.New("TCP端口无效或重复") }
		seen[port] = true
	}
	return nil
}

// ResolvePlanScope 从规范化规划范围和模型任务解析不可扩大的执行范围，且不修改输入。
func ResolvePlanScope(scope contracts.Scope, tasks []contracts.TaskSpec) (contracts.Scope, error) {
	resolved := cloneScope(scope)
	if len(scope.Targets) == 0 || len(scope.Tools) == 0 || len(tasks) == 0 || len(tasks) > 1000 { return resolved, errors.New("缺少规范化规划范围或可执行任务") }
	if err := validatePlanningPorts(scope.TCPPorts); err != nil { return resolved, err }
	allowedTools := map[string]bool{}
	for _, tool := range scope.Tools {
		if allowedTools[tool] || (tool != "http.observe" && tool != "tcp.discover") { return resolved, errors.New("规划能力清单无效") }; allowedTools[tool] = true
	}
	allowedPorts := map[int]bool{}
	for _, port := range scope.TCPPorts { allowedPorts[port] = true }
	usedTools := map[string]bool{}
	usedPorts := map[int]bool{}
	for _, spec := range tasks {
		if err := ValidateTarget(scope, spec.Target); err != nil { return resolved, err }
		if len(spec.Tools) != 1 || !allowedTools[spec.Tools[0]] { return resolved, errors.New("任务工具超出规划能力或显式限制") }
		tool := spec.Tools[0]
		usedTools[tool] = true
		if tool != "tcp.discover" {
			if len(spec.Ports) != 0 { return resolved, errors.New("非TCP任务不得夹带端口") }; continue
		}
		if len(scope.Targets) != 1 || !planningTCPTarget(spec.Target) || len(spec.Ports) == 0 { return resolved, errors.New("TCP计划必须选择单IPv4及明确端口") }
		if err := validatePlanningPorts(spec.Ports); err != nil { return resolved, err }
		for _, port := range spec.Ports {
			if len(allowedPorts) > 0 && !allowedPorts[port] { return resolved, errors.New("模型端口超出显式端口限制") }
			usedPorts[port] = true
		}
		if len(usedPorts) > 16 { return resolved, errors.New("计划TCP端口并集不得超过16个") }
	}
	resolved.Tools = make([]string, 0, len(usedTools))
	for tool := range usedTools { resolved.Tools = append(resolved.Tools, tool) }
	slices.Sort(resolved.Tools)
	// 显式端口仍是需要验收的范围；缺省时才从实际模型任务生成冻结全集。
	if !usedTools["tcp.discover"] { resolved.TCPPorts = nil } else if len(scope.TCPPorts) == 0 {
		resolved.TCPPorts = make([]int, 0, len(usedPorts))
		for port := range usedPorts { resolved.TCPPorts = append(resolved.TCPPorts, port) }
	}
	slices.Sort(resolved.TCPPorts)
	for _, spec := range tasks { if err := ValidatePlannedTask(resolved, spec); err != nil { return resolved, err } }
	return resolved, nil
}

// ValidatePlannedTask 约束真实计划任务的类型、工具和可满足能力组合。
func ValidatePlannedTask(scope contracts.Scope, t contracts.TaskSpec) error {
	if err := ValidateTask(scope, t); err != nil {
		return err
	}
	if t.Executor != "pi" || t.DelayMS != 0 || len(t.Tools) != 1 || len(t.Capabilities) != 1 || t.Capabilities[0] != t.Type || len(t.Key) > 200 || len(t.Name) > 255 || len(t.Dependencies) > 1000 {
		return errors.New("计划必须使用真实Pi执行器及可用的单一任务能力")
	}
	if (t.Type == "asset_discovery" && t.Tools[0] == "tcp.discover") || (t.Type == "web_analysis" && t.Tools[0] == "http.observe" && len(t.Ports) == 0) {
		return nil
	}
	return errors.New("计划任务类型与真实工具不匹配")
}
