package httpapi

import (
	"autonomous-engine/internal/config"
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/engine"
	"autonomous-engine/pkg/security"
	"autonomous-engine/pkg/version"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type API struct {
	Engine       *engine.Engine     // 领域服务
	Config       config.Config      // 运行配置
	StartWorkers func(string) error // 启动共享运行时的逻辑 Worker
	Wake         func()             // 有界调度通知
	Ready        atomic.Bool        // 优雅关闭就绪标记
	Context      context.Context    // 进程生命周期
}
type reply struct {
	Data  any    `json:"data,omitempty"`  // 成功结果
	Error string `json:"error,omitempty"` // 稳定错误消息
}
type runURI struct {
	ID string `uri:"id" binding:"required,uuid"` // 路径标识
}
type listQuery struct {
	RunID    string `form:"run_id" binding:"omitempty,uuid"`           // Blackboard 运行参数
	WorkerID string `form:"worker_id" binding:"omitempty,uuid"`        // Worker 身份
	After    int64  `form:"after" binding:"gte=0"`                     // 事件恢复游标
	Limit    int    `form:"limit,default=100" binding:"gte=1,lte=200"` // 查询上限
}
type identityRequest struct {
	RunID    string `json:"run_id" binding:"required,uuid"`    // 绑定运行
	WorkerID string `json:"worker_id" binding:"required,uuid"` // 执行者身份
}

// Router 设置协议边界、恢复、鉴权与有界输入，不向 handler 暴露数据库。
func (a *API) Router() (*gin.Engine, error) {
	if a.Config.Mode == "dev" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	binding.EnableDecoderDisallowUnknownFields = true
	r := gin.New()
	if er := r.SetTrustedProxies(nil); er != nil {
		return nil, er
	}
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { c.AbortWithStatusJSON(500, reply{Error: "服务内部错误"}) }))
	limiter := rate.NewLimiter(100, 200)
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "no-store")
		c.Header("X-Frame-Options", "DENY")
		if !limiter.Allow() {
			c.AbortWithStatusJSON(429, reply{Error: "请求过于频繁"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, reply{Data: gin.H{"status": "ok", "mode": a.Config.Mode, "version": version.Version}})
	})
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if !a.Ready.Load() || a.Engine.Repo.Ping(ctx) != nil {
			c.JSON(503, reply{Error: "服务未就绪"})
			return
		}
		c.JSON(200, reply{Data: "ready"})
	})
	control := r.Group("/api/v1", a.controlAuth())
	control.POST("/runs", a.createRun)
	control.GET("/runs", a.listRuns)
	control.GET("/runs/:id", a.getRun)
	control.POST("/runs/:id/start", a.startRun)
	control.POST("/runs/:id/cancel", a.cancelRun)
	control.GET("/runs/:id/status", a.getRun)
	control.GET("/runs/:id/result", a.result)
	control.GET("/runs/:id/ws", a.websocket)
	for _, kind := range []string{"tasks", "goals", "workers", "assets", "facts", "evidence", "findings", "proposals", "events"} {
		control.GET("/runs/:id/"+kind, a.listRun(kind))
	}
	internal := r.Group("/internal")
	internal.POST("/workers/register", a.controlAuth(), a.register)
	internal.POST("/workers/heartbeat", a.heartbeat)
	internal.POST("/workers/claim", a.claim)
	internal.POST("/tasks/:id/heartbeat", a.renew)
	internal.POST("/tasks/:id/context", a.snapshot)
	for _, action := range []string{"complete", "fail", "cancel"} {
		internal.POST("/tasks/:id/"+action, a.submit(action))
	}
	internal.POST("/proposals", a.propose)
	internal.POST("/blackboard/proposals", a.propose)
	for _, kind := range []string{"assets", "facts", "evidence", "findings"} {
		internal.GET("/blackboard/"+kind, a.boardList(kind))
		internal.POST("/blackboard/"+kind, a.boardAppend(kind))
	}
	a.consoleRoutes(r)
	a.Ready.Store(true)
	return r, nil
}

