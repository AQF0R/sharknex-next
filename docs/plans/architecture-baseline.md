# 设计方案：sharknex-next 架构基线（现状）

> 状态标签：**现行**（与 main 分支代码一致，非愿景稿）。
> 本文是现状架构的简明基线与真实能力边界；详细决策见根《设计文档.md》及 docs/ 各增量设计。

## 1. 系统定位

sharknex-next 是**已接入真实 Pi 模型的受限协同执行引擎**：用户提交任务目标（objective）与授权范围，模型 Planner 生成结构化阶段计划，Go 服务端校验后交给 Scheduler 分配 Worker 执行，阶段产出经只读复核验收。

它**不是**通用自主渗透 Agent，也**不只是**端口扫描器。当前执行能力严格限定为：授权 HTTP 观察与有界 TCP 探测。

## 2. 分层架构

| 层 | 职责 | 关键实现 |
|---|---|---|
| 入口装配 | Engine、Runtime、Pi Planner、恢复轮询、Outbox 装配 | `cmd/engine/main.go:90-143` |
| HTTP API | Gin 协议适配、事件 WebSocket、控制台托管 | `internal/httpapi/` |
| Engine | Run 生命周期、调度、Blackboard、阶段验收、恢复 | `pkg/engine/`（run/scheduler/blackboard/stages/recovery 分文件） |
| Planner | 规则 Initial Planner（旧 API）+ Pi 模型规划（objective 新路径） | `pkg/planner/rules.go`、`pkg/engine/planning.go:89-193` |
| Runtime | 执行器策略（local 演示 / pi 受限工具）、stage review | `pkg/runtime/`、`pkg/runtime/review.go` |
| Pi 桥接 | Go 拉起专属 Node 子进程，stdio 转发官方 SDK `runAgentLoop` | `pi-adapter/bridge.ts:100-246`、`pkg/pi/executor.go` |
| 存储 | GORM/PostgreSQL 唯一事实源；Outbox 事件；内容寻址证据对象 | `internal/storage/postgres.go:67-77`、`internal/storage/objects.go:16-44` |
| 前端 | Vue 3 + Pinia + Ant Design Vue + Vue Flow 实时协作控制台 | `web/` |

## 3. 一致性与安全模型

- PostgreSQL 是唯一事实来源；状态、事实、证据索引、轨迹与 Outbox 同事务提交（`internal/storage/postgres.go:67-77`）。
- Run 行锁是调度线性化点；任务认领使用 `FOR UPDATE SKIP LOCKED` + 递增 generation + 高熵租约 token（只存 hash），旧执行者无法覆盖新结果（`pkg/engine/repository.go:75-83`、`pkg/engine/scheduler.go`）。
- 精确授权快照：Run 级 URL/端口/路径白名单冻结于计划通过时，不隐式授权子域或额外端口；工具须同时在 Run/Task 白名单。
- 证据按 Run 分区、内容哈希寻址、单对象上限 16KB（`internal/storage/objects.go`）；数据库仅存引用与哈希。
- 默认拒绝本机/私有/保留网络；本地靶场须服务端显式配置 `PI_ALLOWED_CIDRS`。

## 4. 五类必须区分的能力边界（现状）

### 4.1 端口探测
单 IPv4、最多 16 个 Planner 选定 TCP 端口、串行 connect、间隔 ≥500ms、连接超时 800ms、探测窗口 28 秒；仅 80/8080/443/8443 追加一次根路径 HTTP(S) GET。**不含** UDP、全端口、爬站、认证尝试或漏洞利用。`timeout/error` 计入"已尝试覆盖"，不等于确认关闭。（`pkg/pi/discovery.go:62-254`）

### 4.2 scope 授权
精确字符串目标匹配；裸 IPv4 规范化后提供 HTTP/TCP 候选；模型选出的工具/端口经校验后冻结。**自然语言 objective 里的限制不构成结构化硬约束**；服务端 CIDR 例外只是网络出口许可，不扩大 Run/Task 授权。（`pkg/security/policy.go:20-36`、`pkg/security/objective.go:12-123`）

### 4.3 能力验证
当前是**标签匹配 + 执行结果检查**，不是能力实测认证：Worker 注册即 `Enabled=true`，无探针、无版本/有效期、无健康准入。（`internal/app/runtime.go:81-90`、`pkg/scheduler/priority.go:9-25`）

### 4.4 阶段复核（stage review）
普通可调度的内部只读任务，运行确定性 `CheckStage`（任务完成、事实类型、证据索引/hash、HTTP 状态、TCP 覆盖），不调模型不碰网络；服务端复算 digest。**不读证据对象正文验证存在性，不判漏洞，不证明 objective 语义达成**。交叉复核优先但不强制；`self_review` 表示复核者参与过阶段工作。（`pkg/runtime/review.go:14-194`、`pkg/engine/stages.go:295-369`）

### 4.5 Finding 验证
Pi 执行仅产出 `verification_observed`；同目标批量 confirmed 是旧本地演示逻辑（明确跳过 Pi 来源）。精确 Finding/Evidence 独立验证器**未实现**。（`pkg/pi/executor.go:283-294`、`pkg/engine/verification.go`）

## 5. 双入口并存（兼容策略）

| 入口 | Planner | 执行 | 状态 |
|---|---|---|---|
| 旧显式 tasks API | 规则 Initial Planner + 规则增量 | local 演示 / pi（显式 executor） | 兼容保留 |
| 新 objective API | Pi 模型规划（事务外执行，提交时校验 nonce/状态/截止） | 统一调度 | 现行主入口 |

统一规划器当前只接受两种配对：`asset_discovery+tcp.discover`、`web_analysis+http.observe`（`pkg/security/objective.go:126-137`）。

## 6. 已知偏差与待办（衔接 next-phase-execution.md）

1. README.md:40「租约恢复可能重新执行」对新建 Pi Task 不准确：其 `MaxRetries=0`，过期恢复直接 timeout（`pkg/engine/run.go:111-115`、`pkg/engine/recovery.go:90-101`）。
2. 前端 `maxTasks=1` 与后端 objective 最少 2 个任务不一致（`web/src/components/CreateRunDrawer.vue:163-175` vs `pkg/engine/planning.go:43-44`）。
3. objective 提案不合法时进入 `needs_work → failed`，无自动补充闭环（`pkg/engine/stages.go:189-193,416-430`）。
4. `web/pi-console-notes.md` 仍描述已被统一表单替代的 local/Pi 模式切换，应标注为历史资料。
