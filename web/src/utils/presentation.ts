import type { EngineEvent, StageSummary, Task, Worker } from '@/types/engine'

const statusNames: Record<string, string> = {
  planning: '规划中',
  reviewing: '只读复核中',
  needs_work: '待补充',
  created: '待启动',
  pending: '等待中',
  blocked: '依赖阻塞',
  ready: '就绪',
  claimed: '已认领',
  running: '执行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
  retry_wait: '等待重试',
  timeout: '已超时',
  available: '空闲',
  busy: '忙碌',
  draining: '排空中',
  offline: '离线',
  active: '进行中',
  achieved: '已达成',
  partially_achieved: '部分达成',
  partially_completed: '部分完成',
  observed: '已观察',
  confirmed: '已确认',
  unverified: '待验证',
  corroborated: '已佐证',
  approved: '已批准',
  rejected: '已拒绝',
}
const eventNames: Record<string, string> = {
  'planner.started': 'Planner 开始规划',
  'planner.completed': 'Planner 计划已校验',
  'planner.failed': 'Planner 规划失败',
  'stage.running': '阶段开始执行',
  'stage.reviewing': '阶段进入只读复核',
  'stage.needs_work': '阶段需要补充',
  'stage.achieved': '阶段验收通过',
  'stage.failed': '阶段失败',
  'stage.cancelled': '阶段已取消',
  'agent.started': 'Pi 会话开始',
  'agent.turn': 'Pi 模型轮次',
  'agent.usage': 'Pi Token 用量',
  'agent.tool_result': 'Pi 工具结果',
  'agent.completed': 'Pi 执行完成',
  'agent.failed': 'Pi 执行失败',
  'run.created': '运行已创建',
  'run.started': '运行开始',
  'run.completed': '运行完成',
  'run.failed': '运行失败',
  'run.cancelled': '运行已取消',
  'run.timeout': '运行已超时',
  'task.created': 'Planner 创建任务',
  'task.claimed': 'Scheduler 分配任务',
  'task.started': 'Worker 开始执行',
  'task.completed': '任务完成',
  'task.ready': '依赖满足，任务就绪',
  'task.retry_wait': '任务进入重试',
  'task.failed': '任务失败',
  'worker.registered': 'Worker 加入协作',
  'worker.offline': 'Worker 离线',
  'fact.created': 'Blackboard 新增事实',
  'tool.called': '运行时调用工具',
  'agent.discovery': '授权端口探测',
  'proposal.created': 'Worker 提出协作建议',
  'proposal.reviewed': 'Planner 审核建议',
  'finding.created': '新增待验证发现',
  'finding.confirmed': '独立验证确认发现',
}
export const typeNames: Record<string, string> = {
  stage_review: '阶段只读复核',
  asset_discovery: '资产观察',
  service_analysis: '服务分析',
  web_analysis: 'Web 分析',
  evidence_collection: '证据整理',
  verification: '独立验证',
}
// statusLabel 将持久状态映射为明确中文，不以颜色代替状态。
export function statusLabel(status: string) {
  return statusNames[status] || status
}
// statusColor 保持图、表格与时间线的状态色一致。
export function statusColor(status: string) {
  if (['completed', 'achieved', 'confirmed', 'approved', 'available'].includes(status))
    return 'success'
  if (['planning', 'reviewing', 'running', 'claimed', 'active', 'busy'].includes(status))
    return 'processing'
  if (['failed', 'timeout', 'rejected'].includes(status)) return 'error'
  if (
    [
      'needs_work',
      'blocked',
      'retry_wait',
      'unverified',
      'partially_completed',
      'partially_achieved',
    ].includes(status)
  )
    return 'warning'
  return 'default'
}
// eventLabel 返回事件的可读行为说明。
export function eventLabel(type: string) {
  return eventNames[type] || type
}
// timeLabel 统一显示本地时间，空时间不展示伪造值。
export function timeLabel(value?: string | null) {
  return value ? new Date(value).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
}
// shortID 仅缩短界面标识，完整 ID 仍可在详情查看。
export function shortID(value?: string | null) {
  return value ? value.slice(0, 8) : '未分配'
}
// workerDisplayName 隐去内置执行器名称，优先显示稳定角色字母。
export function workerDisplayName(value?: string | null) {
  if (!value) return '未分配'
  const internalWorker = /^(?:local|pi|review)-.*-([A-Z]+)$/.exec(value)
  if (internalWorker) return `执行者 ${internalWorker[1]}`
  return /^(?:local|pi|review)-/.test(value) ? '执行者' : value
}
// workerNameMap 为快照中的执行者提供统一可读角色，不将内部名作为主要协作信息。
export function workerNameMap(workers: Worker[]) {
  return new Map(
    workers.map((worker, index) => [
      worker.id,
      /^(?:local|pi|review)-.*-[A-Z]+$/.test(worker.name)
        ? workerDisplayName(worker.name)
        : `执行者 ${index < 26 ? String.fromCharCode(65 + index) : index + 1}`,
    ]),
  )
}
// taskStage 用真实目标关联定位任务所属阶段，旧运行没有阶段时返回空。
export function taskStage(task: Task | undefined, stages: StageSummary[]) {
  return task
    ? stages.find(
        (stage) =>
          stage.id === task.goal_id ||
          stage.key === task.input.goal ||
          stage.review_task_id === task.id,
      )
    : undefined
}
// currentStage 优先显示正在执行或复核的阶段，不把任务终态当成验收结果。
export function currentStage(stages: StageSummary[]) {
  return (
    stages.find((stage) => ['running', 'reviewing', 'needs_work'].includes(stage.status)) ||
    stages.find((stage) => stage.status === 'failed') ||
    stages.find((stage) => stage.status === 'pending') ||
    stages.at(-1)
  )
}
// stageReviewLabel 明确区分交叉复核与单执行者自检，不暗示多人共识。
export function stageReviewLabel(stage: StageSummary, workers: Worker[]) {
  const reviewer = stage.reviewer_id
    ? workerNameMap(workers).get(stage.reviewer_id) || '已指定执行者'
    : '待分配执行者'
  if (stage.review_mode === 'self_review') return `单执行者自检 · ${reviewer}（非多人共识）`
  if (stage.review_mode === 'cross_review') return `交叉只读复核 · ${reviewer}`
  return stage.reviewer_id ? `只读复核 · ${reviewer}` : '尚未安排复核'
}
// eventReason 只展示真实事件中明确的错误或原因，不生成推测结论。
export function eventReason(event?: EngineEvent) {
  if (!event) return ''
  const reason = event.payload.reason || event.payload.error || event.payload.message
  return typeof reason === 'string' ? reason : ''
}
