# SHARKNEX 鲨核 · 智能安全竞赛平台（下一代）

自主渗透协同引擎 V1.0 —— 按《设计文档.md》实现的 Go 模块化单体协同引擎。当前交付聚焦 **Hybrid Planner + Dynamic Worker + Shared Runtime + Blackboard**，不是完整渗透平台。

## 开发启动

要求 Go 1.26.2+、PostgreSQL 16+、Node.js 24 LTS（24.15+）及 npm；Redis 可选但推荐用于事件通知。

1. 创建独立 PostgreSQL 数据库，复制 `.env.example` 为 `.env`。
2. 设置 `DATABASE_URL`、高熵随机 `ENGINE_API_TOKEN`；可选设置 `REDIS_URL`。
3. 在项目根目录运行：

```sh
make console-install  # 首次安装前端依赖
make dev              # 一次启动 Go API + Vue 热更新控制台
# 仅后端：make dev-api 或 go run ./cmd/engine --mode dev
```

dev 模式仅允许回环监听，保持全部鉴权与授权校验，启动时执行版本化 SQL 迁移。后端默认监听 `127.0.0.1:8080`，开发控制台打开 **http://127.0.0.1:5174**。`make dev` 只复用健康的 dev 后端，不停止其他项目的进程；复用时后端代码修改需要重启该实例。生产模式不自动迁移，先执行 `make migrate`，再通过可信 TLS 代理部署。不要将 dev 服务直接暴露公网。

配置顺序：安全默认值 < 根目录 `config.yaml` < `.env` < 进程环境变量。配置文件可以缺失，但缺少数据库连接或控制令牌会拒绝启动；无效 YAML/env 类型会报错。CLI `--mode` 显式覆盖模式。凭证不会写入普通上下文或数据库日志。

`ENGINE_WORKERS=0` 可关闭内置 Worker，使用内部 API 手动驱动。默认三个逻辑 Worker 都具备多项能力，并非固定角色。默认执行器仍为 local 受控演示；现在可显式启用 Pi SDK Adapter，对授权的精确 HTTP 目标执行有界只读 GET。不会发送漏洞利用或攻击请求，无任意 Shell 或文件工具。接入方式见下文。

## Pi SDK 后端（可选）

```sh
make pi-install
make pi-check
```

在安全环境中设置 `PI_ENABLED=true`、`PI_MODEL`、`PI_BASE_URL`、`PI_API_KEY` 后重启后端。当前支持 OpenAI 兼容的 `chat/completions` 接口（提供方须支持工具调用），需要 Node >=22.19。SDK 固定为官方新命名空间 `@earendil-works/pi-agent-core` / `pi-ai` 1.5.2。模型凭证只经 stdin 传给专属子进程，不写入 Run 配置或事件。

Web 控制台使用统一「新建任务」入口，提交任务描述 `objective`、精确授权范围和预算，不再让用户区分 Pi、端口扫描或演示模式，也不在前端拼装任务图。启动后由模型 Planner 生成结构化阶段计划，经 Go 校验后交给 Scheduler 分配 Worker；服务端未启用 Pi 时明确拒绝真实任务，不回退模拟执行。

旧显式任务 API 继续兼容：Task 设置 `"executor":"pi"`，Run 的 `scope.tools` 与 Task 的 `tools` 均包含 `"http.observe"`。完整示例见 `examples/pi-run.json`。不含 objective 的旧请求仍保留 local 默认及规则 Initial Planner；新 objective 请求不得混入手工 goals/tasks。现有只读工具仅覆盖授权 HTTP 观察与有界 TCP 探测，不等于具备任意安全测试能力。

默认拒绝本机、私有及保留网络；本地授权靶场必须由服务端设置精确 `PI_ALLOWED_CIDRS`（如 `127.0.0.1/32`）。CIDR 例外不会扩大 Run/Task URL 授权。HTTP 工具不跟随重定向、不继承代理或 Cookie，响应体最多 2048 字节；它不是扫描器。

