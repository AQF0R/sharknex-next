<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, useId, watch } from 'vue'
import { VueFlow, useVueFlow, type Node } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import {
  AimOutlined,
  ApartmentOutlined,
  DeploymentUnitOutlined,
  ExpandOutlined,
} from '@ant-design/icons-vue'
import CollaborationActor, {
  type CollaborationKind,
  type CollaborationPoint,
} from '@/components/graph/CollaborationActor.vue'
import SystemNode, { type SystemKind } from '@/components/graph/SystemNode.vue'
import TaskNode from '@/components/graph/TaskNode.vue'
import WorkerNode from '@/components/graph/WorkerNode.vue'
import type { EngineEvent, Fact, StageSummary, Task, Worker } from '@/types/engine'
import {
  eventLabel,
  statusLabel,
  timeLabel,
  typeNames,
  workerNameMap,
  currentStage,
  eventReason,
} from '@/utils/presentation'

interface CollaborationActivity {
  id: string
  role: string
  action: string
  kind: CollaborationKind
  fromID: string
  toID: string
  from: CollaborationPoint
  arc: CollaborationPoint
  to: CollaborationPoint
}

const props = defineProps<{
  tasks: Task[]
  workers: Worker[]
  events: EngineEvent[]
  stages: StageSummary[]
  facts: Fact[]
  runStatus: string
  plannerFailure: string
  selected: string
  paused: boolean
}>()
const emit = defineEmits<{ select: [id: string] }>()
const nodes = shallowRef<Node[]>([])
const activities = ref<CollaborationActivity[]>([])
const followNew = ref(false)
const flowId = useId()
const { fitView, onNodesInitialized, viewport } = useVueFlow({ id: flowId })
const workerNames = computed(() => workerNameMap(props.workers))
const stage = computed(() => currentStage(props.stages))
const latestEvent = computed(() => props.events.at(-1))
const plannerEvent = computed(() =>
  [...props.events].reverse().find((event) => event.event_type.startsWith('planner.')),
)
const emptyDescription = computed(() => {
  if (props.runStatus === 'planning') return 'Planner 处理中 · 任务与阶段尚未生成，可取消运行'
  if (props.plannerFailure) return props.plannerFailure
  if (plannerEvent.value?.event_type === 'planner.failed')
    return eventReason(plannerEvent.value) || 'Planner 规划失败，暂无任务'
  if (props.runStatus === 'created') return '运行已创建 · 启动后 Planner 才会生成计划'
  if (props.runStatus === 'cancelled') return '运行已取消 · 不再生成或执行任务'
  if (['failed', 'timeout'].includes(props.runStatus))
    return '运行已结束，暂无任务；请查看真实事件与错误'
  return '等待服务端任务快照，不模拟执行轨迹'
})
const focusedTask = computed(() => {
  const latestTaskID = [...props.events].reverse().find((event) => event.task_id)?.task_id
  return (
    props.tasks.find((task) => task.id === latestTaskID) ||
    props.tasks.find((task) => isActive(task)) ||
    props.tasks.at(-1)
  )
})
const activeSystems = computed(
  () =>
    new Set(
      activities.value.flatMap((activity) => [activity.fromID, activity.toID]).filter(Boolean),
    ),
)
const collaborationTransform = computed(() => ({
  transform: `translate3d(${viewport.value.x}px, ${viewport.value.y}px, 0) scale(${viewport.value.zoom})`,
}))
const SYSTEM_SIZE = { width: 220, height: 80 }
const TASK_SIZE = { width: 280, height: 156 }
const WORKER_SIZE = { width: 216, height: 144 }
const SYSTEMS: { id: string; kind: SystemKind }[] = [
  { id: 'system:planner', kind: 'planner' },
  { id: 'system:scheduler', kind: 'scheduler' },
  { id: 'system:blackboard', kind: 'blackboard' },
]
let fitted = false
let observedSequence = 0
let eventsInitialized = false
let skipResumedBatch = false
let fitTimer: ReturnType<typeof setTimeout> | undefined