// token 读取 Authorization，不允许查询参数传递秘密。
func token(c *gin.Context) string { return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ") }

// controlAuth 只接受控制端凭证，不与 Worker token 混用。
func (a *API) controlAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || !security.Equal(security.Hash(token(c)), security.Hash(a.Config.APIToken)) {
			c.AbortWithStatusJSON(401, reply{Error: "认证失败"})
			return
		}
		c.Next()
	}
}

// fail 将业务错误映射为稳定状态，隐藏 SQL、路径和内部凭证。
func fail(c *gin.Context, er error) {
	code := 500
	message := "服务内部错误"
	switch {
	case errors.Is(er, engine.ErrInvalid):
		code = 400
		message = er.Error()
	case errors.Is(er, engine.ErrForbidden):
		code = 403
		message = er.Error()
	case errors.Is(er, engine.ErrNotFound):
		code = 404
		message = er.Error()
	case errors.Is(er, engine.ErrConflict):
		code = 409
		message = er.Error()
	case errors.Is(er, engine.ErrEmpty):
		code = 204
		message = ""
	default:
		slog.Error("API 操作失败", "request_id", uuid.NewString(), "error", er)
	}
	if code == 204 {
		c.Status(code)
		return
	}
	c.JSON(code, reply{Error: message})
}

// bind 统一调用 Gin 的 JSON 绑定与校验。
func bind(c *gin.Context, v any) bool {
	if er := c.ShouldBindJSON(v); er != nil {
		c.JSON(400, reply{Error: "请求 JSON 或字段校验失败"})
		return false
	}
	return true
}

// uri 使用 Gin 验证 UUID 路径引用。
func uri(c *gin.Context) (string, bool) {
	var p runURI
	if c.ShouldBindUri(&p) != nil {
		c.JSON(400, reply{Error: "路径标识无效"})
		return "", false
	}
	return p.ID, true
}

// query 绑定有限分页和身份参数。
func query(c *gin.Context) (listQuery, bool) {
	var q listQuery
	if c.ShouldBindQuery(&q) != nil {
		c.JSON(400, reply{Error: "查询参数无效"})
		return q, false
	}
	return q, true
}

// createRun 通过服务校验任务图并创建运行。
func (a *API) createRun(c *gin.Context) {
	var s contracts.RunSpec
	if !bind(c, &s) {
		return
	}
	r, er := a.Engine.CreateRun(c.Request.Context(), s)
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(201, reply{Data: r})
}

// getRun 返回运行状态与不可变授权快照。
func (a *API) getRun(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	r, er := a.Engine.GetRun(c.Request.Context(), id)
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: r})
}

// startRun 先持久化启动，再注册逻辑 Worker。
func (a *API) startRun(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	if er := a.Engine.StartRun(c.Request.Context(), id); er != nil {
		fail(c, er)
		return
	}
	if er := a.StartWorkers(id); er != nil {
		fail(c, er)
		return
	}
	a.Wake()
	run, err := a.Engine.GetRun(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, reply{Data: run.Status})
}

// cancelRun 撤销全部子任务的执行资格。
func (a *API) cancelRun(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	if er := a.Engine.CancelRun(c.Request.Context(), id); er != nil {
		fail(c, er)
		return
	}
	a.Wake()
	c.JSON(200, reply{Data: "cancelled"})
}

// listRun 返回当前运行的有界资源列表。
func (a *API) listRun(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := uri(c)
		if !ok {
			return
		}
		q, ok := query(c)
		if !ok {
			return
		}
		v, er := a.Engine.ListResource(c.Request.Context(), id, kind, q.After, q.Limit)
		if er != nil {
			fail(c, er)
			return
		}
		c.JSON(200, reply{Data: v})
	}
}

// result 返回可归档的版本化结果契约。
func (a *API) result(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	v, er := a.Engine.Result(c.Request.Context(), id)
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: v})
}

