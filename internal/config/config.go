package config

import (
	"autonomous-engine/pkg/pi"
	"errors"
	"fmt"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
	"net"
	"os"
	"time"
)

type Config struct {
	Pi          pi.Config     `yaml:"pi" envPrefix:"PI_"`               // 可选真实 Agent 配置
	Mode        string        `yaml:"mode" env:"ENGINE_MODE"`           // dev 或 production
	Listen      string        `yaml:"listen" env:"ENGINE_LISTEN"`       // 监听地址
	DatabaseURL string        `yaml:"database_url" env:"DATABASE_URL"`  // 数据库连接
	RedisURL    string        `yaml:"redis_url" env:"REDIS_URL"`        // 可选通知服务
	APIToken    string        `yaml:"-" env:"ENGINE_API_TOKEN"`         // 控制端凭证
	Workspace   string        `yaml:"workspace" env:"ENGINE_WORKSPACE"` // 隔离工作空间
	Workers     int           `yaml:"workers" env:"ENGINE_WORKERS"`     // 每 Run 内置 Worker 数量
	Heartbeat   time.Duration `yaml:"heartbeat" env:"ENGINE_HEARTBEAT"` // 心跳周期
	Offline     time.Duration `yaml:"offline" env:"ENGINE_OFFLINE"`     // 离线阈值
	Lease       time.Duration `yaml:"lease" env:"ENGINE_LEASE"`         // 租约时长
	Renew       time.Duration `yaml:"renew" env:"ENGINE_RENEW"`         // 续租周期
	Recovery    time.Duration `yaml:"recovery" env:"ENGINE_RECOVERY"`   // 恢复扫描周期
}

// Load 按默认值、YAML、dotenv、进程环境顺序加载并校验配置。
func Load() (Config, error) {
	c := Config{Pi: pi.DefaultConfig(), Mode: "dev", Listen: "127.0.0.1:8080", Workspace: ".dev/workspaces", Workers: 3, Heartbeat: 10 * time.Second, Offline: 30 * time.Second, Lease: 60 * time.Second, Renew: 20 * time.Second, Recovery: 10 * time.Second}
	b, e := os.ReadFile("config.yaml")
	if e == nil {
		if e = yaml.Unmarshal(b, &c); e != nil {
			return c, errors.New("config.yaml 格式无效")
		}
	} else if !os.IsNotExist(e) {
		return c, e
	}
	if e = godotenv.Load(); e != nil && !os.IsNotExist(e) {
		return c, errors.New(".env 格式无效")
	}
	if e = env.Parse(&c); e != nil {
		return c, errors.New("环境变量类型无效，请检查配置项")
	}
	if c.Mode != "dev" && c.Mode != "production" {
		return c, errors.New("ENGINE_MODE 必须为 dev 或 production")
	}
	host, _, e := net.SplitHostPort(c.Listen)
	if e != nil {
		return c, errors.New("监听地址无效")
	}
	if c.Mode == "dev" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return c, errors.New("dev 模式仅允许回环监听")
	}
	if c.DatabaseURL == "" || len(c.APIToken) < 32 {
		return c, errors.New("必须配置 DATABASE_URL 和至少 32 字符的 ENGINE_API_TOKEN")
	}
	if c.Workers < 0 || c.Workers > 64 || c.Heartbeat < time.Second || c.Offline <= c.Heartbeat || c.Renew < time.Second || c.Lease <= c.Renew || c.Recovery < time.Second {
		return c, fmt.Errorf("Worker 数量或定时参数无效")
	}
	if e := c.Pi.Validate(); e != nil {
		return c, e
	}
	return c, nil
}