默认单任务最多 8 轮模型、8 次工具、16000 个已用 Token，每次输出最多 2048 Token、执行最长 45 秒；进程全局最多 2 个 Pi 子进程。Token 采用 SDK 返回用量，当前在途模型响应可超过剩余预算，但不会继续开启下一轮；自定义模型未配置价格，不将未知成本报告成免费。Pi 普通执行失败不自动付费重试；租约恢复可能重新执行，生产还需提供方幂等/费用控制。

`agent.started/turn/usage/tool_result/completed/failed` 及 `tool.called` 记录进入现有数据库事件与 Outbox，可通过现有 events/result 接口读取。模型摘要不等于事实；真实观察由 Go 工具生成并写入现有证据存储。Pi 不产生漏洞确认结论，精确 Finding/Evidence 独立验证器仍待后续实现。设计取舍与验收边界见 `docs/pi-backend-design.md`、`docs/pi-backend-verification.md`。

## 授权资产探测

通过统一「新建任务」填写任务目标和目标资产（1–10 条精确 HTTP(S) URL，或单一 IPv4），并确认本次授权声明；无需另填 TCP 端口或选择底层工具。Planner 根据任务目标，在服务端支持的能力与预算内选择工具、端口和步骤；服务端校验后固定执行范围，Worker 不能自行扩大范围。显式 API 范围限制仍须遵守。声明随 Run 的 scope.authorization 保存，执行期间不重复索取；变更目标或声明会清除表单勾选，避免沿用旧授权。Run 截止后不继续执行。

端口由 Planner 选择并在计划通过时固定，不预填公网目标。当前执行器能力保持有界：普通 TCP connect 串行执行，连接间隔至少500ms、连接超时800ms。工具只接受空参数，模型无法修改绑定 IP/端口；进程内一次只执行一个探测批次。仅读取有限服务欢迎banner，常见HTTP(S)端口只对根路径执行一次GET；不跟随重定向、不继承代理、不绕过TLS验证，不做UDP、全端口、认证尝试或漏洞利用。

结果在「资产报告」显示：端口状态、实测协议、端口提示（猜测）、服务自报指纹、HTTP状态、TLS信息和限制。完整观察同时写入 Fact/Evidence；存在开放端口时记录主机 Asset。端口超时不等于关闭，自报指纹不能证明版本真实或存在漏洞。报告覆盖全部声明端口不等于完成全站安全评估。

API 示例：`examples/asset-discovery-run.json`；提交前更新未来24小时内的 deadline，并填写自己的真实授权依据。控制端与任务工具均为 `tcp.discover`，scope.tcp_ports 限定 task.ports，缺少明确授权或端口越界会拒绝。
设计与真实验收结果：`docs/asset-discovery-design.md`、`docs/asset-discovery-verification.md`。

## Web 控制台

组件库为 **Ant Design Vue**，协作工作台使用 Vue Flow 与固定角色分区网格，数据来自真实 API，而不是模拟的动画数据。

1. 打开 http://127.0.0.1:5174，点击「新建任务」。
2. 填写任务描述与目标资产，无需另选底层工具或填写端口；Planner 在现有能力和预算内规划执行步骤。并发、任务预算和截止时间位于高级设置。
3. 创建并启动后，先观察 Planner 规划，再查看 Scheduler 分配与 Worker 执行；规划失败不会启动扫描。
4. 阶段摘要显示执行进度、验收条件、复核者与未完成原因。Worker 完成工作后产生只读复核任务，阶段验收通过才进入后续阶段；自检与交叉复核明确区分。
5. 点击图节点查看能力、执行者、事实、证据索引和轨迹；下方页签保留任务、建议、报告与目标结果。
6. 「暂停视图」只冻结界面，恢复后按事件游标补齐；「取消」才会停止后端运行。结果可以导出完整 JSON。旧本地演示运行仍可通过 API 创建并在控制台查看。

浏览器使用每秒增量 HTTP 事件轮询；现阶段没有将其包装成 WebSocket。自动更新保留节点拖动位置、缩放、选择、页签和列表分页，切换运行会取消旧请求。导航只展示最近 100 个运行，事件内存保留最近 500 条（当前筛选显示最多 80 条），建议 / 证据索引最多 200 条。事件多时快照会重新读取完整结果，尚未做大规模历史分页优化。