// fitGraph 在节点测量后居中适配全局，并限制最大倍率以保持可读性。
function fitGraph(duration = 0) {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) duration = 0
  clearTimeout(fitTimer)
  void nextTick(() => {
    fitTimer = setTimeout(() => void fitView({ padding: 0.08, maxZoom: 0.96, duration }), 80)
  })
}
onNodesInitialized(() => {
  if (!fitted && nodes.value.length) {
    fitted = true
    fitGraph()
  }
})
onBeforeUnmount(() => clearTimeout(fitTimer))

// isActive 仅依据服务端任务状态标识正在占用执行者的任务。
function isActive(task: Task) {
  return ['running', 'claimed'].includes(task.status)
}
// layout 按规划、调度、执行、任务和事实共享自上而下排列全部角色。
function layout() {
  const positions = new Map<string, CollaborationPoint>()
  const workerRows = Math.max(1, Math.ceil(props.workers.length / 3))
  const workerY = 200
  const taskY = workerY + workerRows * 176 + 16
  positions.set('system:planner', { x: 410, y: 8 })
  positions.set('system:scheduler', { x: 410, y: 104 })
  props.workers.forEach((worker, index) =>
    positions.set(worker.id, {
      x: 140 + (index % 3) * 250,
      y: workerY + Math.floor(index / 3) * 176,
    }),
  )
  positions.set('lane:focus', { x: 380, y: taskY - 32 })
  if (focusedTask.value) positions.set(focusedTask.value.id, { x: 380, y: taskY })
  positions.set('system:blackboard', { x: 410, y: taskY + 168 })
  return positions
}
// positionFor 返回稳定的舞台位置，已有拖动位置由调用方保留。
function positionFor(positions: Map<string, CollaborationPoint>, id: string) {
  return positions.get(id) || { x: 0, y: 0 }
}
// systemSnapshot 用运行和阶段快照显示角色状态，历史事件不制造持续忙碌。
function systemSnapshot(kind: SystemKind) {
  if (kind === 'planner') {
    if (props.runStatus === 'planning')
      return { stateLabel: '处理中', detail: '生成并校验计划', busy: true }
    if (props.plannerFailure || plannerEvent.value?.event_type === 'planner.failed')
      return { stateLabel: '规划失败', detail: '查看服务端错误', busy: false }
    return {
      stateLabel: stage.value ? statusLabel(stage.value.status) : '待命',
      detail: stage.value?.name || '创建与审核',
      busy: false,
    }
  }
  if (kind === 'scheduler')
    return {
      stateLabel: props.runStatus === 'planning' ? '等待计划' : '按能力调度',
      detail: '依赖 · 预算 · 租约',
      busy: false,
    }
  return {
    stateLabel: `${props.facts.length} 条事实`,
    detail: '事实快照 · 非任务完成推断',
    busy: false,
  }
}
// syncGraph 只渲染当前任务和协作者，完整任务集合由下方队列承载。
function syncGraph() {
  const positions = new Map(nodes.value.map((node) => [node.id, node.position]))
  const previousIDs = new Set(nodes.value.map((node) => node.id))
  const freshIDs = new Set<string>()
  const defaults = layout()
  const workerTasks = new Map<string, Task[]>()
  for (const task of props.tasks) {
    if (!task.assigned_worker_id) continue
    const group = workerTasks.get(task.assigned_worker_id) || []
    group.push(task)
    workerTasks.set(task.assigned_worker_id, group)
  }
  const workerFacts = new Map<string, Fact[]>()
  for (const fact of props.facts) {
    if (!fact.source_worker_id) continue
    const group = workerFacts.get(fact.source_worker_id) || []
    group.push(fact)
    workerFacts.set(fact.source_worker_id, group)
  }
  // makeWorker 保留拖动位置并注入真实任务、阶段和事实快照。
  const makeWorker = (worker: Worker) => ({
    id: worker.id,
    type: 'worker',
    position: positions.get(worker.id) || positionFor(defaults, worker.id),
    data: {
      worker,
      tasks: workerTasks.get(worker.id) || [],
      facts: workerFacts.get(worker.id) || [],
      name: workerNames.value.get(worker.id),
    },
    draggable: true,
  })
  nodes.value = [
    ...SYSTEMS.map((system) => ({
      id: system.id,
      type: 'system',
      position: positions.get(system.id) || positionFor(defaults, system.id),
      data: { kind: system.kind, ...systemSnapshot(system.kind) },
      draggable: false,
      selectable: false,
    })),
    {
      id: 'lane:focus',
      type: 'label',
      position: positionFor(defaults, 'lane:focus'),
      data: {
        title: '当前协作任务',
        detail: focusedTask.value ? statusLabel(focusedTask.value.status) : '等待任务',
      },
      draggable: false,
      selectable: false,
    },
    ...props.workers.map(makeWorker),
  ]
  const task = focusedTask.value
  if (task) {
    if (previousIDs.size && !previousIDs.has(task.id)) freshIDs.add(task.id)
    const node: Node = {
      id: task.id,
      type: 'task',
      position: positions.get(task.id) || positionFor(defaults, task.id),
      data: { task },
      class: freshIDs.has(task.id) ? 'node-enter' : '',
      draggable: true,
    }
    nodes.value.push(node)
  }
  if (!fitted && task) {
    fitted = true
    fitGraph()
  } else if (followNew.value && freshIDs.size) {
    fitGraph(240)
  }
}
// arrange 恢复纵向角色布局并居中适配完整舞台。
function arrange() {
  const defaults = layout()
  nodes.value = nodes.value.map((node) => ({
    ...node,
    position: positionFor(defaults, node.id),
  }))
  fitGraph(220)
}
// toggleFollow 控制当前任务切换时是否自动适配舞台。
function toggleFollow() {
  followNew.value = !followNew.value
  if (followNew.value) fitGraph(220)
}
// selectNode 只允许当前任务节点打开详情。
function selectNode(id: string, type?: string) {
  if (type === 'task') emit('select', id)
}
// nodeCenter 从当前节点位置计算角色动画锚点。
function nodeCenter(id: string) {
  const node = nodes.value.find((item) => item.id === id)
  if (!node) return null
  const size =
    node.type === 'worker' ? WORKER_SIZE : node.type === 'system' ? SYSTEM_SIZE : TASK_SIZE
  return { x: node.position.x + size.width / 2, y: node.position.y + size.height / 2, size }
}
// journey 计算短幅弧线关键帧，不绘制或保留协作路径。
function journey(fromID: string, toID: string) {
  const source = nodeCenter(fromID)
  const target = nodeCenter(toID)
  if (!source || !target || fromID === toID) return null
  const dx = target.x - source.x
  const dy = target.y - source.y
  const distance = Math.hypot(dx, dy)
  if (!distance) return null
  const unit = { x: dx / distance, y: dy / distance }
  const from = {
    x: source.x + unit.x * (source.size.width / 2 + 10),
    y: source.y + unit.y * (source.size.height / 2 + 10),
  }
  const to = {
    x: target.x - unit.x * (target.size.width / 2 + 10),
    y: target.y - unit.y * (target.size.height / 2 + 10),
  }
  const curve = Math.min(28, Math.max(12, distance * 0.05))
  return {
    from,
    to,
    arc: { x: (from.x + to.x) / 2 + unit.y * curve, y: (from.y + to.y) / 2 - unit.x * curve },
  }
}
// linkPath 为当前交接生成与角色关键帧一致的临时二次曲线。
function linkPath(activity: CollaborationActivity) {
  return `M ${activity.from.x} ${activity.from.y} Q ${activity.arc.x} ${activity.arc.y} ${activity.to.x} ${activity.to.y}`
}
// activityFor 将真实事件映射到当前任务及职责明确的协作者。
function activityFor(event: EngineEvent): CollaborationActivity | null {
  const task = props.tasks.find((item) => item.id === event.task_id)
  const workerID = event.worker_id || task?.assigned_worker_id || ''
  const route = (
    fromID: string,
    toID: string,
    role: string,
    action: string,
    kind: CollaborationKind,
  ) => {
    const points = journey(fromID, toID)
    return points ? { id: event.id, role, action, kind, fromID, toID, ...points } : null
  }
  if (event.event_type === 'planner.started') return null
  if (event.event_type === 'planner.completed')
    return route('system:planner', 'system:scheduler', '规划员', '交付已校验计划', 'planning')
  if (event.event_type === 'planner.failed') return null
  if (event.event_type.startsWith('stage.')) {
    const eventStage = props.stages.find(
      (item) =>
        item.id === event.payload.stage_id ||
        item.key === event.payload.key ||
        item.key === event.payload.stage_key,
    )
    if (!eventStage) return null
    if (event.event_type === 'stage.running')
      return route('system:planner', 'system:scheduler', '规划员', '推进阶段', 'planning')
    if (event.event_type === 'stage.reviewing' && eventStage.reviewer_id)
      return route(
        'system:blackboard',
        eventStage.reviewer_id,
        '复核员',
        eventStage.review_mode === 'self_review' ? '读取事实自检' : '读取事实复核',
        'knowledge',
      )
    if (['stage.achieved', 'stage.needs_work', 'stage.failed'].includes(event.event_type))
      return route(
        'system:blackboard',
        'system:planner',
        '复核员',
        event.event_type === 'stage.achieved'
          ? '阶段验收通过'
          : event.event_type === 'stage.failed'
            ? '反馈阶段失败'
            : '反馈补充原因',
        event.event_type === 'stage.failed' ? 'failure' : 'planning',
      )
    return null
  }
  if (event.event_type === 'run.started' && props.runStatus !== 'planning' && !plannerEvent.value)
    return route('system:planner', 'system:scheduler', '规划员', '启动既有计划', 'planning')
  if (event.event_type === 'run.completed')
    return route('system:blackboard', 'system:planner', '规划员', '汇总结果', 'planning')
  if (event.event_type === 'worker.registered')
    return route('system:scheduler', workerID, '调度员', '注册执行者', 'assignment')
  if (!task) return null
  if (event.event_type === 'task.claimed')
    return route('system:scheduler', workerID, '调度员', '分配执行者', 'assignment')
  if (['task.started', 'tool.called'].includes(event.event_type))
    return route(
      workerID,
      task.id,
      '执行者',
      event.event_type === 'task.started' ? '开始执行' : '调用工具',
      'execution',
    )
  if (['task.completed', 'task.failed', 'task.retry_wait'].includes(event.event_type))
    return route(
      task.id,
      workerID,
      '执行者',
      event.event_type === 'task.completed'
        ? '回传结果'
        : event.event_type === 'task.failed'
          ? '反馈异常'
          : '申请重试',
      event.event_type === 'task.failed' ? 'failure' : 'execution',
    )
  if (event.event_type === 'fact.created')
    return route(task.id, 'system:blackboard', '记录员', '写入事实', 'knowledge')
  if (event.event_type === 'proposal.created')
    return route(workerID, 'system:planner', '规划员', '提交建议', 'planning')
  if (event.event_type === 'proposal.reviewed')
    return route(
      'system:planner',
      task.id,
      '规划员',
      event.payload.status === 'approved' ? '批准建议' : '退回建议',
      'planning',
    )
  if (event.event_type === 'task.created')
    return route('system:planner', task.id, '规划员', '创建任务', 'planning')
  if (event.event_type === 'task.ready')
    return route('system:scheduler', task.id, '协调员', '解锁依赖', 'dependency')
  return null
}
// removeActivity 在动画结束后释放临时角色。
function removeActivity(id: string) {
  activities.value = activities.value.filter((activity) => activity.id !== id)
}
watch(
  () => [
    props.tasks,
    props.workers,
    props.facts,
    props.stages,
    props.runStatus,
    props.plannerFailure,
    props.paused,
    focusedTask.value?.id,
    plannerEvent.value?.event_type,
  ],
  syncGraph,
  {
    immediate: true,
  },
)
watch(
  () => props.events,
  (events) => {
    const latest = events.at(-1)
    if (!latest || !eventsInitialized || latest.sequence < observedSequence) {
      activities.value = []
      observedSequence = latest?.sequence || 0
      eventsInitialized = Boolean(latest)
      return
    }
    const fresh = events.filter((event) => event.sequence > observedSequence)
    observedSequence = latest.sequence
    if (!fresh.length || props.paused) return
    if (skipResumedBatch) {
      skipResumedBatch = false
      return
    }
    if (activities.value.length) return
    const incoming = [...fresh]
      .reverse()
      .map(activityFor)
      .filter((activity): activity is CollaborationActivity => Boolean(activity))
      .slice(0, 1)
    if (incoming.length) activities.value = incoming
  },
  { immediate: true, flush: 'post' },
)
watch(
  () => props.paused,
  (paused) => {
    activities.value = []
    if (paused) skipResumedBatch = true
  },
)
watch(
  () => props.selected,
  () => {
    nodes.value = nodes.value.map((node) => ({
      ...node,
      selected: node.type === 'task' && node.id === props.selected,
    }))
  },
)
</script>

