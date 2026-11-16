# Pi SDK 后端接入设计与预检

## 范围与证据
2026-11-15。沿用已有 Go/Gin API，不修改前端，不引入新公开服务端口。
已从 npm 核验 @mariozechner/pi-agent-core、@mariozechner/pi-ai 0.73.1，MIT，官方源码 https://github.com/badlogic/pi-mono 。检查发布包 README、agent-loop.d.ts、types.d.ts；使用真实 agentLoop / TypeBox 工具而不是自造模型循环。用户已指定 Pi，不再横向选择其他 Agent 框架。安装时发现旧包已由维护者标记弃用；进一步从 npm 核验新包 @earendil-works/pi-agent-core 与 pi-ai 1.5.2（MIT），仓库 https://github.com/earendil-works/pi ，最终使用新包并以新包类型检查为准。

## 接入方案比较
1. Pi coding-agent CLI RPC：复用完整编码 Agent，但默认文件/Shell能力和宿主资源发现不适合当前只读评估边界，不选。
2. 常驻 Node HTTP 服务：可共享启动成本，但多会话路由、认证、取消、隔离复杂，当前不选。
3. 每 Task/Lease 启动官方 Pi agent-core Node 桥接进程（选用）：Go 管生命周期与工具权限，stdio 有界 JSON 行通信。启动成本更高，但无需外部端口、共享 Session 或隐藏宿主扩展。
按现有 Go 契合度/隔离复杂度/运维（5为最佳）：CLI 3/2/3；HTTP 3/2/2；stdio 4/4/4。不是 OS 沙箱，不允许安装或执行不可信扩展/脚本。

## 模式与性能预检
Adapter 将 Pi TypeScript SDK 转成 TaskExecutor；复用现有 Strategy 注册表。Go 工具分派为保护代理，模型只能提出工具请求和 TaskProposal，不能改数据库状态。
不新增通用工作流引擎或消息框架。顺序工具调用、map ID 关联平均 O(1)，事件与输出按步骤和字节数限额，O(S+B) 空间；默认最多 8 轮、8 工具、16000 总 Token、每轮最多 2048 输出 Token，45 秒总执行边界沿用 Runtime。每模型响应后核算 Token，硬限制在下一轮前阻止（在途响应可能超预算，不声称精确费用硬上限）。模型用量记录 SDK 返回的 Token；自定义模型价格未知，记录 cost_known=false，不输出零费用或虚构估价。
默认全局最多 2 个 Pi 执行进程，避免 Run 并发数被误当成 Node 资源上限。暂不做进程池或吞吐承诺。

## 数据流与安全
TaskSpec.executor=pi 显式选择，默认 local 不变；未启用 Pi 不回落为演示成功。
Go 取有限 Snapshot → 租约审计开始 → 新 Node/Pi Session → 模型提出工具 → Go 再验授权和租约 → 精确目标只读 GET → 真实观察写入结果 → Submit 原子保存。
Node 仅获得任务白名单工具，无 read/write/bash、无扩展自动发现、不继承数据库/控制令牌或 NODE_OPTIONS；模型 key 经 stdin 传递，不落库、不写普通日志。每任务/代数独立目录，旧进程取消并等待回收。
只读 HTTP 工具只访问 Task 目标完整 URL，不接受模型自定路径、请求体、Cookie或Header；拒绝重定向和代理，限制响应大小、超时、压缩；连接 DNS 解析后的地址再次检查。默认拒绝本地/私有/保留地址；本地靶场需服务端显式允许 CIDR。访问范围=Run精确目标 ∩ Task工具 ∩ 服务端网络规则。
模型文本只作为不可信摘要，不作为漏洞或验证事实。真实工具观察由 Go 生成；Pi 验证不沿用演示中的“相同目标全部确认”逻辑，不会仅因模型说完成就确认真实漏洞。
复用 execution_events/Outbox 保存受限步骤元数据、工具摘要与 usage，无数据库迁移；输入快照仅记录事实ID与配置摘要，不保存密钥或思维链。

## 验证与边界
Go vet/build、Node类型检查、官方 SDK + 本地 OpenAI兼容SSE协议夹具的端到端行为检查（不是线上模型能力验收）；真实 DB/API、目标拒绝、取消、预算、缺失配置。没有用户模型凭证时不调用或声称完成真实商业模型验收。
首批只支持 Pi SDK 的 openai-completions API；后续再扩展提供方、精确 Finding 验证与沙箱。无真实攻击工具、任意Shell和公网批量扫描。
