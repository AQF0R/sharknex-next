package pi

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type PortObservation struct {
	Port        int    `json:"port"`                  // 明确授权的端口
	State       string `json:"state"`                 // open/closed/timeout/error
	Service     string `json:"service"`               // 有证据支持的协议而非端口猜测
	Hint        string `json:"hint"`                  // 仅根据端口推测的服务
	Banner      string `json:"banner,omitempty"`      // 有界欢迎文本
	HTTPStatus  int    `json:"http_status,omitempty"` // 实际HTTP状态
	Server      string `json:"server,omitempty"`      // 服务端自报字段
	PoweredBy   string `json:"powered_by,omitempty"`  // 服务端自报框架
	Title       string `json:"title,omitempty"`       // 页面标题
	Fingerprint string `json:"fingerprint,omitempty"` // 规则匹配结果
	Basis       string `json:"basis,omitempty"`       // 指纹证据依据与限制
	TLS         string `json:"tls,omitempty"`         // 经正常证书验证的TLS信息
	Note        string `json:"note,omitempty"`        // 失败或覆盖限制
}
type DiscoveryReport struct {
	Version     string            `json:"version"`         // 报告格式版本
	IP          string            `json:"ip"`              // 固定IP
	Requested   []int             `json:"requested_ports"` // 声明的端口全集
	Ports       []PortObservation `json:"ports"`           // 已执行的端口观察
	Complete    bool              `json:"complete"`        // 是否完成全部端口
	StartedAt   time.Time         `json:"started_at"`      // 开始时间
	FinishedAt  time.Time         `json:"finished_at"`     // 结束时间
	Limitations string            `json:"limitations"`     // 防止错误解读的覆盖边界
}

