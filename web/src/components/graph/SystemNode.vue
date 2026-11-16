<script setup lang="ts">
import { computed } from 'vue'
import { ApartmentOutlined, DatabaseOutlined, DeploymentUnitOutlined } from '@ant-design/icons-vue'

export type SystemKind = 'planner' | 'scheduler' | 'blackboard'

const props = defineProps<{
  kind: SystemKind
  active: boolean
  paused: boolean
  stateLabel?: string
  detail?: string
}>()
const content = {
  planner: {
    name: 'Planner',
    role: '规划员',
    detail: '创建与审核',
    icon: ApartmentOutlined,
  },
  scheduler: {
    name: 'Scheduler',
    role: '调度员',
    detail: '分配与解锁',
    icon: DeploymentUnitOutlined,
  },
  blackboard: {
    name: 'Blackboard',
    role: '记录员',
    detail: '事实共享',
    icon: DatabaseOutlined,
  },
}
const current = computed(() => content[props.kind])
</script>

<template>
  <div class="system-node" :class="['kind-' + kind, { 'is-active': active && !paused }]">
    <span class="system-icon"><component :is="current.icon" /></span>
    <div class="system-copy">
      <strong>{{ current.name }}</strong>
      <span :title="detail || current.detail"
        >{{ current.role }} · {{ detail || current.detail }}</span
      >
    </div>
    <span class="system-state"><i />{{ stateLabel || (active ? '协作中' : '待命') }}</span>
  </div>
</template>

<style scoped>
.system-node {
  --system-color: var(--info);
  position: relative;
  display: flex;
  width: 220px;
  height: 80px;
  align-items: center;
  gap: 10px;
  padding: 14px;
  background: var(--surface-panel);
  border: 1px solid var(--system-color);
  border-radius: 4px;
  transition:
    border-color var(--motion-base) ease-out,
    background-color var(--motion-base) ease-out;
}
.kind-scheduler {
  --system-color: var(--accent);
  border-radius: 12px;
}
.kind-blackboard {
  --system-color: var(--warning);
  border-radius: 22px;
}
.system-node.is-active {
  background: var(--surface-raised);
  border-color: var(--system-color);
}
.system-icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: none;
  place-items: center;
  color: var(--system-color);
  background: var(--surface-inset);
  border-radius: 8px;
  font-size: 17px;
}
.system-copy {
  min-width: 0;
  flex: 1;
}
.system-copy strong,
.system-copy span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.system-copy strong {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 600;
}
.system-copy span {
  margin-top: 3px;
  color: var(--text-tertiary);
  font-size: 12px;
}
.system-state {
  position: absolute;
  top: 7px;
  right: 9px;
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--text-tertiary);
  font-size: 12px;
  transform: scale(0.85);
  transform-origin: right top;
}
.system-state i {
  width: 5px;
  height: 5px;
  background: currentColor;
  border-radius: 50%;
}
.is-active .system-state {
  color: var(--system-color);
}
@media (prefers-reduced-motion: reduce) {
  .system-node {
    transition: none;
  }
}
</style>
