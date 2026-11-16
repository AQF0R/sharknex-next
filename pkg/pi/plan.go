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
	"time"
)

var (
	ErrPlanConfig      = errors.New("Pi 规划配置无效或未启用")
	ErrPlanInput       = errors.New("Pi 规划目标、授权范围或预算无效")
	ErrPlanInvalid     = errors.New("Pi 计划无效：结构、范围、依赖或事实产出不符合约束")
	ErrPlanBudget      = errors.New("Pi 规划预算已用尽")
	ErrPlanUnsupported = errors.New("当前授权工具无法完成目标；仅支持有限TCP探测和只读HTTP观察")
	ErrPlanModel       = errors.New("Pi 规划模型调用失败，请检查提供方配置与可用性")
	ErrPlanProtocol    = errors.New("Pi 规划子进程或协议无效")
)

const (
	maxPlanBytes = 65536
	maxPlanGoals = 16
	maxPlanTasks = 128
	planTimeout  = 45 * time.Second
)

type planningFrame struct {
	Type         string          `json:"type"`                    // 固定协议类别
	ID           string          `json:"id,omitempty"`            // 本次工具调用标识
	Name         string          `json:"name,omitempty"`          // 仅允许规划工具
	Args         json.RawMessage `json:"args,omitempty"`          // 不可信结构化计划
	Turn         int             `json:"turn,omitempty"`          // 开始的模型轮次
	Turns        int             `json:"turns,omitempty"`         // 用量归属轮次
	Tokens       int             `json:"tokens,omitempty"`        // 累计Token用量
	InputTokens  int             `json:"input_tokens,omitempty"`  // 累计输入Token
	OutputTokens int             `json:"output_tokens,omitempty"` // 累计输出Token
	Code         string          `json:"code,omitempty"`          // 固定失败枚举
}

// Plan 生成待服务端二次校验的计划，不执行网络工具、不落库也不推进运行状态。
func (e *Executor) Plan(ctx context.Context, spec contracts.RunSpec) (*contracts.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || !e.Config.Enabled || e.Slots == nil || cap(e.Slots) == 0 {
		return nil, ErrPlanConfig
	}
	config := e.Config
	if err := config.Validate(); err != nil {
		return nil, ErrPlanConfig
	}
	if err := validatePlanningSpec(spec); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(planTimeout)
	if spec.Deadline.Before(deadline) {
		deadline = spec.Deadline
	}
	planCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	select {
	case e.Slots <- struct{}{}:
		defer func() { <-e.Slots }()
	case <-planCtx.Done():
		return nil, planningContextError(ctx, planCtx)
	}
	if planCtx.Err() != nil {
		return nil, planningContextError(ctx, planCtx)
	}
	workspace, err := os.MkdirTemp("", "pi-plan-")
	if err != nil {
		return nil, ErrPlanProtocol
	}
	defer os.RemoveAll(workspace)
	plan, err := runPlanningProcess(planCtx, config, spec, workspace)
	if planCtx.Err() != nil {
		return nil, planningContextError(ctx, planCtx)
	}
	return plan, err
}

// planningContextError 区分调用方取消和本地规划时间预算，不携带外部响应。
func planningContextError(parent, child context.Context) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	return fmt.Errorf("%w: %w", ErrPlanBudget, child.Err())
}

