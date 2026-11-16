<script setup lang="ts">
import { computed } from 'vue'
import { BranchesOutlined, DeploymentUnitOutlined, NodeIndexOutlined } from '@ant-design/icons-vue'
import type { Task } from '@/types/engine'
import { statusLabel, typeNames, workerDisplayName } from '@/utils/presentation'

const props = defineProps<{
  task: Task
  selected: boolean
  paused: boolean
  workerName?: string
}>()
const emit = defineEmits<{ select: [id: string] }>()
const active = computed(() => ['running', 'claimed'].includes(props.task.status))
</script>

<template>
  <div
    class="task-node"
    :class="[
      'state-' + task.status,
      { 'is-selected': selected, 'is-active': active, 'is-paused': paused },
    ]"
    role="button"
    tabindex="0"
    :aria-label="task.name + '，' + statusLabel(task.status)"
    @keydown.enter="emit('select', task.id)"
    @keydown.space.prevent="emit('select', task.id)"
  >
    <div class="node-top">
      <span class="node-type"
        ><NodeIndexOutlined />{{ typeNames[task.task_type] || task.task_type }}</span
      >
      <span class="node-status"><i />{{ statusLabel(task.status) }}</span>
    </div>
    <strong class="node-name">{{ task.name }}</strong>
    <div class="node-target">{{ task.input.target }}</div>
    <div class="node-footer">
      <span class="node-worker"
        ><DeploymentUnitOutlined />{{
          workerName ||
          (task.assigned_worker_id ? workerDisplayName(task.assigned_worker_id) : '等待能力匹配')
        }}</span
      >
      <span class="node-origin" :class="{ dynamic: task.parent_id }"
        ><BranchesOutlined v-if="task.parent_id" />{{
          task.parent_id ? '增量规划' : '初始计划'
        }}</span
      >
    </div>
  </div>
</template>

<style scoped>
.task-node {
  position: relative;
  width: 280px;
  height: 156px;
  padding: 16px 18px;
  color: var(--text-primary);
  background: var(--node-bg);
  border: 1px solid var(--node-border);
  border-radius: 10px;
  box-shadow: 0 10px 28px oklch(0.08 0.01 240 / 0.22);
  cursor: pointer;
  transition:
    transform 160ms ease-out,
    border-color 160ms ease-out,
    box-shadow 160ms ease-out,
    background-color 160ms ease-out;
}
.task-node:hover {
  transform: translateY(-2px);
  border-color: var(--node-hover);
  background: var(--node-hover-bg);
}
.task-node.is-selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px var(--accent-soft);
}
.task-node:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
.node-top,
.node-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.node-type,
.node-status,
.node-footer {
  font-size: 12px;
  line-height: 18px;
}
.node-type {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-secondary);
  font-weight: 550;
}
.node-type .anticon {
  color: var(--text-tertiary);
}
.node-status {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-secondary);
  white-space: nowrap;
}
.node-status i {
  position: relative;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}
.is-active .node-status i::after {
  content: '';
  position: absolute;
  inset: -4px;
  border: 1px solid currentColor;
  border-radius: inherit;
  animation: node-signal 1.8s ease-out infinite;
}
.is-paused .node-status i::after {
  animation-play-state: paused;
}
.node-name {
  display: block;
  margin-top: 10px;
  overflow: hidden;
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 620;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-target {
  margin-top: 2px;
  overflow: hidden;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-footer {
  margin-top: 10px;
  padding-top: 8px;
  color: var(--text-tertiary);
  border-top: 1px solid var(--line-subtle);
}
.node-worker,
.node-origin {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-origin {
  flex: none;
}
.node-origin.dynamic {
  color: var(--warning);
}
.is-active {
  border-color: var(--accent-muted);
  background: var(--node-active-bg);
}
.is-active .node-status,
.state-running .node-worker,
.state-claimed .node-worker {
  color: var(--accent);
}
.state-completed .node-status {
  color: var(--success);
}
.state-blocked .node-status,
.state-retry_wait .node-status {
  color: var(--warning);
}
.state-failed .node-status,
.state-timeout .node-status {
  color: var(--danger);
}
@keyframes node-signal {
  from {
    opacity: 0.75;
    transform: scale(0.65);
  }
  to {
    opacity: 0;
    transform: scale(1.65);
  }
}
@media (prefers-reduced-motion: reduce) {
  .task-node {
    transition: none;
  }
  .task-node:hover {
    transform: none;
  }
  .is-active .node-status i::after {
    animation: none;
  }
}
</style>