dev 代理仅在 Node 服务端读取根 .env 并注入 Bearer，拒绝跨站 Origin / Host；不向客户端下发配置文件或令牌。构建产物由后端根路径托管，但该静态入口需要手动输入控制端令牌（仅内存保存），不会自动获得 dev 代理权限。

```sh
make console-check    # lint + 类型检查
make console-build    # 生产构建，输出 web/dist
make check            # Go vet + 带裁剪参数的构建
```

静态入口使用 Hash Router，未知 API / 资源仍返回真实 404。没有 dist 时，后端首页提供明确的构建和开发入口指引。请勿将 Vite 开发服务暴露公网。

## 演示

`examples/run.json` 展示并行任务与 success 依赖；提交前将 deadline 改为未来 24 小时内的 RFC3339 时间。

```sh
# TOKEN 取自自己的安全环境配置，不要将真实值写入脚本或提交
curl -H "Authorization: Bearer $ENGINE_API_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @examples/run.json http://127.0.0.1:8080/api/v1/runs

curl -X POST -H "Authorization: Bearer $ENGINE_API_TOKEN" \
  http://127.0.0.1:8080/api/v1/runs/RUN_ID/start
```

执行过程：初始资产观察和 Web 分析并行；资产事实触发 service_analysis 增量任务；证据任务读取相同目标的有限事实快照，并提出 verification 建议；Planner 校验后创建任务；Goal Evaluator 根据 required_facts、未验证发现及任务终态决定结果。省略 tasks 时由规则 Initial Planner 生成授权资产观察计划，不声称使用了 LLM。

## 目标驱动与阶段协作

新入口使用 objective，由配置的模型产生计划。规划只提供结构化输出工具，不开放目标网络操作；Go 验证计划后使用现有 Goal、Task、Dependency、Blackboard 持久化，不新增阶段表。规划及执行继续受精确授权、任务总量、截止时间、模型和工具预算约束。

阶段内执行完成后，Scheduler 分配 `stage_review` 只读复核任务，检查每个目标的工作结果、事实与证据关联。优先交叉复核；单 Worker 可自检，但不显示为多人共识。服务端重新核对复核所依据的版本，只有阶段验收通过才允许后置阶段执行；复核不重新扫描，也不把服务观察升级为漏洞确认。

规划中断不会自动重复付费调用，证据不足不会自动重新扫描。系统保留明确失败原因，用户可调整范围/预算后重新下发；当前不承诺无限自动重规划或任意任务能力。完整设计和实际验证边界见 `docs/unified-workflow-design.md`、`docs/unified-workflow-verification.md`。

## 接口

所有业务接口使用 Authorization Bearer。控制端令牌只能用于控制 API 和 Worker 注册；注册返回独立 Worker token，绑定单个 Run。响应格式 `{"data": ...}` 或 `{"error": ...}`；无可领取任务返回 204。

### 控制端

- `GET /api/v1/runs?limit=100&offset=0`：鉴权运行导航，创建时间倒序
- `POST /api/v1/runs`
- `GET /api/v1/runs/:id`、`GET /api/v1/runs/:id/status`
- `POST /api/v1/runs/:id/start`、`POST /api/v1/runs/:id/cancel`
- `GET /api/v1/runs/:id/{tasks,goals,workers,assets,facts,evidence,findings,proposals,events}`
- `GET /api/v1/runs/:id/result`：结果契约 `autonomous-engine.result/v1`
- `GET /api/v1/runs/:id/ws?after=SEQUENCE`：WebSocket 按数据库游标补齐事件；使用支持 Authorization header 的客户端
- `GET /healthz`：进程健康、模式和版本；`GET /readyz`：数据库就绪

列表默认最多 100 条，`limit` 最大 200；事件通过 `after` 序列增量读取。

### Worker

