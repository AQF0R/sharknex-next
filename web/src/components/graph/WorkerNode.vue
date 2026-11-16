<script setup lang="ts">
import { computed } from 'vue'
import { ClockCircleOutlined, RobotOutlined } from '@ant-design/icons-vue'
import type { Fact, StageSummary, Task, Worker } from '@/types/engine'
import { statusLabel, taskStage, timeLabel } from '@/utils/presentation'

const props = defineProps<{
  worker: Worker
  tasks: Task[]
  facts: Fact[]
  stages: StageSummary[]
  displayName: string
  paused: boolean
}>()
const activeTasks = computed(() =>
  props.tasks.filter((task) => ['running', 'claimed'].includes(task.status)),
)
const currentTask = computed(
  () =>
    props.tasks.find((task) => task.status === 'running') ||
    props.tasks.find((task) => task.status === 'claimed') ||
    props.tasks.at(-1),
)
const completedTasks = computed(
  () => props.tasks.filter((task) => task.status === 'completed').length,
)
const progress = computed(() =>
  props.tasks.length ? Math.min(completedTasks.value / props.tasks.length, 1) : 0,
)
const taskSummary = computed(() => {
  if (!currentTask.value) return '暂无分配任务'
  return (activeTasks.value.length ? '正在：' : '最近：') + currentTask.value.name
})
const stage = computed(() => taskStage(currentTask.value, props.stages))
const factsSummary = computed(() => {
  const facts = props.facts.filter((fact) => fact.source_task_id === currentTask.value?.id)
  if (!facts.length) return '该任务暂无共享事实'
  const statuses = [...new Set(facts.map((fact) => statusLabel(fact.status)))]
  return `事实 ${facts.length} 条 · ${statuses.join(' / ')}`
})
// collaborationStage 优先显示服务端阶段验收，旧任务仅显示实际任务状态。
const collaborationStage = computed(() => {
  const task = currentTask.value
  if (!task) return '等待 Planner 规划与 Scheduler 分配'
  if (stage.value) {
    const review =
      stage.value.review_mode === 'self_review'
        ? ' · 单执行者自检'
        : stage.value.review_mode === 'cross_review'
          ? ' · 交叉只读复核'
          : ''
    return `${stage.value.name} · ${statusLabel(stage.value.status)}${review}`
  }
  return `${task.task_type === 'stage_review' ? '只读复核' : '任务'}${statusLabel(task.status)} · 无阶段验收快照`
})
</script>

<template>
  <div
    class="worker-node"
    :class="['state-' + worker.status, { 'is-working': activeTasks.length, 'is-paused': paused }]"
  >
    <div class="worker-top">
      <span class="worker-identity"><RobotOutlined /></span>
      <div class="worker-name">
        <strong>{{ displayName }}</strong>
        <span :title="currentTask?.name">{{ taskSummary }}</span>
      </div>
      <span class="worker-status"><i />{{ statusLabel(worker.status) }}</span>
    </div>
    <div class="worker-load">
      <span>任务进度</span>
      <strong
        >{{ completedTasks }}<small> / {{ tasks.length }} 已完成</small></strong
      >
    </div>
    <div
      class="worker-track"
      role="progressbar"
      aria-label="Worker 已分配任务完成进度"
      :aria-valuenow="Math.round(progress * 100)"
      aria-valuemin="0"
      aria-valuemax="100"
    >
      <span :style="{ transform: 'scaleX(' + progress + ')' }" />
    </div>
    <div class="worker-current" :title="collaborationStage">{{ collaborationStage }}</div>
    <div
      class="worker-heartbeat"
      :title="factsSummary + ' · 心跳 ' + timeLabel(worker.last_heartbeat_at)"
    >
      <ClockCircleOutlined /><span>{{ factsSummary }}</span>
    </div>
  </div>
</template>

<style scoped>
.worker-node {
  width: 216px;
  height: 144px;
  padding: 13px 14px;
  color: var(--text-primary);
  background: var(--worker-bg);
  border: 1px solid var(--worker-border);
  border-radius: 10px;
  transition:
    border-color var(--motion-base) ease-out,
    background-color var(--motion-base) ease-out;
}
.worker-top {
  display: flex;
  align-items: center;
  gap: 8px;
}
.worker-identity {
  display: grid;
  width: 28px;
  height: 28px;
  flex: none;
  place-items: center;
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: 7px;
  font-size: 15px;
}
.worker-name {
  min-width: 0;
  flex: 1;
}
.worker-name strong,
.worker-name span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.worker-name strong {
  font-size: 13px;
  font-weight: 600;
}
.worker-name span {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 12px;
}
.worker-status {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--text-secondary);
  font-size: 12px;
  white-space: nowrap;
}
.worker-status i {
  width: 5px;
  height: 5px;
  background: currentColor;
  border-radius: 50%;
}
.state-busy .worker-status {
  color: var(--accent);
}
.state-offline .worker-status {
  color: var(--danger);
}
.worker-load {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-top: 10px;
  color: var(--text-tertiary);
  font-size: 12px;
}
.worker-load strong {
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-weight: 500;
}
.worker-load small {
  color: var(--text-tertiary);
}
.worker-track {
  height: 3px;
  margin-top: 5px;
  overflow: hidden;
  background: var(--line-subtle);
  border-radius: 3px;
}
.worker-track span {
  display: block;
  width: 100%;
  height: 100%;
  background: var(--accent);
  transform-origin: left center;
  transition: transform 220ms ease-out;
}
.worker-current {
  margin-top: 7px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 17px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.worker-heartbeat {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 4px;
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 16px;
}
.worker-heartbeat span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.is-working {
  border-color: var(--accent-muted);
}
.is-working .worker-identity {
  color: var(--surface-inset);
  background: var(--accent);
  animation: worker-action 900ms ease-in-out infinite alternate;
}
.is-working .worker-status i {
  animation: worker-signal 900ms ease-in-out infinite alternate;
}
.is-paused .worker-identity,
.is-paused .worker-status i {
  animation-play-state: paused;
}
@keyframes worker-action {
  from {
    transform: translateY(0);
  }
  to {
    transform: translateY(-2px);
  }
}
@keyframes worker-signal {
  from {
    opacity: 0.45;
  }
  to {
    opacity: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .worker-track span {
    transition: none;
  }
  .is-working .worker-identity,
  .is-working .worker-status i {
    animation: none;
  }
}
</style>
