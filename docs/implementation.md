# 第一阶段实施决策

## 范围与模式
采用 API 模式，依据设计文档第十章明确的 HTTP 内部接口和第十六章 WebSocket 技术栈。按六个迭代交付，不建设平台 UI、真实攻击工具、LLM 服务或 TSecBench 私有协议。工程入口沿用设计中的 cmd/engine/main.go。

## 模式与边界
- Strategy：TaskExecutor 接口和只读注册表，替换任务执行方式，不设置固定 Worker 角色。
- Adapter：Local Benchmark 和对象存储接口隔离外部依赖，TSecBench 只保留契约。
- Observer：事务 Outbox + Redis 通知；数据库轮询兜底。事件至少一次投递，规划通过幂等键去重。
- 状态机使用状态常量和显式转移表，不为每个状态建立类。
- Run/Planner/Scheduler/Blackboard 服务依赖 repository 契约；PostgreSQL repository 封装跨表原子操作。纯业务策略在公共领域包，HTTP 不接触 ORM。
- 所有 Run 级写操作先锁 Run 行，再锁 Worker、Task、Lease；认领使用 FOR UPDATE SKIP LOCKED，租约 generation 与 token hash 防止旧提交。Run 锁是 MVP 并发上限的线性化点，任务执行在事务外并行。
- Worker 绑定授权 Run 并使用独立令牌；控制端令牌和 Worker 令牌隔离。dev 不关闭权限检查。

## 数据结构、算法与规模假设
- MVP 参考预算为每 Run 100 个任务、5 个并发；内置逻辑 Worker 默认 3 个，API 要求显式提供 Run 预算，硬上限 1000 个任务、64 并发；没有吞吐承诺。
- DAG 邻接表 + DFS，O(V+E) 时间/空间；注册表 map 平均 O(1)。
- 调度评分采用文档的五项权重，能力必须全部满足；一次预解析任务与依赖 O(V+E)，有限候选 O(N)，排序 O(N log N)。当前为保证依赖快照一致性，按 Run 有界加载最多 1000 个任务并使用数据库行锁；DDL 提供 Run/status 就绪索引，不声称已实现大规模高吞吐调度。
- Blackboard 上下文按当前任务的精确目标过滤，最多 32 条事实；API 游标/分页上限 200；证据内容存独立对象目录，数据库仅存哈希和引用。
- PostgreSQL 唯一约束负责任务、资产和结果提交去重；Outbox 批量处理并通过持久事件序列补齐 WebSocket。
- 任务执行受 context 截止时间、固定并发槽位、最大任务数、工具白名单约束；无任意脚本/子进程执行能力。

## 技术选择与验证
Gin 相比标准库减少绑定与中间件样板；GORM 官方 PostgreSQL 驱动提供事务/锁/映射，相比驱动手写减少数据层重复；Redis 官方 Go 客户端用于通知。配置使用 YAML v3 + godotenv + env 类型解析，严格固定合并优先级。UUID 使用 google/uuid。以上库使用 Go Modules 固定版本，许可主要为 MIT/BSD/Apache-2.0，具体依赖见 go.mod/go.sum。不引入微服务、队列平台或 Kubernetes。
已核对 Gin/GORM 官方文档和 go-redis 当前仓库维护情况。数据库锁和事务使用 GORM clause API；Unit of Work 的动态实体操作沿用传统链式 API 并逐次检查错误，公共服务不接收 GORM 句柄；复杂加权候选查询可使用参数化原生表达式。
验证：gofmt、go vet、构建；真实 PostgreSQL + dev 服务的 HTTP 行为检查，覆盖文档第十四章协作、隔离、fencing、取消和事件恢复。无已有测试，普通功能不创建单元测试源码；验收结果另行记录。