- `POST /internal/workers/register`：控制端提交 run_id、name、capabilities、max_concurrency
- `POST /internal/workers/{heartbeat,claim}`：Worker token，提交 run_id、worker_id
- `POST /internal/tasks/:id/heartbeat`：执行开始/续租
- `POST /internal/tasks/:id/context`：有限目标事实快照
- `POST /internal/tasks/:id/{complete,fail,cancel}`：结果、失败或取消提交
- `POST /internal/proposals` 和 `POST /internal/blackboard/proposals`
- `GET/POST /internal/blackboard/{assets,facts,evidence,findings}`

租约证明包含 `run_id, worker_id, lease_token, generation`。认领后先调用任务 heartbeat 进入 running。结果提交追加 observations、proposals 和可选 message。Blackboard POST 和 proposal POST 额外传 task_id；GET 必须提供 run_id、worker_id。Worker 不存在任意状态修改、任务创建或事件改写接口。

## 一致性与隔离

- PostgreSQL 是唯一事实来源，14 张业务表加迁移版本表；SQL 迁移位于 migrations/001_init.sql，实体按表分别存放 models/。
- 状态、事实、证据索引、轨迹和 Outbox 同事务提交。Redis 缺失或停机时 Outbox 保持 pending；恢复后至少一次投递，数据库轮询继续推进协同。
- 原子认领先锁 Run，再检查全部资源约束与能力；任务行使用 FOR UPDATE SKIP LOCKED。Run 行锁是 MVP 调度的线性化点，同 Run 认领事务短暂串行，任务执行保持并行。
- 每次重新认领使用递增 generation + 高熵租约 token；只存 token hash，旧执行者不能覆盖新结果；相同结果重复提交不重复写观察。
- Run 级精确 URL 授权快照覆盖地址、端口、路径；不隐式授权子域、额外端口或路径。工具必须同时处于 Run/Task 白名单。
- Worker/Task 独立 context 和工作目录；证据按 Run 分区、内容哈希寻址，文件权限 0600、目录 0700；数据库仅保存证据引用和哈希。对象在事务回滚时可能留下未引用的不可变文件，后续可按保留策略回收。
- Run 最大 1000 个任务、64 并发，单任务最长 45 秒；独立逻辑 Worker 可以覆盖多个能力。max_workers 表示 Run 同时执行任务上限。
- Shared Runtime 不执行不可信脚本/二进制；未来真实工具必须使用独立沙箱，并实现 context 取消及资源回收。当前没有共享凭证、Cookie 或外部 Agent 记忆。

## 目录

- cmd/engine：入口装配
- pkg/contracts、pkg/task、pkg/security：公共契约、状态机、授权规则
- pkg/engine：按 run/scheduler/blackboard/recovery/tool 分文件组织的应用服务与 repository 契约
- pkg/planner、pkg/scheduler、pkg/runtime：纯规则、评分、执行器策略
- pkg/benchmark：BenchmarkAdapter 契约及 LocalAdapter；未虚构 TSecBench 协议
- internal/storage：GORM/PostgreSQL、Outbox、证据对象存储
- internal/app：共享运行时及逻辑 Worker 生命周期
- internal/httpapi：Gin 协议适配和事件 WebSocket
- web：Vue 3 / TypeScript / Ant Design Vue 实时协作控制台
- scripts/dev.mjs：统一开发启动与本脚本进程生命周期
- docs/console-design.md：控制台预检、状态与安全边界
- docs/implementation.md：后端预检与技术决策

## 校验与迁移

`make check` 执行 vet 与完整构建。普通新功能不默认新增测试源码，当前采用真实数据库和 HTTP 行为验收；具体结果见 docs/verification.md。

生产迁移由明确操作执行，dev 启动自动执行；初始化迁移在 advisory lock 下原子执行，失败整体回滚。已经存在的数据不会被清空；版本记录阻止重复执行。回滚需停止服务并恢复迁移前数据库备份，不提供自动 DROP 脚本。当前版本不包含生产认证平台、TSecBench 正式接入、不可信工具沙箱或真实漏洞验证器。Pi SDK 已接入，但未配置模型时仍默认使用演示执行器。
