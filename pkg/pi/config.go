package pi

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	Enabled      bool     `yaml:"enabled" env:"ENABLED"`                              // 显式启用真实执行
	Node         string   `yaml:"node" env:"NODE"`                                    // 可信 Node 可执行文件
	Script       string   `yaml:"script" env:"SCRIPT"`                                // 固定桥接入口
	Model        string   `yaml:"model" env:"MODEL"`                                  // 模型标识
	BaseURL      string   `yaml:"base_url" env:"BASE_URL"`                            // 运维配置的模型端点
	APIKey       string   `yaml:"-" env:"API_KEY"`                                    // 不落盘的模型凭证
	MaxTurns     int      `yaml:"max_turns" env:"MAX_TURNS"`                          // 单任务模型轮次上限
	MaxTools     int      `yaml:"max_tools" env:"MAX_TOOLS"`                          // 单任务工具调用上限
	MaxTokens    int      `yaml:"max_tokens" env:"MAX_TOKENS"`                        // 单任务已用 Token 上限
	OutputTokens int      `yaml:"output_tokens" env:"OUTPUT_TOKENS"`                  // 单轮最大输出
	Concurrency  int      `yaml:"concurrency" env:"CONCURRENCY"`                      // 进程级 Pi 并发上限
	AllowedCIDRs []string `yaml:"allowed_cidrs" env:"ALLOWED_CIDRS" envSeparator:","` // 特许靶场网络
}

// DefaultConfig 返回默认关闭且有界的模型执行配置。
func DefaultConfig() Config {
	return Config{Node: "node", Script: "pi-adapter/bridge.ts", MaxTurns: 8, MaxTools: 8, MaxTokens: 16000, OutputTokens: 2048, Concurrency: 2}
}

// Validate 验证 SDK 装配和预算，不打印端点秘密或凭证。
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Model == "" || c.APIKey == "" {
		return errors.New("启用 Pi 需要 PI_MODEL、PI_BASE_URL 和 PI_API_KEY")
	}
	u, er := url.Parse(c.BaseURL)
	if er != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return errors.New("PI_BASE_URL 必须为 HTTPS 或本地回环 HTTP")
	}
	if c.MaxTurns < 1 || c.MaxTurns > 32 || c.MaxTools < 1 || c.MaxTools > 32 || c.MaxTokens < 256 || c.MaxTokens > 128000 || c.OutputTokens < 64 || c.OutputTokens > 8192 || c.OutputTokens > c.MaxTokens || c.Concurrency < 1 || c.Concurrency > 8 {
		return errors.New("Pi 执行预算无效")
	}
	for _, raw := range c.AllowedCIDRs {
		if _, er := netip.ParsePrefix(raw); er != nil {
			return errors.New("PI_ALLOWED_CIDRS 无效")
		}
	}
	node, er := exec.LookPath(c.Node)
	if er != nil {
		return errors.New("找不到 PI_NODE")
	}
	c.Node, er = filepath.Abs(node)
	if er != nil {
		return er
	}
	c.Script, er = filepath.Abs(c.Script)
	if er != nil {
		return er
	}
	if !strings.HasSuffix(c.Script, ".ts") {
		return errors.New("PI_SCRIPT 必须为可信 TypeScript 桥接文件")
	}
	if _, er := os.Stat(c.Script); er != nil {
		return errors.New("Pi 桥接文件不存在")
	}
	if _, er := os.Stat(filepath.Join(filepath.Dir(c.Script), "node_modules", "@earendil-works", "pi-agent-core", "package.json")); er != nil {
		return errors.New("请先运行 npm --prefix pi-adapter ci")
	}
	return nil
}
