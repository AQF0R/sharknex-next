package pi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

var (
	errNetworkPolicy = errors.New("目标地址被网络策略拒绝；本地授权靶场需在后端配置 PI_ALLOWED_CIDRS 后重启")
	errTargetConnect = errors.New("无法连接授权目标；请确认目标服务已启动、端口可达且 TLS 配置有效")
	errTargetDNS     = errors.New("授权目标 DNS 解析失败")
)

type HTTPObservation struct {
	Status      int    `json:"status"`       // 实际响应状态
	ContentType string `json:"content_type"` // 响应媒体类型
	Body        string `json:"body"`         // 有界不可信响应文本
	Truncated   bool   `json:"truncated"`    // 是否超过读取窗口
}

// permittedIP 阻止保留网络，私有靶场仅允许服务端明确配置的网段。
func permittedIP(ip netip.Addr, allowed []string) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, raw := range allowed {
		prefix, er := netip.ParsePrefix(raw)
		if er == nil && prefix.Contains(ip) {
			return true
		}
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16"} {
		prefix := netip.MustParsePrefix(raw)
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// observeHTTP 只访问精确任务目标，不跟随重定向、不继承代理、认证或 Cookie。
func observeHTTP(ctx context.Context, target string, allowed []string) (string, error) {
	u, er := url.Parse(target)
	if er != nil || u.User != nil || u.Hostname() == "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("HTTP 目标无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	transport := &http.Transport{DisableCompression: true, MaxResponseHeaderBytes: 8192, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, er := net.SplitHostPort(address)
			if er != nil {
				return nil, errors.New("连接地址无效")
			}
			addresses, er := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if er != nil || len(addresses) == 0 {
				return nil, errTargetDNS
			}
			// 任一地址不允许就拒绝整次请求，不在候选地址之间扩大权限。
			for _, ip := range addresses {
				if !permittedIP(ip, allowed) {
					return nil, errNetworkPolicy
				}
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, er := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if er != nil {
		return "", errors.New("HTTP 请求无效")
	}
	request.Header.Set("User-Agent", "AutonomousEngine-ReadOnly/1")
	response, er := client.Do(request)
	if er != nil {
		if errors.Is(er, errNetworkPolicy) {
			return "", errNetworkPolicy
		}
		if errors.Is(er, errTargetDNS) {
			return "", errTargetDNS
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errTargetConnect
	}
	defer response.Body.Close()
	body, er := io.ReadAll(io.LimitReader(response.Body, 2049))
	if er != nil {
		return "", errors.New("读取 HTTP 观察失败")
	}
	truncated := len(body) > 2048
	if truncated {
		body = body[:2048]
	}
	contentType := response.Header.Get("Content-Type")
	if len(contentType) > 256 {
		contentType = contentType[:256]
	}
	data, er := json.Marshal(HTTPObservation{Status: response.StatusCode, ContentType: contentType, Body: string(body), Truncated: truncated})
	return string(data), er
}
