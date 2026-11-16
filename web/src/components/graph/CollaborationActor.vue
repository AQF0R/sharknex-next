<script setup lang="ts">
import { computed } from 'vue'
import {
  ApartmentOutlined,
  BranchesOutlined,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  RobotOutlined,
} from '@ant-design/icons-vue'

export type CollaborationKind =
  'assignment' | 'execution' | 'knowledge' | 'planning' | 'dependency' | 'failure'

export interface CollaborationPoint {
  x: number
  y: number
}

const props = defineProps<{
  id: string
  role: string
  action: string
  kind: CollaborationKind
  from: CollaborationPoint
  arc: CollaborationPoint
  to: CollaborationPoint
  paused: boolean
  zoom: number
}>()
const emit = defineEmits<{ finished: [id: string] }>()
const icon = computed(() => {
  if (props.kind === 'assignment') return DeploymentUnitOutlined
  if (props.kind === 'knowledge') return DatabaseOutlined
  if (props.kind === 'planning') return ApartmentOutlined
  if (props.kind === 'dependency') return BranchesOutlined
  return RobotOutlined
})
const reverse = computed(() => props.to.x < props.from.x)
// journeyStyle 抵消视口缩放，让小人移动时仍保持屏幕可读尺寸。
const journeyStyle = computed(() => {
  const inverseZoom = 1 / Math.max(props.zoom, 0.16)
  return {
    '--from-x': props.from.x + 'px',
    '--from-y': props.from.y + 'px',
    '--arc-x': props.arc.x + 'px',
    '--arc-y': props.arc.y + 'px',
    '--to-x': props.to.x + 'px',
    '--to-y': props.to.y + 'px',
    '--enter-scale': 0.86 * inverseZoom,
    '--actor-scale': inverseZoom,
    '--peak-scale': inverseZoom,
    '--exit-scale': 0.92 * inverseZoom,
  }
})
</script>

<template>
  <div
    class="collaboration-actor"
    :class="['kind-' + kind, { 'is-paused': paused, reverse }]"
    :style="journeyStyle"
    @animationend.self="emit('finished', id)"
  >
    <span class="actor-person" aria-hidden="true">
      <i class="person-head" />
      <i class="person-body" />
      <i class="person-arm" />
      <i class="person-leg left" />
      <i class="person-leg right" />
      <span class="person-prop"><component :is="icon" /></span>
    </span>
    <span class="actor-copy">
      <strong>{{ role }}</strong>
      <small>{{ action }}</small>
    </span>
  </div>
</template>

<style scoped>
.collaboration-actor {
  --actor-color: var(--info);
  position: absolute;
  top: 0;
  left: 0;
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 136px;
  padding: 6px 11px 6px 8px;
  color: var(--text-primary);
  background: var(--surface-raised);
  border: 1px solid var(--actor-color);
  border-radius: 10px;
  box-shadow: 0 8px 24px oklch(0.08 0.01 240 / 0.32);
  opacity: 0;
  pointer-events: none;
  animation: actor-journey 1.9s cubic-bezier(0.4, 0, 0.2, 1) both;
  will-change: transform, opacity;
}
.kind-assignment,
.kind-knowledge {
  --actor-color: var(--accent);
}
.kind-planning {
  --actor-color: var(--warning);
}
.kind-failure {
  --actor-color: var(--danger);
}
.actor-person {
  position: relative;
  width: 38px;
  height: 42px;
  flex: none;
  transform-origin: center;
}
.reverse .actor-person {
  transform: scaleX(-1);
}
.person-head,
.person-body,
.person-arm,
.person-leg {
  position: absolute;
  display: block;
  background: var(--actor-color);
}
.person-head {
  top: 1px;
  left: 13px;
  width: 12px;
  height: 12px;
  border: 2px solid var(--surface-raised);
  border-radius: 50%;
  box-shadow: 0 0 0 1px var(--actor-color);
}
.person-body {
  top: 14px;
  left: 11px;
  width: 16px;
  height: 16px;
  border-radius: 6px 6px 4px 4px;
}
.person-arm {
  top: 16px;
  left: 7px;
  width: 4px;
  height: 14px;
  border-radius: 3px;
  transform-origin: 2px 2px;
  animation: person-work 480ms ease-in-out infinite alternate;
}
.person-leg {
  top: 28px;
  width: 5px;
  height: 12px;
  border-radius: 3px;
  transform-origin: center top;
  animation: person-step 480ms ease-in-out infinite alternate;
}
.person-leg.left {
  left: 11px;
}
.person-leg.right {
  left: 22px;
  animation-direction: alternate-reverse;
}
.person-prop {
  position: absolute;
  top: 17px;
  right: -1px;
  display: grid;
  width: 16px;
  height: 16px;
  place-items: center;
  color: var(--actor-color);
  background: var(--surface-base);
  border: 1px solid var(--actor-color);
  border-radius: 3px;
  font-size: 10px;
}
.actor-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  line-height: 1.25;
  white-space: nowrap;
}
.actor-copy strong {
  font-size: 12px;
  font-weight: 600;
}
.actor-copy small {
  margin-top: 3px;
  color: var(--text-secondary);
  font-size: 12px;
}
.is-paused,
.is-paused .person-arm,
.is-paused .person-leg {
  animation-play-state: paused;
}
@keyframes actor-journey {
  0% {
    opacity: 0;
    transform: translate(var(--from-x), var(--from-y)) scale(var(--enter-scale))
      translate(-50%, -50%);
  }
  12% {
    opacity: 1;
    transform: translate(var(--from-x), var(--from-y)) scale(var(--actor-scale))
      translate(-50%, -50%);
  }
  48% {
    opacity: 1;
    transform: translate(var(--arc-x), var(--arc-y)) scale(var(--peak-scale)) translate(-50%, -50%);
  }
  88% {
    opacity: 1;
    transform: translate(var(--to-x), var(--to-y)) scale(var(--actor-scale)) translate(-50%, -50%);
  }
  100% {
    opacity: 0;
    transform: translate(var(--to-x), var(--to-y)) scale(var(--exit-scale)) translate(-50%, -50%);
  }
}
@keyframes person-work {
  from {
    transform: rotate(-18deg);
  }
  to {
    transform: rotate(18deg);
  }
}
@keyframes person-step {
  from {
    transform: rotate(-16deg);
  }
  to {
    transform: rotate(16deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .collaboration-actor {
    display: none;
  }
}
</style>
