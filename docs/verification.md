# 第一阶段验收记录

## 环境与执行方式

- 日期：2026-11-15；macOS arm64；Go 1.26.2。
- 真实 PostgreSQL 16.15，开发库 autonomous_engine_dev、独立边界验收库 autonomous_engine_acceptance。
- 本地 Redis 8.10.3，仅回环监听 16379，未注册为开机启动服务。
- 使用 `go run ./cmd/engine --mode dev` 启动开发服务，Gin debug 模式、数据库持久化、鉴权均开启。
- 另启 dev 验收实例，关闭内置 Worker，通过真实 HTTP 驱动注册、并发认领和结果提交。租约缩短为 3 秒用于验证。
- 通过临时 Python HTTP/WebSocket 请求和临时 Go 适配器调用验证，没有新增永久测试源码；临时 Go 检查文件已清理。

## 静态与构建检查

- gofmt 格式化已执行。
- `go vet ./...` 通过。
- `go build -trimpath -ldflags "-w -s" ./...` 通过。
- 单入口产物构建通过。
- 非法 ENGINE_WORKERS 环境值被拒绝，未泄露变量值；`--version` 输出 dev。
- 迁移首次创建 14 张核心表和 schema_migrations；重复迁移不清空数据。

## 行为验收

| 设计文档场景 | 实际证据 |
| --- | --- |
| 多 Worker 并行 | 最终 Run 9feaa7e9-bed9-4a9e-8f76-7eb3d4660db9 注册 3 个 Worker，2 个实际参与，discover 与 web 的执行时间区间重叠 |
| 动态认领 | 同一 Worker 完成任务后继续认领；完整闭环执行 5 个任务 |
| 能力匹配 | 仅具备 verification 能力的 Worker 无法认领 web_analysis |
| 依赖管理 | evidence 在 discover 完成前不能认领；DAG 环路在创建阶段返回 400 |
| Blackboard 共享 | 后续 Worker 的 context 读取到前置 Worker 写入的目标事实；最终 5 条事实 |
| 任务与发现去重 | Planner 使用幂等键去重；相同发现观察两次只生成一个 Finding；重复结果提交只保留一条事实 |
| 并发控制 | 8 个并发认领请求在 Run 上限为 1 时仅授权 1 份；Worker 上限为 1 时即使 Run 上限为 2 也不能超领 |
| Lease 过期恢复 | 未续租任务被回收，generation 从 1 增加到 2，新 Worker 成功完成 |
| 旧 Worker 恢复 | 旧 token/generation 不能覆盖重新分配后的结果；错误 generation 被拒绝 |
| 事件恢复 | Redis 离线时 27 条轨迹及对应 Outbox 保留；恢复后全部 published，轨迹数量不变 |
| 取消运行 | 确认任务已进入 running 后取消，任务为 cancelled，Worker 槽位全部释放，未产生迟到完成结果 |
| Run 隔离 | Worker token 访问另一 Run 的 Blackboard 返回 403 |
| 授权约束 | 越界目标在创建阶段返回 400；认领和工具分派再次检查授权及工具白名单 |
| 评测结果 | 导出 autonomous-engine.result/v1，最终完整结果包含 5 个完成任务、5 条事实、39 条事件 |
| WebSocket | 实际协议升级和实时 run.timeout 事件接收通过；after 游标恢复只返回之后的事件 |
| 截止时间 | 无可用 Worker 的运行到期进入 timeout，未结束任务失效 |
| 失败预算 | 连续失败产生 generation 1、2、3，retry_count 停在 2，最终任务 failed |
| 独立验证 | Planner 自动创建发现验证任务，独立验证完成后 Finding 从 unverified 变为 confirmed |
| 证据存储 | evidence.content_ref 为 object://RunID/hash，实际内容在私有对象目录 |
| Local Benchmark | 首次文件归档成功、相同结果重复提交成功、冲突结果拒绝覆盖 |

最终 JSON 结果保存于忽略目录 `.dev/final-result.json`，Local Adapter 归档于 `.dev/archived-results/`。

## 调试中定位并修正的问题

首次真实数据库联调发现 TaskLease 查询误用不存在的 created_at 排序；已改用 generation，并重启验证原运行可从数据库继续完成。最终并行验收曾错误继承 max_workers=1，服务串行执行符合预算约束；改用明确并发预算 3 后确认执行区间重叠。上述初次失败不计入通过结果。

## 验证边界

- 未执行大规模性能压测、生产 TLS 部署、外部 Pi/LLM SDK、TSecBench 正式接口或真实攻击工具验证。
- 所有分析与“确认”均来自本地受控观察，用于验证引擎机制，不代表对真实目标完成了安全评估或确认了漏洞。
- Shared Runtime 只注册可信内置执行器；没有提供不可信脚本/二进制沙箱。未来接入高风险执行器前必须独立实现该隔离边界。
- 单 Run 原子认领使用 Run 锁，优先保证预算一致性，不承诺高并发调度吞吐。
- Local Adapter 通过明确调用归档，服务端 GET result 提供结果导出；没有自动向第三方 Benchmark 提交结果。
