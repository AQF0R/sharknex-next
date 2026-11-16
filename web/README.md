# 实时协作控制台

Vue 3、TypeScript、Ant Design Vue 与 Vue Flow 实现的运行观察台。数据来自引擎 API；不生成模拟任务、事实或执行轨迹。

## 开发与检查

在 `web/` 目录使用项目现有命令：

```sh
npm ci
npm run dev
npm run lint
npm run type-check
npm run build
```

也可从项目根目录执行 `make console-check`（lint 与类型检查）、`make console-build`（输出 `web/dist`）。开发服务只绑定 `127.0.0.1:5174`；静态入口的控制令牌仅保存在浏览器内存中。

## 统一新建任务

- 点击「新建任务」，填写任务描述 `objective`（1–4000 字）与精确授权范围。名称默认取描述摘要；名称、并发、任务预算与截止时长位于高级折叠区。
- 目标支持 1–10 个完整、精确的 HTTP/HTTPS URL（每行一个），或一个标准裸 IPv4；IPv4 原样传入 `scope.targets`，不与其他地址混填。不支持裸域名、网段、通配符、URL 账号密码或片段，规范化与最终范围校验仍由服务端执行。
- 不要求另填 TCP 端口或选择工具。任务描述可写明限制；Planner 在目标授权、服务端已实现能力和预算内决定工具、端口及步骤，经服务端校验后固定执行范围。当前真实能力仍有限，不承诺任意任务均可执行。
- 页面不预填公网目标、不选择执行模式，不推导或发送 `scope.tools` / `scope.tcp_ports`。授权声明绑定任务、目标与截止时长，修改任务、范围、声明或有效时长后重新确认。
- 创建请求仅包含 `objective`、`scope`（目标与授权声明）、`name`、`source: web`、`mode: normal` 和预算/截止字段，不拼装 `goals`、`tasks` 或固定 `required_facts`。`RunScopeSpec` 单独定义可选技术参数；Run 返回快照仍要求 `scope.tools`，不放松响应校验。创建只保存请求，启动后才异步规划。
- 关闭保留内存草稿；提交期间防重。创建失败保留输入；创建成功但启动失败可重试同一运行，不重复创建，也可返回运行页。后端未配置真实能力时明确拒绝，不静默转成演示。

## 规划、阶段与真实协作

- `planning` 持续轮询快照，显示「Planner 处理中」，任务尚未生成时展示空态且允许取消。不用虚构任务或动画填充等待时间。
- 规划失败优先读取 `run.config.planning.reason`，再使用真实 `planner.failed` 事件；不依赖最近 500 条事件推断持久状态。配置仅解析 objective、summary 和 planning.reason，不在界面输出内部配置。
- `result.stages` 是验收主数据：显示当前阶段、必需事实、已完成/总任务、复核者、复核模式与未完成原因。`cross_review` 为交叉只读复核；`self_review` 明确标注单执行者自检，不称为多人共识。
- `stage_review` 与 `review` / `internal_review` 执行器按只读复核展示，不由前端创建，不重新扫描。任务完成比例不等于阶段验收通过。
- 旧运行的 stages 可为空，保留查看和启动能力；原 goals 独立展示，不包装为阶段验收结果。资产报告继续解析 `asset-discovery/v1`，以授权 `tcp.discover` 范围判定，不绑定创建模式。
- Planner → Scheduler → Worker → 当前任务 → Blackboard 维持全宽纵向舞台。执行者使用 A/B/C 等可读角色，显示当前/最近任务、个人已完成/总任务和真实阶段、事实状态；任务 completed 不推断事实已经共享。
- 真实新增事件最多触发一个即时交接，临时关系线随交接结束消失，不维持静态传统连线，不排队补播。首次加载历史、暂停和恢复补齐不会重放历史动画。

## 状态、性能与验证边界

- 保留现有 API 边界和 Pinia 快照模式，不新增类层级、依赖或 UI 库。小规模阶段与任务使用数组和 Map/Set，任务/事实按 Worker 单遍分组，舞台只绘制当前任务与协作者。
- 轮询间隔约 1 秒；created/planning/running 即使没有新事件也刷新快照，终态空闲时保留轻量心跳同步。暂停仅冻结界面，不取消后端；切换运行使用 AbortController 与请求代数隔离响应。
- 节点拖动、缩放、任务选择、页签与分页不因快照更新清空。初次测量后适配舞台，支持键盘操作和 reduced-motion。折叠与状态变化使用 Vue Transition/TransitionGroup。
- 使用已有 lint、vue-tsc、生产构建验证；不新增测试源码。本次浏览器联调由主 agent 统一执行，子任务不启动或关闭服务。
