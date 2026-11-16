package contracts

import (
	"autonomous-engine/models"
	"context"
	"time"
)

type Authorization struct {
	Confirmed bool   `json:"confirmed"`                    // 控制端声明具有目标测试授权
	Statement string `json:"statement" binding:"max=1000"` // 与此Run精确范围绑定的授权依据
}
type Scope struct {
	TCPPorts      []int          `json:"tcp_ports,omitempty" binding:"max=16"`                    // 显式允许的TCP端口
	Authorization *Authorization `json:"authorization,omitempty"`                                 // 不跨Run隐式复用的声明
	Targets       []string       `json:"targets" yaml:"targets" binding:"required,min=1,max=100"` // 精确授权目标
	Tools         []string       `json:"tools,omitempty" yaml:"tools" binding:"max=32"` // 可选显式工具限制；规划后固定执行工具
}
type GoalSpec struct {
	DependsOn     []string `json:"depends_on,omitempty" binding:"max=100"` // 前置阶段键
	Targets       []string `json:"targets,omitempty" binding:"max=100"`    // 阶段必须覆盖的授权目标
	Key           string   `json:"key" binding:"required,max=100"`         // 计划内引用
	Name          string   `json:"name" binding:"required,max=255"`        // 目标名称
	RequiredFacts []string `json:"required_facts" binding:"max=32"`        // 成功所需事实类别
}
type DependencySpec struct {
	Key  string `json:"key" binding:"required"`                          // 前置任务键
	Type string `json:"type" binding:"required,oneof=success completed"` // 依赖条件
}
type TaskSpec struct {
	Ports        []int            `json:"ports,omitempty" binding:"max=16"`                      // 资产探测端口子集
	Executor     string           `json:"executor,omitempty" binding:"omitempty,oneof=local pi"` // 显式选择可信执行器
	Key          string           `json:"key" binding:"required,max=200"`                        // 幂等键
	Goal         string           `json:"goal" binding:"required"`                               // 目标引用
	Type         string           `json:"type" binding:"required"`                               // 执行器类别
	Name         string           `json:"name" binding:"required,max=255"`                       // 任务名称
	Target       string           `json:"target" binding:"required,max=512"`                     // 完整目标标识
	Tools        []string         `json:"tools" binding:"required,min=1,max=32"`                 // 任务允许工具
	Capabilities []string         `json:"capabilities" binding:"required,min=1,max=32"`          // 所需能力
	Priority     int              `json:"priority" binding:"gte=0,lte=100"`                      // 调度优先级
	Dependencies []DependencySpec `json:"dependencies" binding:"max=1000,dive"`                  // 前置条件
	DelayMS      int              `json:"delay_ms" binding:"gte=0,lte=30000"`                    // 本地受控执行延迟
}
type RunSpec struct {
	Objective  string     `json:"objective" binding:"max=4000"`           // 交由真实模型规划的任务描述
	Name       string     `json:"name" binding:"required,max=255"`        // 运行名称
	Source     string     `json:"source"`                                 // 任务来源
	Mode       string     `json:"mode"`                                   // normal 或 benchmark
	Scope      Scope      `json:"scope" binding:"required"`               // 不可扩大的授权快照
	MaxWorkers int        `json:"max_workers" binding:"gte=1,lte=64"`     // 并发预算
	MaxTasks   int        `json:"max_tasks" binding:"gte=1,lte=1000"`     // 任务总预算
	Deadline   time.Time  `json:"deadline" binding:"required"`            // 运行截止时间
	Goals      []GoalSpec `json:"goals,omitempty" binding:"max=100,dive"` // 目标集合
	Tasks      []TaskSpec `json:"tasks" binding:"max=1000,dive"`          // 可选初始计划
}
type WorkerSpec struct {
	RunID          string   `json:"run_id" binding:"required,uuid"`               // 授权运行
	Name           string   `json:"name" binding:"required,max=100"`              // 逻辑执行者名称
	Capabilities   []string `json:"capabilities" binding:"required,min=1,max=32"` // 能力标签
	MaxConcurrency int      `json:"max_concurrency" binding:"gte=1,lte=64"`       // 并发槽位
}
type WorkerIdentity struct {
	Worker models.Worker `json:"worker"` // 已注册执行者
	Token  string        `json:"token"`  // 仅注册时返回凭证
}
type LeaseProof struct {
	RunID      string `json:"run_id" binding:"required,uuid"`    // 授权运行
	WorkerID   string `json:"worker_id" binding:"required,uuid"` // 执行者
	Token      string `json:"lease_token" binding:"required"`    // 租约秘密
	Generation int64  `json:"generation" binding:"gte=1"`        // fencing 代数
}
type Claim struct {
	Task  models.Task `json:"task"`  // 已认领任务
	Proof LeaseProof  `json:"proof"` // 执行凭据
}
type Observation struct {
	Kind       string  `json:"kind" binding:"required,oneof=asset fact evidence finding"` // 数据类别
	Type       string  `json:"type" binding:"required,max=100"`                           // 事实类别
	Target     string  `json:"target" binding:"required,max=512"`                         // 授权目标
	Content    string  `json:"content" binding:"required,max=16384"`                      // 安全观察内容
	Confidence float64 `json:"confidence" binding:"gte=0,lte=1"`                          // 可信度
}
type Submission struct {
	Review       *StageReviewResult `json:"review,omitempty"` // 只读复核结论，由服务端重新核验
	LeaseProof                      // 租约证明
	Observations []Observation      `json:"observations" binding:"max=64,dive"` // 原子提交的观察
	Proposals    []TaskSpec         `json:"proposals" binding:"max=16,dive"`    // 后续任务建议
	Message      string             `json:"message" binding:"max=1000"`         // 失败或取消原因
}
type Snapshot struct {
	Review    *StageReviewSnapshot                                `json:"review,omitempty"` // 阶段专用完整只读上下文，不受普通32条事实窗口截断
	Task      models.Task                                         `json:"task"`             // 独立任务快照
	Facts     []models.Fact                                       `json:"facts"`            // 有限相关事实
	Workspace string                                              `json:"-"`                // 私有文件目录
	Audit     func(context.Context, string, map[string]any) error `json:"-"`                // 绑定当前租约的服务端审计
}
type RunResult struct {
	Stages   []StageSummary          `json:"stages,omitempty"` // 稳定排序的服务端阶段验收快照
	Contract string                  `json:"contract"`         // 结果契约版本
	Run      models.Run              `json:"run"`              // 运行生命周期
	Goals    []models.Goal           `json:"goals"`            // 目标评估
	Tasks    []models.Task           `json:"tasks"`            // 任务结果
	Facts    []models.Fact           `json:"facts"`            // 共享观察
	Findings []models.Finding        `json:"findings"`         // 发现及验证
	Events   []models.ExecutionEvent `json:"events"`           // 执行轨迹
}
