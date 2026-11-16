# 阶段设计落地方案：完成度映射

> 状态标签：**现行盘点**。把《设计文档.md》的六个迭代与四个增量功能映射到已落地代码、历史验收与剩余缺口。
> 规则：只引用代码存在性或历史验收文档陈述；**本文不宣称任何测试在本次盘点中运行通过**。

## 1. 总览

| 阶段 | 主题 | 完成度 | 主要缺口 |
|---|---|---|---|
| 原六迭代 | 运行管理/调度/租约/Blackboard/规则规划/结果导出 | ✅ 已落地 | 模型动态重规划、Run 级累计预算硬限 |
| 控制台增量 | Vue 3 实时协作工作台 | ✅ 已落地 | 大规模历史分页 |
| Pi 后端增量 | SDK 桥接与受限工具 | ✅ 已落地（能力有界） | 通用工具、Finding 验证器 |
| 资产探测增量 | 有界 TCP 探测 + 报告 | ✅ 已落地 | 仅单 IPv4、16 端口 |
| 统一工作流增量 | objective 模型规划 + 阶段验收 | ✅ 代码落地 / ⚠️ 真实验收部分 | 真实提供方下全场景验收、自动补充闭环 |

## 2. 原六迭代（依据《设计文档.md》1633-1687 行迭代映射）

| 能力 | 证据 | 状态 |
|---|---|---|
| Run/Goal/Task/Dependency 持久化与状态机 | `models/`、`pkg/contracts/`、`pkg/task/` | 已落地 |
| 能力标签匹配 + 双层依赖门禁 + 原子认领 + 租约 fencing | `pkg/engine/scheduler.go:15-260` | 已落地 |
| 规则 Initial Planner + 规则增量提案 | `pkg/planner/rules.go:8-31`、`pkg/engine/recovery.go:48-57` | 已落地（仅旧 API 路径有增量） |
| Blackboard：观察/提案/证据/事件同事务提交 | `pkg/engine/blackboard.go:17-180` | 已落地 |
| Outbox + Redis 可选通知 + 至少一次投递 | `internal/storage/outbox.go` | 已落地 |
| 租约过期恢复轮询 | `pkg/engine/recovery.go` | 已落地（Pi Task `MaxRetries=0`，过期即 timeout） |

历史验收：`docs/implementation.md`、`docs/verification.md`（陈述为当时通过；「不建设 UI/LLM」等结论仅适用当时）。

## 3. 控制台增量

- 统一「新建任务」表单只提交 objective/目标/授权声明/预算（`web/src/components/CreateRunDrawer.vue:53-86,153-191`）。
- 每秒增量事件轮询；活跃运行无事件也刷新快照（`web/src/stores/console.ts:79-105,135-178`）——设计文档第 8 行「仅事件刷新」已被覆盖。
- 动画只消费真实新增事件、同时最多一个交接（`web/src/components/TaskGraph.vue:423-446`）。
- 已知限制：导航最近 100 运行、事件内存 500 条、证据索引 200 条，无大规模分页。
- 历史验收：`docs/console-design.md`、`docs/console-verification.md`。

## 4. Pi 后端增量

- 官方 SDK `runAgentLoop`，Go 拉起专属 Node 子进程，stdio 转发，无文件/Shell/任意命令工具（`pi-adapter/bridge.ts:100-246`）。
- 凭证仅经 stdin 传入子进程，不写入 Run 配置或事件。
- 单任务预算：8 轮模型 / 8 次工具 / 16000 Token / 45 秒；全局并发 2。
- `docs/pi-backend-verification.md:44-61` 记录过**一次真实提供方执行成功**的历史验收——但不能用它证明统一规划入口的自然语言规划质量已验收。

## 5. 资产探测增量

- 探测边界见 `architecture-baseline.md` §4.1；结果落 Fact/Evidence，开放端口记主机 Asset（`pkg/pi/discovery.go`）。
- 旧入口「默认 12 端口 / 用户填端口」已被统一入口替代：端口由 Planner 选定并在计划通过时冻结。
- 历史验收：`docs/asset-discovery-design.md`、`docs/asset-discovery-verification.md`。

## 6. 统一工作流增量（现行主入口）

- objective → Pi 规划（事务外）→ 提交时校验 nonce/状态/截止 → 计划+scope 冻结 → 调度执行 → stage review → 阶段验收门禁（`pkg/engine/planning.go:89-193`、`pkg/engine/stages.go`）。
- 失败策略：规划中断不自动重复付费调用；证据不足不自动重扫；保留明确失败原因。
- `docs/unified-workflow-verification.md:8,17,54` 声明的模型规划验收**使用确定性协议夹具**，真实提供方下的全场景验收未完成——这是下一阶段的重点工作之一。

## 7. 永久测试资产现状（区别于历史验收陈述）

仓库现有永久 Go 测试仅三份：
- `pkg/security/objective_test.go:21-149`（范围/计划纯函数）
- `pkg/pi/plan_scope_test.go:61-177`（计划范围）
- `pkg/pi/observe_failure_test.go:16-54`（含本地 HTTP 目标）

未发现独立的 stage review、Engine、Scheduler 永久测试文件。`make check` 仅 vet+构建，不是测试命令（`Makefile:12-23`）。

## 8. 明确未实现（不得在宣传或文档中暗示已具备）

- 通用 Pi 编码/终端 Agent、不可信工具沙箱
- 精确 Finding/Evidence 独立验证器闭环
- 受控阶段修复 / 自动重规划闭环
- 正式 TSecBench 接入（`pkg/benchmark` 仅契约+LocalAdapter，未虚构协议）
- 生产多租户认证体系
