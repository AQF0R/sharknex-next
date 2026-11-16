package security

import (
	"autonomous-engine/pkg/contracts"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
)

// Hash 使用标准摘要保存高熵令牌和证据内容的指纹。
func Hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

// Equal 以常量时间比较令牌摘要。
func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// ValidateTarget 采用完整目标精确匹配，不隐式放大域名、端口或路径授权。
func ValidateTarget(scope contracts.Scope, target string) error {
	if !slices.Contains(scope.Targets, target) {
		return errors.New("目标超出授权范围")
	}
	if strings.HasPrefix(target, "local://") {
		u, e := url.Parse(target)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("本地目标无效")
		}
		return nil
	}
	u, e := url.Parse(target)
	if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
		return errors.New("目标必须为明确的 local/http/https URL")
	}
	return nil
}

// ValidateTask 在规划和执行边界重复校验目标、能力及工具白名单。
func ValidateTask(scope contracts.Scope, t contracts.TaskSpec) error {
	if e := ValidateTarget(scope, t.Target); e != nil {
		return e
	}
	if t.Key == "" || t.Name == "" || t.Goal == "" || t.Priority < 0 || t.Priority > 100 || len(t.Capabilities) == 0 || len(t.Tools) == 0 || t.DelayMS < 0 || t.DelayMS > 30000 {
		return errors.New("任务参数无效")
	}
	for _, tool := range t.Tools {
		if !slices.Contains(scope.Tools, tool) {
			return errors.New("工具超出授权清单")
		}
	}
	requiredTool := "local.observe"
	if t.Executor == "pi" {
		requiredTool = "http.observe"
		if slices.Contains(t.Tools, "tcp.discover") {
			requiredTool = "tcp.discover"
			if t.Type != "asset_discovery" || len(t.Tools) != 1 {
				return errors.New("TCP资产任务只能使用tcp.discover")
			}
			if er := ValidateDiscovery(scope, t); er != nil {
				return er
			}
		}
	} else if t.Executor != "" && t.Executor != "local" {
		return errors.New("执行器未注册")
	}
	for _, tool := range t.Tools {
		if tool != "local.observe" && tool != "http.observe" && tool != "tcp.discover" {
			return errors.New("工具未注册")
		}
	}
	if t.Executor == "pi" && strings.HasPrefix(t.Target, "local://") {
		return errors.New("Pi 需要明确的 HTTP 目标")
	}
	if !slices.Contains(t.Tools, requiredTool) || !slices.Contains(t.Capabilities, t.Type) {
		return errors.New("任务缺少执行器所需工具或能力")
	}
	if !slices.Contains([]string{"asset_discovery", "service_analysis", "web_analysis", "evidence_collection", "verification"}, t.Type) {
		return errors.New("任务类型未注册")
	}
	return nil
}