// register 只允许控制端为特定 Run 注册 Worker。
func (a *API) register(c *gin.Context) {
	var s contracts.WorkerSpec
	if !bind(c, &s) {
		return
	}
	v, er := a.Engine.RegisterWorker(c.Request.Context(), s)
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(201, reply{Data: v})
}

// heartbeat 更新已认证 Worker 的持久心跳。
func (a *API) heartbeat(c *gin.Context) {
	var s identityRequest
	if !bind(c, &s) {
		return
	}
	if er := a.Engine.Heartbeat(c.Request.Context(), s.RunID, s.WorkerID, token(c)); er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: "ok"})
}

// claim 返回数据库授予的任务及一次租约凭据。
func (a *API) claim(c *gin.Context) {
	var s identityRequest
	if !bind(c, &s) {
		return
	}
	v, er := a.Engine.ClaimTask(c.Request.Context(), s.RunID, s.WorkerID, token(c))
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: v})
}

// renew 校验 Worker、token 和 generation 后续租。
func (a *API) renew(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	var p contracts.LeaseProof
	if !bind(c, &p) {
		return
	}
	if er := a.Engine.RenewLease(c.Request.Context(), id, p, token(c)); er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: "ok"})
}

// snapshot 提供与任务目标相关的有限事实快照。
func (a *API) snapshot(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	var p contracts.LeaseProof
	if !bind(c, &p) {
		return
	}
	v, er := a.Engine.Snapshot(c.Request.Context(), id, p, token(c))
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: v})
}

// submit 将协议动作映射为领域状态，Worker 无权任意修改状态。
func (a *API) submit(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := uri(c)
		if !ok {
			return
		}
		var s contracts.Submission
		if !bind(c, &s) {
			return
		}
		status := map[string]string{"complete": "completed", "fail": "failed", "cancel": "cancelled"}[action]
		if er := a.Engine.Submit(c.Request.Context(), id, token(c), status, s); er != nil {
			fail(c, er)
			return
		}
		a.Wake()
		c.JSON(200, reply{Data: "accepted"})
	}
}

type boardRequest struct {
	TaskID               string `json:"task_id" binding:"required,uuid"` // 来源任务
	contracts.Submission        // 证明、观察与建议
}

// boardList 在服务端验证 Worker 对 run_id 的访问权限。
func (a *API) boardList(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		q, ok := query(c)
		if !ok {
			return
		}
		if er := a.Engine.RequireRunAccess(c.Request.Context(), q.RunID, q.WorkerID, token(c)); er != nil {
			fail(c, er)
			return
		}
		v, er := a.Engine.ListResource(c.Request.Context(), q.RunID, kind, q.After, q.Limit)
		if er != nil {
			fail(c, er)
			return
		}
		c.JSON(200, reply{Data: v})
	}
}

// boardAppend 以租约绑定来源，不允许 Worker 伪造其他任务的共享记录。
func (a *API) boardAppend(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var s boardRequest
		if !bind(c, &s) {
			return
		}
		expected := map[string]string{"assets": "asset", "facts": "fact", "evidence": "evidence", "findings": "finding"}[kind]
		if len(s.Observations) == 0 {
			fail(c, engine.ErrInvalid)
			return
		}
		for _, o := range s.Observations {
			if o.Kind != expected {
				fail(c, engine.ErrInvalid)
				return
			}
		}
		if er := a.Engine.AppendObservations(c.Request.Context(), s.TaskID, token(c), s.Submission); er != nil {
			fail(c, er)
			return
		}
		a.Wake()
		c.JSON(201, reply{Data: "recorded"})
	}
}

// propose 只保存建议，由 Planner 在事务内审核后创建任务。
func (a *API) propose(c *gin.Context) {
	var s boardRequest
	if !bind(c, &s) {
		return
	}
	if er := a.Engine.Propose(c.Request.Context(), s.TaskID, token(c), s.Submission); er != nil {
		fail(c, er)
		return
	}
	a.Wake()
	c.JSON(201, reply{Data: "pending"})
}