<template>
  <section class="graph-shell" :class="{ 'animations-paused': paused }">
    <header class="graph-toolbar">
      <div class="section-title">
        <ApartmentOutlined />
        <div>
          <strong>实时协作</strong>
          <span class="muted">Planner → Scheduler → Worker · {{ workers.length }} 位执行者</span>
        </div>
      </div>
      <div class="graph-actions">
        <a-tooltip title="当前任务变化时自动适配完整舞台">
          <a-button size="small" :type="followNew ? 'primary' : 'default'" @click="toggleFollow">
            <AimOutlined />跟随协作
          </a-button>
        </a-tooltip>
        <a-tooltip title="恢复纵向角色布局并居中">
          <a-button size="small" @click="arrange"><DeploymentUnitOutlined />整理阵容</a-button>
        </a-tooltip>
        <a-tooltip title="缩放至完整视图">
          <a-button size="small" aria-label="适配完整视图" @click="fitGraph(220)">
            <ExpandOutlined />
          </a-button>
        </a-tooltip>
      </div>
    </header>
    <div class="graph-statusbar">
      <span class="flow-note">真实事件驱动 · 关键协作即时呈现</span>
      <div class="latest-event" :class="{ idle: !latestEvent }">
        <span class="live-dot" :class="{ paused: paused }" />
        <strong>{{ latestEvent ? eventLabel(latestEvent.event_type) : '等待运行事件' }}</strong>
        <small v-if="latestEvent">{{ timeLabel(latestEvent.created_at) }}</small>
      </div>
    </div>
    <div class="graph-canvas">
      <VueFlow
        :id="flowId"
        v-model:nodes="nodes"
        :min-zoom="0.35"
        :max-zoom="1.8"
        :nodes-connectable="false"
        :edges-focusable="false"
        :delete-key-code="null"
        :edges-updatable="false"
        @node-click="selectNode($event.node.id, $event.node.type)"
      >
        <Background :gap="28" :size="1" pattern-color="#293844" />
        <Controls :show-interactive="false" />
        <template #node-system="{ data }">
          <SystemNode
            :kind="data.kind"
            :active="data.busy || activeSystems.has('system:' + data.kind)"
            :state-label="data.stateLabel"
            :detail="data.detail"
            :paused="paused"
          />
        </template>
        <template #node-label="{ data }">
          <div class="focus-label">
            <strong>{{ data.title }}</strong
            ><span>{{ data.detail }}</span>
          </div>
        </template>
        <template #node-task="{ data, selected: nodeSelected }">
          <TaskNode
            :task="data.task"
            :selected="nodeSelected"
            :paused="paused"
            :worker-name="
              data.task.assigned_worker_id
                ? workerNames.get(data.task.assigned_worker_id)
                : undefined
            "
            @select="emit('select', $event)"
          />
        </template>
        <template #node-worker="{ data }">
          <WorkerNode
            :worker="data.worker"
            :tasks="data.tasks"
            :facts="data.facts"
            :stages="stages"
            :display-name="data.name"
            :paused="paused"
          />
        </template>
      </VueFlow>
      <div class="collaboration-viewport" :style="collaborationTransform" aria-hidden="true">
        <svg class="collaboration-links">
          <path
            v-for="activity in activities"
            :key="activity.id"
            :d="linkPath(activity)"
            class="collaboration-link"
            :class="'link-' + activity.kind"
            pathLength="1"
            vector-effect="non-scaling-stroke"
          />
        </svg>
        <CollaborationActor
          v-for="activity in activities"
          :key="activity.id"
          v-bind="activity"
          :paused="paused"
          :zoom="viewport.zoom"
          @finished="removeActivity"
        />
      </div>
      <Transition name="panel">
        <div v-if="!tasks.length" class="graph-empty" role="status">
          <a-empty :description="emptyDescription" />
        </div>
      </Transition>
    </div>
    <div class="task-roster-heading">
      <span
        >任务队列 <small>{{ tasks.length }}</small></span
      >
      <span>点击任务查看详情 · 当前任务自动进入中央舞台</span>
    </div>
    <TransitionGroup name="list" tag="div" class="task-roster">
      <button
        v-for="task in tasks"
        :key="task.id"
        class="roster-task"
        :class="{ current: focusedTask?.id === task.id, active: isActive(task) }"
        @click="emit('select', task.id)"
      >
        <i :class="'state-' + task.status" />
        <span>
          <strong>{{ task.name }}</strong>
          <small
            >{{ typeNames[task.task_type] || task.task_type }} ·
            {{ statusLabel(task.status) }}</small
          >
        </span>
      </button>
    </TransitionGroup>
  </section>
