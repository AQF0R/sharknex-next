package contracts

// Plan 表示模型生成、尚需服务端完整校验的阶段化计划。
type Plan struct {
	Summary string     `json:"summary"`         // 面向用户的计划摘要
	Goals   []GoalSpec `json:"goals"`           // 有向无环的阶段集合
	Tasks   []TaskSpec `json:"tasks"`           // 真实工具执行任务
	Usage   *PlanUsage `json:"usage,omitempty"` // 规划模型调用的审计用量
}

// PlanUsage 记录规划资源消耗，未知价格不推算费用。
type PlanUsage struct {
	Turns        int  `json:"turns"`         // 模型轮数
	ToolCalls    int  `json:"tool_calls"`    // 工具调用数
	Tokens       int  `json:"tokens"`        // 合计令牌数
	InputTokens  int  `json:"input_tokens"`  // 输入令牌数
	OutputTokens int  `json:"output_tokens"` // 输出令牌数
	CostKnown    bool `json:"cost_known"`    // 是否已掌握真实费用
}

// StageSummary 是面向界面的只读阶段进度，不授予 Worker 修改阶段的权限。
type StageSummary struct {
	ID             string   `json:"id"`                       // 持久化 Goal 标识
	Key            string   `json:"key"`                      // 计划中的阶段键
	Name           string   `json:"name"`                     // 可读阶段名称
	Status         string   `json:"status"`                   // pending/running/reviewing/needs_work/achieved/failed/cancelled
	DependsOn      []string `json:"depends_on"`               // 前置阶段键
	Targets        []string `json:"targets"`                  // 必须覆盖的精确目标
	RequiredFacts  []string `json:"required_facts"`           // 必须具备的事实类别
	CompletedTasks int      `json:"completed_tasks"`          // 已完成的阶段工作任务数
	TotalTasks     int      `json:"total_tasks"`              // 阶段工作任务总数
	ReviewTaskID   string   `json:"review_task_id,omitempty"` // 只读复核任务标识
	ReviewerID     string   `json:"reviewer_id,omitempty"`    // 认领复核的执行者
	ReviewMode     string   `json:"review_mode,omitempty"`    // cross_review 或明确标注的 self_review
	Reason         string   `json:"reason,omitempty"`         // 阶段验收未通过或终止原因
}