// runPlanningProcess 使用受限临时目录和共享配置启动独立桥接，所有退出路径都回收进程。
func runPlanningProcess(ctx context.Context, config Config, spec contracts.RunSpec, workspace string) (*contracts.Plan, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, config.Node, "--experimental-strip-types", config.Script)
	cmd.Dir = workspace
	cmd.Env = []string{"HOME=" + workspace, "TMPDIR=" + workspace, "PATH=" + filepath.Dir(config.Node), "LANG=C.UTF-8"}
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrPlanProtocol
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, ErrPlanProtocol
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return nil, ErrPlanProtocol
	}
	waited := false
	defer func() {
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	}()
	// 只投影必要字段；不发送Run旧计划、授权声明原文、路径、共享事实或环境变量。
	input := map[string]any{
		"version": 1, "mode": "plan",
		"spec": map[string]any{
			"objective": spec.Objective,
			"scope":     map[string]any{"targets": spec.Scope.Targets, "tools": spec.Scope.Tools, "tcp_ports": spec.Scope.TCPPorts},
			"budget":    map[string]any{"max_tasks": spec.MaxTasks, "max_workers": spec.MaxWorkers, "deadline": spec.Deadline, "max_goals": min(maxPlanGoals, spec.MaxTasks/2), "max_plan_tasks": min(maxPlanTasks, spec.MaxTasks-1)},
		},
		"model": config.Model, "base_url": config.BaseURL, "api_key": config.APIKey,
		"max_turns": config.MaxTurns, "max_tools": config.MaxTools, "max_tokens": config.MaxTokens, "output_tokens": config.OutputTokens,
	}
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(input); err != nil {
		return nil, ErrPlanProtocol
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxPlanBytes+4096)
	usage := &contracts.PlanUsage{CostKnown: false}
	var plan *contracts.Plan
	total, usageTurn := 0, 0
	done := false
	seen := map[string]bool{}
	for scanner.Scan() {
		raw := scanner.Bytes()
		total += len(raw) + 1
		if total > 262144 || done {
			return nil, ErrPlanProtocol
		}
		var f planningFrame
		if strictPlanningJSON(raw, &f, maxPlanBytes+4096) != nil || !planningFrameShape(raw, f.Type) {
			return nil, ErrPlanProtocol
		}
		switch f.Type {
		case "turn":
			if plan != nil || f.Turn != usage.Turns+1 || usageTurn != usage.Turns {
				return nil, ErrPlanProtocol
			}
			if f.Turn > config.MaxTurns || usage.Tokens >= config.MaxTokens {
				return nil, ErrPlanBudget
			}
			usage.Turns = f.Turn
		case "usage":
			if usage.Turns < 1 || usageTurn == usage.Turns || f.Turns != usage.Turns || f.Tokens < usage.Tokens || f.InputTokens < usage.InputTokens || f.OutputTokens < usage.OutputTokens || f.InputTokens > f.Tokens || f.OutputTokens > f.Tokens-f.InputTokens {
				return nil, ErrPlanProtocol
			}
			if f.Tokens > config.MaxTokens || f.OutputTokens-usage.OutputTokens > config.OutputTokens {
				return nil, ErrPlanBudget
			}
			usageTurn = f.Turns
			usage.Tokens, usage.InputTokens, usage.OutputTokens = f.Tokens, f.InputTokens, f.OutputTokens
		case "tool":
			if usage.Turns < 1 || usageTurn != usage.Turns || plan != nil || f.ID == "" || len(f.ID) > 100 || seen[f.ID] {
				return nil, ErrPlanProtocol
			}
			usage.ToolCalls++
			if usage.ToolCalls > config.MaxTools {
				return nil, ErrPlanBudget
			}
			seen[f.ID] = true
			switch f.Name {
			case "submit_plan":
				plan, err = decodePlanningOutput(f.Args, spec)
				if err != nil {
					return nil, err
				}
				if err := encoder.Encode(map[string]any{"id": f.ID, "result": "计划已接收，仍需服务端验证和阶段复核"}); err != nil {
					return nil, ErrPlanProtocol
				}
			case "finish":
				var refusal struct {
					Reason string `json:"reason"` // 固定拒绝类别
				}
				if strictPlanningJSON(f.Args, &refusal, 256) != nil || !planningObjectShape(f.Args, []string{"reason"}, nil) {
					return nil, ErrPlanInvalid
				}
				switch refusal.Reason {
				case "unsupported_objective", "insufficient_scope":
					return nil, ErrPlanUnsupported
				case "insufficient_budget":
					return nil, ErrPlanBudget
				default:
					return nil, ErrPlanInvalid
				}
			default:
				return nil, ErrPlanInvalid
			}
		case "done":
			if plan == nil || usageTurn != usage.Turns || usage.ToolCalls != 1 {
				return nil, ErrPlanInvalid
			}
			done = true
			if err := stdin.Close(); err != nil {
				return nil, ErrPlanProtocol
			}
		case "error":
			switch f.Code {
			case "budget":
				return nil, ErrPlanBudget
			case "invalid_plan":
				return nil, ErrPlanInvalid
			case "unsupported":
				return nil, ErrPlanUnsupported
			case "model":
				return nil, ErrPlanModel
			default:
				return nil, ErrPlanProtocol
			}
		default:
			return nil, ErrPlanProtocol
		}
	}
	if scanner.Err() != nil || !done {
		return nil, ErrPlanProtocol
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		return nil, ErrPlanProtocol
	}
	plan.Usage = usage
	return plan, nil
}

// planningFrameShape 为各协议类型只接受精确字段名，拒绝字段混用。
func planningFrameShape(raw []byte, kind string) bool {
	fields := map[string][]string{
		"turn":  {"type", "turn"},
		"usage": {"type", "turns", "tokens", "input_tokens", "output_tokens"},
		"tool":  {"type", "id", "name", "args"},
		"done":  {"type"},
		"error": {"type", "code"},
	}
	required, ok := fields[kind]
	return ok && planningObjectShape(raw, required, nil)
}