// boundedText 将不可信响应限制为有效UTF-8和固定字节数。
func boundedText(s string, max int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// waitProbe 限制每批次网络连接启动频率，不使用Shell或裸包。
func waitProbe(ctx context.Context) error {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// discoverServices 仅探测服务端绑定IP及端口，串行connect和有限只读指纹采集。
func (e *Executor) discoverServices(ctx context.Context, s *session) (DiscoveryReport, error) {
	ip, err := security.DiscoveryIP(s.spec.Target)
	if err != nil || !permittedIP(ip, e.Config.AllowedCIDRs) {
		return DiscoveryReport{}, errNetworkPolicy
	}
	if len(s.spec.Ports) == 0 || len(s.spec.Ports) > 16 {
		return DiscoveryReport{}, errors.New("端口范围无效")
	}
	select {
	case e.ScanSlots <- struct{}{}:
		defer func() { <-e.ScanSlots }()
	case <-ctx.Done():
		return DiscoveryReport{}, ctx.Err()
	}
	scanCtx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	report := DiscoveryReport{Version: "asset-discovery/v1", IP: ip.String(), Requested: s.spec.Ports, Ports: []PortObservation{}, StartedAt: time.Now().UTC(), Limitations: "仅扫描声明的TCP端口；timeout不等于关闭；未做UDP、全端口、认证或漏洞利用。端口提示是猜测，HTTP头是自报信息；未跟随重定向或扩展关联资产。"}
	for _, port := range s.spec.Ports {
		if port < 1 || port > 65535 {
			return report, errors.New("端口无效")
		}
		if err := waitProbe(scanCtx); err != nil {
			break
		}
		if err := s.snapshot.Audit(scanCtx, "agent.discovery", map[string]any{"port": port, "stage": "connect"}); err != nil {
			return report, err
		}
		observation := probePort(scanCtx, ip, port)
		report.Ports = append(report.Ports, observation)
	}
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	report.Complete = len(report.Ports) == len(report.Requested)
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

// probePort 区分明确拒绝、超时和其他错误，仅对识别白名单端口发送只读HTTP。
func probePort(ctx context.Context, ip netip.Addr, port int) PortObservation {
	hints := map[int]string{21: "FTP", 22: "SSH", 25: "SMTP", 53: "DNS", 80: "HTTP", 110: "POP3", 143: "IMAP", 443: "HTTPS", 445: "SMB", 3306: "MySQL", 3389: "RDP", 5432: "PostgreSQL", 6379: "Redis", 8080: "HTTP", 8443: "HTTPS"}
	observation := PortObservation{Port: port, State: "error", Hint: hints[port]}
	address := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	dialer := net.Dialer{Timeout: 800 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		var ne net.Error
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			observation.State = "closed"
		case errors.As(err, &ne) && ne.Timeout():
			observation.State = "timeout"
			observation.Note = "连接超时，不能判定关闭"
		default:
			observation.Note = "连接失败，未取得可确认服务证据"
		}
		return observation
	}
	observation.State = "open"
	// 只读取已有欢迎banner，不发送协议命令或凭据。
	if port == 21 || port == 22 || port == 25 || port == 110 || port == 143 {
		_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
		buffer := make([]byte, 384)
		n, _ := conn.Read(buffer)
		observation.Banner = boundedText(strings.TrimSpace(string(buffer[:n])), 384)
		if strings.HasPrefix(observation.Banner, "SSH-") {
			observation.Service = "ssh"
			observation.Basis = "实际SSH欢迎banner，软件名称为服务自报"
		}
		if strings.HasPrefix(observation.Banner, "220") {
			if port == 21 {
				observation.Service = "ftp"
			} else if port == 25 {
				observation.Service = "smtp"
			}
			observation.Basis = "实际220欢迎banner；协议结合端口推测，未认证验证"
		}
	}
	_ = conn.Close()
	if port == 80 || port == 8080 || port == 443 || port == 8443 {
		if err := waitProbe(ctx); err != nil {
			observation.Note = "指纹采集被取消或超时"
			return observation
		}
		fingerprintHTTP(ctx, ip, port, &observation)
	}
	if observation.Service == "" {
		observation.Note = boundedText(observation.Note+" 仅确认TCP连接，服务提示不等于指纹。", 256)
	}
	return observation
}

// fingerprintHTTP 对授权IP端口根路径执行一次GET，正常验证TLS且禁止重定向和代理。
func fingerprintHTTP(ctx context.Context, ip netip.Addr, port int, out *PortObservation) {
	scheme := "http"
	if port == 443 || port == 8443 {
		scheme = "https"
	}
	target := scheme + "://" + net.JoinHostPort(ip.String(), strconv.Itoa(port)) + "/"
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	transport := &http.Transport{DisableCompression: true, MaxResponseHeaderBytes: 8192, TLSHandshakeTimeout: time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return
	}
	request.Header.Set("User-Agent", "AutonomousEngine-AuthorizedDiscovery/1")
	response, err := client.Do(request)
	if err != nil {
		out.Note = "TCP开放，但HTTP/TLS采集失败（可能证书不匹配、非HTTP或超时）；未绕过证书验证"
		return
	}
	defer response.Body.Close()
	out.Service = scheme
	out.HTTPStatus = response.StatusCode
	out.Server = boundedText(response.Header.Get("Server"), 128)
	out.PoweredBy = boundedText(response.Header.Get("X-Powered-By"), 128)
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
	if readErr != nil {
		out.Note = "HTTP响应已收到，正文读取不完整"
	}
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))
	inTitle := false
	for {
		token := tokenizer.Next()
		if token == html.ErrorToken {
			break
		}
		if token == html.StartTagToken {
			tag, _ := tokenizer.TagName()
			inTitle = string(tag) == "title"
		}
		if token == html.TextToken && inTitle {
			out.Title = boundedText(strings.TrimSpace(string(tokenizer.Text())), 192)
			break
		}
	}
	if out.Server != "" {
		out.Fingerprint = out.Server
		out.Basis = "HTTP Server响应头自报，不等于版本或漏洞已验证"
	}
	if out.PoweredBy != "" {
		out.Fingerprint = boundedText(out.Fingerprint+" / "+out.PoweredBy, 256)
		out.Basis = "HTTP响应头自报的软件/框架，可能隐藏或伪装"
	}
	if out.Fingerprint == "" && out.Title != "" {
		out.Basis = "仅取得页面标题，尚无可靠产品指纹"
	}
	if response.TLS != nil && len(response.TLS.VerifiedChains) > 0 {
		cert := response.TLS.VerifiedChains[0][0]
		out.TLS = boundedText("证书验证通过；签发者 "+cert.Issuer.CommonName+"；有效至 "+cert.NotAfter.UTC().Format(time.RFC3339), 256)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		out.Note = "重定向响应已记录，未访问Location目标"
	}
}

// discoveryObservations 从真实网络报告生成事实和主机资产，不创建漏洞Finding。
func discoveryObservations(target string, report DiscoveryReport) ([]contracts.Observation, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if len(data) > 16384 {
		return nil, errors.New("资产报告超过证据对象限制")
	}
	kind := "services_discovered"
	if !report.Complete {
		kind = "discovery_partial"
	}
	observations := []contracts.Observation{{Kind: "fact", Type: kind, Target: target, Content: string(data), Confidence: 1}}
	for _, p := range report.Ports {
		if p.State == "open" {
			observations = append(observations, contracts.Observation{Kind: "asset", Type: "network_host", Target: target, Content: string(data), Confidence: 1})
			break
		}
	}
	return observations, nil
}
