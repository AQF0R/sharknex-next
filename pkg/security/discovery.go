package security

import (
	"autonomous-engine/pkg/contracts"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

// ValidateDiscovery 校验单一IP与明确端口集合及控制端授权声明，不扩大到关联资产。
func ValidateDiscovery(scope contracts.Scope, t contracts.TaskSpec) error {
	if scope.Authorization == nil || !scope.Authorization.Confirmed || strings.TrimSpace(scope.Authorization.Statement) == "" || len(scope.Authorization.Statement) > 1000 {
		return errors.New("TCP资产探测需要控制端提供明确的授权声明")
	}
	u, err := url.Parse(t.Target)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("资产目标须为 http://精确IP/，端口由授权端口清单指定")
	}
	if _, err := netip.ParseAddr(u.Hostname()); err != nil {
		return errors.New("资产探测仅允许IP字面量，不扩展DNS")
	}
	if len(scope.Targets) != 1 || len(scope.TCPPorts) == 0 || len(scope.TCPPorts) > 16 || len(t.Ports) == 0 || len(t.Ports) > 16 {
		return errors.New("单次资产探测仅允许一个IP及1至16个端口")
	}
	seen := map[int]bool{}
	for _, p := range scope.TCPPorts {
		if p < 1 || p > 65535 || seen[p] {
			return errors.New("授权端口无效或重复")
		}
		seen[p] = true
	}
	seen = map[int]bool{}
	for _, p := range t.Ports {
		if p < 1 || p > 65535 || seen[p] || !slices.Contains(scope.TCPPorts, p) {
			return errors.New("任务端口超出授权范围或重复")
		}
		seen[p] = true
	}
	return nil
}

// DiscoveryIP 提取已验证的固定目标IP供工具使用。
func DiscoveryIP(target string) (netip.Addr, error) {
	u, err := url.Parse(target)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(u.Hostname())
}
