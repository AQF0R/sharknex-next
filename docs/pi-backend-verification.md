# Pi 后端验收记录

2026-11-15。本记录前半部分为首次 SDK 夹具验收历史；18:48 已追加真实提供方验收成功记录，见文末。

## 实际依赖与检查
- 官方维护包：@earendil-works/pi-agent-core / pi-ai 1.5.2，MIT；旧 @mariozechner 0.73.1 的 npm 弃用提示指向此命名空间。
- 使用 SDK 的 runAgentLoop 和 openai-completions streamSimple，所有模型轮次、工具校验和事件来自真实 SDK。
- `make check`：go vet + `go build -trimpath -ldflags "-w -s" ./...` 通过。
- `make build`：实际入口可执行文件构建通过。
- `make pi-check`：桥接 TypeScript 严格应用类型检查通过。
- 生产依赖 npm audit：镜像不支持安全公告接口，改用 registry.npmjs.org 后返回 found 0 vulnerabilities（当前公告快照，不代表永无漏洞）。
- 发布依赖的非选用 Google provider .d.ts 引用缺失可选 MCP 类型，故仅对第三方声明启用 skipLibCheck；未关闭应用 strict，不把此选项称为检查了全部第三方声明。
- 临时行为验收程序放在忽略的 .dev/pi-verify，使用 `go build -race -trimpath -ldflags "-w -s"` 构建并执行，无竞态报告；未新增永久测试源码。

## 跨层验收
真实 Gin HTTP API → Engine → 独立 Runtime/租约 → Node 专属进程 → 官方 Pi SDK → 本地 OpenAI SSE 协议夹具 → Go 工具代理 → 本地授权 HTTP 目标 → PostgreSQL/Evidence/Outbox。
使用独立 schema `pi_verify_20261115_175723`，未修改当前开发 Run 或公开表数据；临时 schema 保留供核对，没有自动 DROP。
最新完整一轮：42 次本地模型协议请求，6 次精确目标请求。

| 场景 | 实际结果 |
| --- | --- |
| 真实只读观察 + 后续证据建议 | completed；2 Task、2 Fact、44 Event；提案经过 Planner 审核 |
| 未开放回环 CIDR | failed；0 Fact；目标请求计数不增加 |
| 轮次/工具预算为 2 | failed；0 Fact；无虚假完成 |
| Token 预算 256，单个在途响应报告用量 300 | failed；仅调用一次模型，无下一轮 |
| 模型给 observe_http 注入额外 url 参数 | failed；目标请求计数不增加 |
| Pi 与 local 验证混合 | completed；3 Task、3 Fact；Pi Fact 保持 observed，不被 local 批量确认 |
| 提供方 401 | failed；0 Fact；错误响应正文不进入结果或事件 |
| 授权目标响应 302 指向未授权地址 | 仅记录原目标响应；不跟随 Location |
| 执行中取消 Run | cancelled；模型连接断开；Pi 槽位回收，Runtime 等待子进程退出 |
| 服务端未启用 Pi | CreateRun 拒绝，不回落成 local 成功 |

结果/事件检查：无模型 key、无提供方错误正文、无思维链，且不存在只读请求伪造的 Finding。控制字符大响应经过字节截断后可写入现有 16KB 对象限制。事件包含 agent.started/turn/usage/tool_result/completed/failed、tool.called 和现有提案/事实/任务事件。
最初夹具错误地依赖 OpenAI tool 消息的可选 name 字段，导致首次临时验收失败；根据实际 SDK 消息转换源码修正为读取 assistant.tool_calls 后，重新验收通过。此处没有更改 SDK 来迎合夹具。

## 未验证与剩余边界
- 首次夹具验收未调用外部模型；文末追加了一次真实模型成功记录，但不据此推断全部提供方兼容性、模型质量或长期成功率。
- 未专门等待 45 秒任务总时限，也未专门压测全部并发上限、所有公网 DNS/TLS 网络组合；已验证主动取消、进程回收和失败路径。
- CIDR 例外由运维管理；生产应追加网络出口防火墙。Node 专属进程隔离不是不可信代码沙箱。
- 模型用量按 SDK 响应统计；在途响应可能超过剩余 Token 预算。价格未知时 cost_known=false，不报告成零费用。普通 Pi 失败不自动重试，失联租约恢复仍可能重做/重复付费。
- 不实现漏洞利用或真实 Finding 验证；Pi verification 只产生 verification_observed，不产生 verification_completed。
- 证据仍使用现有小对象存储，本阶段没有大文件/抓包/完整页面归档与历史持久会话。

## 18:48 真实模型故障修复与成功验收
用户报告 Web Pi 任务失败，原 Run：`2b0c7e5f-df95-4a6d-b0d0-f44caf3c26b9`。模型调用成功，但目标 `127.0.0.1:18080/authorized` 无监听服务，且未设置 PI_ALLOWED_CIDRS；HTTP 观察被拒绝，模型继续调用直至预算耗尽。

修复与范围：
- 启动仅绑定回环的本地只读目标（.dev/pi-target），加入精确 `PI_ALLOWED_CIDRS=127.0.0.1/32`，保留其余模型设置；重启开发服务。未全局关闭网络策略。
- 使用 errors.Is 保留固定的网络策略、DNS 和连接错误；受限原因写入 tool_result、agent.failed 与 Task result，Web Pi 页签展示原因。未知错误正文不公开，不能从提供方错误泄漏密钥。
- 新增聚焦回归测试：未授权目标不产生请求、显式回环授权后取得真实 HTTP 内容、安全诊断不泄漏未知错误。首个错误提示测试修复前失败，修复后通过；`go test -race ./pkg/pi -count=1` 通过。
- 不新增设计模式或通用错误框架：复用 stdlib 哨兵错误，错误分类为固定长度数组 O(1)，不改变模型/工具执行预算与权限边界。

真实验收（不是模型协议夹具）：
- 通过现有 Web dev 代理控制 API 创建并启动，使用用户配置模型 `vip/glm-5.2`。
- Run：`13fb6548-2e62-42be-a19f-2b929eeb5928`，名称“Pi 真实模型验收 · 本地只读目标”。
- Run 与 1 个 Pi Task 均 completed；3 轮、3 次工具（read_context → observe_http → finish）。
- 目标返回 HTTP 200；Fact 中保留实际标记 `PI-LIVE-LOCAL-20261115`；1 Fact、1 Evidence、0 Finding。
- SDK 累计报告 2037 Token，费用未知；不据此推导真实账单。
- 浏览器打开该 Run 的 Pi 页签，确认执行器模式、成功轨迹、Token 合计 2,037 均真实显示。
- Go vet/build、前端 lint/type-check/build 通过；前端已有主包体积警告仍存在。
- 本地目标及 dev 服务保留运行供用户复验。自定义目标仍须自行确保服务可达、处于明确授权范围和网络例外范围内。

