package main

import (
	"autonomous-engine/internal/app"
	"autonomous-engine/internal/config"
	"autonomous-engine/internal/httpapi"
	"autonomous-engine/internal/storage"
	"autonomous-engine/migrations"
	"autonomous-engine/pkg/engine"
	"autonomous-engine/pkg/version"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// main 只负责启动装配、退出码及生命周期，不承载领域决策。
func main() {
	if er := run(); er != nil {
		slog.Error("引擎退出", "error", er)
		os.Exit(1)
	}
}

// run 使用 dev 开发模式自动执行非破坏性版本迁移并提供安全的 HTTP 服务。
func run() error {
	mode := flag.String("mode", "", "运行模式：dev 或 production")
	migrate := flag.Bool("migrate", false, "执行版本化迁移后退出")
	showVersion := flag.Bool("version", false, "输出版本")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Version)
		return nil
	}
	if *mode != "" {
		if er := os.Setenv("ENGINE_MODE", *mode); er != nil {
			return er
		}
	}
	cfg, er := config.Load()
	if er != nil {
		return er
	}
	root, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(root)
	defer cancel()
	store, er := storage.Open(cfg.DatabaseURL)
	if er != nil {
		return er
	}
	defer func() {
		if er := store.Close(); er != nil {
			slog.Error("数据库关闭失败", "error", er)
		}
	}()
	if cfg.Mode == "dev" || *migrate {
		if er := migrations.Apply(ctx, store.DB); er != nil {
			return fmt.Errorf("迁移失败: %w", er)
		}
	}
	if *migrate {
		return nil
	}
	var client *redis.Client
	if cfg.RedisURL != "" {
		opts, er := redis.ParseURL(cfg.RedisURL)
		if er != nil {
			return errors.New("REDIS_URL 无效")
		}
		opts.DialTimeout = time.Second
		opts.ReadTimeout = time.Second
		opts.WriteTimeout = time.Second
		client = redis.NewClient(opts)
		defer func() {
			if er := client.Close(); er != nil {
				slog.Error("Redis 关闭失败", "error", er)
			}
		}()
	}
	eng := &engine.Engine{PiEnabled: cfg.Pi.Enabled, Repo: store, Lease: cfg.Lease, Offline: cfg.Offline, Objects: storage.Objects{Directory: filepath.Join(cfg.Workspace, "objects")}}
	runtime := app.NewRuntime(ctx, eng, cfg)
	eng.Planner = runtime.Pi
	api := &httpapi.API{Engine: eng, Config: cfg, StartWorkers: runtime.Start, Wake: runtime.Wake, Context: ctx}
	router, er := api.Router()
	if er != nil {
		return er
	}
	if er := runtime.Resume(); er != nil {
		return er
	}
	outbox := &storage.Outbox{Store: store, Redis: client}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(cfg.Recovery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				workCtx, done := context.WithTimeout(ctx, 10*time.Second)
				if er := eng.Tick(workCtx); er != nil && !errors.Is(er, context.Canceled) {
					slog.Error("恢复与规划扫描失败", "error", er)
				}
				if er := outbox.Publish(workCtx); er != nil && !errors.Is(er, context.Canceled) {
					slog.Error("Outbox 扫描失败", "error", er)
				}
				done()
				runtime.Wake()
			}
		}
	}()
	if client != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := client.Subscribe(ctx, "scheduler:wakeup")
			defer func() {
				if er := sub.Close(); er != nil {
					slog.Warn("关闭事件订阅失败", "error", er)
				}
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-sub.Channel():
					if !ok {
						return
					}
					runtime.Wake()
				}
			}
		}()
	}
	server := &http.Server{Addr: cfg.Listen, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	slog.Info("协同引擎已启动", "mode", cfg.Mode, "listen", cfg.Listen, "version", version.Version, "redis_notifications", client != nil)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErrors:
		cancel()
	}
	api.Ready.Store(false)
	cancel()
	shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	shutdownErr := server.Shutdown(shutdownCtx)
	runtime.Wait()
	wg.Wait()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return shutdownErr
}