</template>

<style scoped>
.graph-shell {
  min-width: 0;
  overflow: hidden;
  background: var(--surface-panel);
  border: 1px solid var(--line-subtle);
  border-radius: 12px;
}
.graph-toolbar {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 20px;
  border-bottom: 1px solid var(--line-subtle);
}
.graph-toolbar .section-title > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.graph-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.graph-actions .ant-btn {
  font-size: 12px;
}
.graph-statusbar,
.latest-event {
  display: flex;
  align-items: center;
}
.graph-statusbar {
  min-height: 44px;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 20px;
  background: var(--surface-inset);
  border-bottom: 1px solid var(--line-subtle);
}
.flow-note {
  color: var(--text-tertiary);
  font-size: 12px;
}
.latest-event {
  min-width: 0;
  gap: 7px;
  color: var(--text-secondary);
  font-size: 12px;
}
.latest-event strong {
  overflow: hidden;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.latest-event small {
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  white-space: nowrap;
}
.latest-event.idle strong {
  color: var(--text-tertiary);
  font-weight: 400;
}
.graph-canvas {
  position: relative;
  height: 620px;
  background: var(--surface-base);
}
.focus-label {
  display: flex;
  align-items: baseline;
  gap: 8px;
  width: 280px;
  justify-content: center;
  pointer-events: none;
}
.focus-label strong {
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}
.focus-label span {
  color: var(--accent);
  font-size: 12px;
}
.collaboration-viewport {
  position: absolute;
  z-index: 5;
  inset: 0;
  overflow: visible;
  pointer-events: none;
  transform-origin: 0 0;
}
.collaboration-links {
  position: absolute;
  inset: 0;
  overflow: visible;
}
.collaboration-link {
  fill: none;
  stroke: var(--info);
  stroke-width: 1.5px;
  stroke-linecap: round;
  stroke-dasharray: 1;
  stroke-dashoffset: 1;
  opacity: 0;
  animation: link-lifecycle 1.9s cubic-bezier(0.4, 0, 0.2, 1) both;
}
.link-assignment,
.link-knowledge {
  stroke: var(--accent);
}
.link-planning {
  stroke: var(--warning);
}
.link-failure {
  stroke: var(--danger);
}
.animations-paused .collaboration-link {
  animation-play-state: paused;
}
@keyframes link-lifecycle {
  0% {
    opacity: 0;
    stroke-dashoffset: 1;
  }
  20% {
    opacity: 0.65;
    stroke-dashoffset: 0;
  }
  78% {
    opacity: 0.45;
    stroke-dashoffset: 0;
  }
  100% {
    opacity: 0;
    stroke-dashoffset: 0;
  }
}
.graph-empty {
  position: absolute;
  inset: 160px 0 0;
  display: grid;
  place-items: center;
  pointer-events: none;
}
.task-roster-heading {
  display: flex;
  min-height: 44px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 20px 0;
  color: var(--text-tertiary);
  border-top: 1px solid var(--line-subtle);
  font-size: 12px;
}
.task-roster-heading > span:first-child {
  color: var(--text-secondary);
  font-weight: 550;
}
.task-roster-heading small {
  margin-left: 5px;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
}
.task-roster {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  padding: 10px 20px 16px;
}
.roster-task {
  display: flex;
  min-width: 190px;
  min-height: 58px;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  color: var(--text-secondary);
  text-align: left;
  background: var(--surface-inset);
  border: 1px solid var(--line-subtle);
  border-radius: 8px;
}
.roster-task:hover,
.roster-task.current {
  color: var(--text-primary);
  background: var(--surface-raised);
  border-color: var(--line-strong);
}
.roster-task.active {
  border-color: var(--accent-muted);
}
.roster-task > i {
  width: 7px;
  height: 7px;
  flex: none;
  background: var(--text-tertiary);
  border-radius: 50%;
}
.roster-task > i.state-running,
.roster-task > i.state-claimed,
.roster-task > i.state-completed {
  background: var(--accent);
}
.roster-task > i.state-failed,
.roster-task > i.state-timeout {
  background: var(--danger);
}
.roster-task > span {
  min-width: 0;
}
.roster-task strong,
.roster-task small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.roster-task strong {
  font-size: 13px;
  font-weight: 550;
}
.roster-task small {
  margin-top: 3px;
  color: var(--text-tertiary);
  font-size: 12px;
}
:deep(.vue-flow__controls) {
  overflow: hidden;
  background: var(--surface-raised);
  border: 1px solid var(--line-strong);
  border-radius: 8px;
  box-shadow: none;
}
:deep(.vue-flow__controls-button) {
  width: 30px;
  height: 30px;
  color: var(--text-secondary);
  background: var(--surface-raised);
  border-bottom-color: var(--line-subtle);
}
:deep(.vue-flow__controls-button:hover) {
  background: var(--surface-hover);
}
:deep(.vue-flow__controls-button svg) {
  fill: currentColor;
}
:deep(.vue-flow__node.node-enter) {
  animation: node-enter 200ms ease-out both;
}
@keyframes node-enter {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}
@media (max-width: 860px) {
  .graph-toolbar,
  .graph-statusbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .graph-actions {
    width: 100%;
    flex-wrap: wrap;
  }
  .graph-statusbar {
    gap: 6px;
  }
  .graph-canvas {
    height: 520px;
  }
  .task-roster-heading > span:last-child {
    display: none;
  }
}
@media (max-width: 560px) {
  .graph-actions .ant-btn {
    min-height: 44px;
  }
  .graph-canvas {
    height: 480px;
  }
}
@media (prefers-reduced-motion: reduce) {
  :deep(.vue-flow__node.node-enter) {
    animation: none;
  }
  .collaboration-viewport {
    display: none;
  }
}
</style>
