package pi

import (
	"autonomous-engine/pkg/contracts"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Executor struct {
	Config    Config        // 只读服务端配置
	Slots     chan struct{} // 共享进程并发预算
	ScanSlots chan struct{} // 进程内只允许一个串行网络探测批次
}
type frame struct {
	Type         string          `json:"type"`          // 固定协议类别
	ID           string          `json:"id"`            // 工具调用标识
	Name         string          `json:"name"`          // 工具标识
	Args         json.RawMessage `json:"args"`          // 严格校验的参数
	Turn         int             `json:"turn"`          // 当前模型轮次
	Turns        int             `json:"turns"`         // 已完成轮次
	Tokens       int             `json:"tokens"`        // 累计 Token
	InputTokens  int             `json:"input_tokens"`  // 输入 Token
	OutputTokens int             `json:"output_tokens"` // 输出 Token
}
type session struct {
	spec     contracts.TaskSpec   // 绑定的任务
	snapshot *contracts.Snapshot  // 有限事实与审计
	result   contracts.Submission // 仅由真实工具生成
	calls    int                  // 工具请求计数
	finished bool                 // 显式完成标记
	observed bool                 // 是否取得真实观察
	seen     map[string]bool      // 防止重复协议请求
	failure  error                // 仅保存可公开的本地网络诊断
}

// Type 标识通过 TaskSpec 显式选择的 Pi 执行器。
func (e *Executor) Type() string { return "pi" }

// Validate 未启用时明确失败，不退回本地模拟执行。
func (e *Executor) Validate(ctx context.Context, s *contracts.TaskSpec) error {
	if er := ctx.Err(); er != nil {
		return er
	}
	if !e.Config.Enabled || s.Executor != "pi" {
		return errors.New("Pi 未启用")
	}
	if !strings.HasPrefix(s.Target, "http://") && !strings.HasPrefix(s.Target, "https://") {
		return errors.New("Pi 只接受明确 HTTP 目标")
	}
	return nil
}

// Cancel 取消通过 Execute 的 context 杀死并回收专属 Node 进程。
func (e *Executor) Cancel(ctx context.Context, _ string) error { return ctx.Err() }

// Execute 为每个任务代数创建独立 Pi 会话，所有工具仍由 Go 授权并执行。
func (e *Executor) Execute(ctx context.Context, in *contracts.Snapshot) (*contracts.Submission, error) {
	var sp contracts.TaskSpec
	if er := json.Unmarshal(in.Task.Input, &sp); er != nil {
		return nil, er
	}
	if er := e.Validate(ctx, &sp); er != nil {
		return nil, er
	}
	select {
	case e.Slots <- struct{}{}:
		defer func() { <-e.Slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if in.Audit == nil {
		return nil, errors.New("Pi 缺少租约审计边界")
	}
	facts := make([]string, 0, len(in.Facts))
	for _, f := range in.Facts {
		facts = append(facts, f.ID)
	}
	if er := in.Audit(ctx, "agent.started", map[string]any{"executor": "pi", "model": e.Config.Model, "fact_ids": facts}); er != nil {
		return nil, er
	}
	s := session{spec: sp, snapshot: in, seen: map[string]bool{}}
	result, er := e.run(ctx, &s)
	kind := "agent.completed"
	if er != nil {
		kind = "agent.failed"
	}
	payload := map[string]any{"tool_calls": s.calls, "observed": s.observed}
	if er != nil {
		payload["reason"] = FailureMessage(er)
	}
	auditErr := in.Audit(ctx, kind, payload)
	if er != nil {
		return nil, er
	}
	if auditErr != nil {
		return nil, auditErr
	}
	return result, nil
}

// run 使用固定入口而非 Shell；子进程只继承最小环境，输出和等待均有界。
func (e *Executor) run(ctx context.Context, s *session) (*contracts.Submission, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Config.Node, "--experimental-strip-types", e.Config.Script)
	cmd.Dir = s.snapshot.Workspace
	cmd.Env = []string{"HOME=" + s.snapshot.Workspace, "TMPDIR=" + s.snapshot.Workspace, "PATH=" + filepath.Dir(e.Config.Node), "LANG=C.UTF-8"}
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	stdin, er := cmd.StdinPipe()
	if er != nil {
		return nil, er
	}
	stdout, er := cmd.StdoutPipe()
	if er != nil {
		return nil, er
	}
	if er = cmd.Start(); er != nil {
		return nil, errors.New("Pi 子进程启动失败")
	}
	defer func() { cancel(); _ = stdin.Close(); _ = cmd.Wait() }()
	encoder := json.NewEncoder(stdin)
	input := map[string]any{"version": 1, "spec": s.spec, "model": e.Config.Model, "base_url": e.Config.BaseURL, "api_key": e.Config.APIKey, "max_turns": e.Config.MaxTurns, "max_tools": e.Config.MaxTools, "max_tokens": e.Config.MaxTokens, "output_tokens": e.Config.OutputTokens}
	if er := encoder.Encode(input); er != nil {
		return nil, errors.New("Pi 任务输入失败")
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 65536)
	total, turns, tokens := 0, 0, 0
	for scanner.Scan() {
		total += len(scanner.Bytes())
		if total > 262144 {
			return nil, errors.New("Pi 协议输出超过上限")
		}
		var f frame
		if er := json.Unmarshal(scanner.Bytes(), &f); er != nil {
			return nil, errors.New("Pi 协议消息无效")
		}
		switch f.Type {
		case "turn":
			if f.Turn != turns+1 || f.Turn > e.Config.MaxTurns || tokens >= e.Config.MaxTokens {
				return nil, errors.New("Pi 模型轮次超过预算")
			}
			turns = f.Turn
			if er := s.snapshot.Audit(ctx, "agent.turn", map[string]any{"turn": turns}); er != nil {
				return nil, er
			}
		case "usage":
			if f.Turns != turns || f.Tokens < tokens || f.InputTokens < 0 || f.OutputTokens < 0 {
				return nil, errors.New("Pi 用量消息无效")
			}
			tokens = f.Tokens
			if er := s.snapshot.Audit(ctx, "agent.usage", map[string]any{"turns": turns, "tokens": tokens, "input_tokens": f.InputTokens, "output_tokens": f.OutputTokens, "cost_known": false}); er != nil {
				return nil, er
			}
		case "tool":
			s.calls++
			if s.calls > e.Config.MaxTools || f.ID == "" || s.seen[f.ID] || s.finished {
				return nil, errors.New("Pi 工具请求超限或重复")
			}
			s.seen[f.ID] = true
			value, toolErr := e.tool(ctx, s, f)
			success := toolErr == nil
			payload := map[string]any{"call": s.calls, "success": success, "tool": safeTool(f.Name)}
			if toolErr != nil {
				payload["reason"] = FailureMessage(toolErr)
			}
			if er := s.snapshot.Audit(ctx, "agent.tool_result", payload); er != nil {
				return nil, er
			}
			reply := map[string]any{"id": f.ID, "result": value}
			if toolErr != nil {
				reply = map[string]any{"id": f.ID, "error": toolErr.Error()}
			}
			if er := encoder.Encode(reply); er != nil {
				return nil, errors.New("Pi 工具响应失败")
			}
		case "done":
			if !s.finished || !s.observed {
				return nil, errors.New("Pi 未产生真实观察")
			}
			if er := stdin.Close(); er != nil {
				return nil, er
			}
			if er := cmd.Wait(); er != nil {
				return nil, errors.New("Pi 子进程异常结束")
			}
			return &s.result, nil
		case "error":
			if s.failure != nil {
				return nil, s.failure
			}
			return nil, errors.New("Pi 模型执行失败或预算已用尽")
		default:
			return nil, errors.New("Pi 消息类别无效")
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, errors.New("Pi 协议提前结束")
}

// safeTool 将不可信工具名限制为固定审计枚举。
func safeTool(name string) string {
	switch name {
	case "read_context", "observe_http", "discover_services", "propose_task", "finish":
		return name
	default:
		return "unknown"
	}
}

// decodeArgs 拒绝未知字段、尾随数据和超大参数。
func decodeArgs(raw []byte, out any) error {
	if len(raw) > 4096 {
		return errors.New("工具参数超限")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if er := d.Decode(out); er != nil {
		return errors.New("工具参数无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("工具参数尾随数据无效")
	}
	return nil
}

// tool 只暴露当前任务快照、精确 URL 的只读观察和待审核建议。
func (e *Executor) tool(ctx context.Context, s *session, f frame) (any, error) {
	switch f.Name {
	case "read_context":
		if er := decodeArgs(f.Args, &struct{}{}); er != nil {
			return nil, er
		}
		// 不向模型暴露服务端路径、执行凭据或未筛选的模型字段。
		facts := make([]map[string]any, 0, 8)
		for _, fact := range s.snapshot.Facts {
			if len(facts) >= 8 {
				break
			}
			content := string(fact.Content)
			if len(content) > 1000 {
				content = content[:1000]
			}
			facts = append(facts, map[string]any{"id": fact.ID, "type": fact.FactType, "excerpt": content, "status": fact.Status, "truncated": len(fact.Content) > 1000})
		}
		return facts, nil
	case "observe_http":
		if !slices.Contains(s.spec.Tools, "http.observe") {
			return nil, errors.New("HTTP观察工具未授权")
		}
		if er := decodeArgs(f.Args, &struct{}{}); er != nil {
			return nil, er
		}
		if s.observed {
			return nil, errors.New("每任务最多一次 HTTP 观察")
		}
		if er := s.snapshot.Audit(ctx, "tool.called", map[string]any{"tool": "http.observe"}); er != nil {
			return nil, er
		}
		content, er := observeHTTP(ctx, s.spec.Target, e.Config.AllowedCIDRs)
		if er != nil {
			s.failure = er
			return nil, er
		}
		s.failure = nil
		s.observed = true
		kind := "http_observed"
		switch s.spec.Type {
		case "asset_discovery":
			kind = "service_identified"
		case "service_analysis", "web_analysis":
			kind = "analysis_completed"
		case "evidence_collection":
			kind = "evidence_collected"
		case "verification":
			kind = "verification_observed"
		}
		s.result.Observations = append(s.result.Observations, contracts.Observation{Kind: "fact", Type: kind, Target: s.spec.Target, Content: content, Confidence: 1})
		return json.RawMessage(content), nil
	case "discover_services":
		if !slices.Contains(s.spec.Tools, "tcp.discover") {
			return nil, errors.New("资产探测工具未授权")
		}
		if err := decodeArgs(f.Args, &struct{}{}); err != nil {
			return nil, err
		}
		if s.observed {
			return nil, errors.New("每任务仅允许一个探测批次")
		}
		if err := s.snapshot.Audit(ctx, "tool.called", map[string]any{"tool": "tcp.discover"}); err != nil {
			return nil, err
		}
		report, err := e.discoverServices(ctx, s)
		if err != nil {
			s.failure = err
			return nil, err
		}
		observations, err := discoveryObservations(s.spec.Target, report)
		if err != nil {
			return nil, err
		}
		s.result.Observations = append(s.result.Observations, observations...)
		s.observed = true
		s.failure = nil
		return report, nil
	case "propose_task":
		if slices.Contains(s.spec.Tools, "tcp.discover") {
			return nil, errors.New("资产探测不得自动扩展端口或派生任务")
		}
		var args struct {
			Type   string `json:"type"`   // 后续任务类型
			Reason string `json:"reason"` // 不可信建议依据
		}
		if er := decodeArgs(f.Args, &args); er != nil {
			return nil, er
		}
		if !s.observed || len(args.Reason) == 0 || len(args.Reason) > 500 || len(s.result.Proposals) >= 2 {
			return nil, errors.New("建议缺少观察依据或超过预算")
		}
		if args.Type != "service_analysis" && args.Type != "web_analysis" && args.Type != "evidence_collection" {
			return nil, errors.New("后续任务类型不允许")
		}
		proposal := s.spec
		proposal.Key = fmt.Sprintf("pi:%s:%s", s.snapshot.Task.ID, args.Type)
		proposal.Type = args.Type
		proposal.Name = "Pi 建议：" + args.Type
		proposal.Capabilities = []string{args.Type}
		proposal.Dependencies = nil
		proposal.DelayMS = 0
		for _, p := range s.result.Proposals {
			if p.Key == proposal.Key {
				return "建议已记录", nil
			}
		}
		s.result.Proposals = append(s.result.Proposals, proposal)
		return "建议已暂存，提交后由 Planner 审核", nil
	case "finish":
		var args struct {
			Summary string `json:"summary"` // 不可信模型摘要
		}
		if er := decodeArgs(f.Args, &args); er != nil {
			return nil, er
		}
		if !s.observed || len(args.Summary) == 0 || len(args.Summary) > 1000 {
			return nil, errors.New("完成前必须获得真实观察并提交有界摘要")
		}
		s.result.Message = args.Summary
		s.finished = true
		return "当前任务结束，不代表发现或确认漏洞", nil
	default:
		return nil, errors.New("工具未授权")
	}
}

// New 创建具有全局进程预算的共享只读适配器。
func New(c Config) *Executor {
	return &Executor{Config: c, Slots: make(chan struct{}, c.Concurrency), ScanSlots: make(chan struct{}, 1)}
}

// CheckWorkspace 限制桥接工作空间为已创建的绝对目录。
func CheckWorkspace(path string) (string, error) {
	absolute, er := filepath.Abs(path)
	if er != nil {
		return "", er
	}
	info, er := os.Stat(absolute)
	if er != nil || !info.IsDir() {
		return "", errors.New("Pi 工作空间无效")
	}
	return absolute, nil
}
