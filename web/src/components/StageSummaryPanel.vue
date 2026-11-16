<script setup lang="ts">
import { computed } from 'vue'
import { SafetyCertificateOutlined } from '@ant-design/icons-vue'
import type { StageSummary, Worker } from '@/types/engine'
import { currentStage, stageReviewLabel, statusColor, statusLabel } from '@/utils/presentation'

const props = withDefaults(
  defineProps<{ stages: StageSummary[]; workers: Worker[]; compact?: boolean }>(),
  { compact: false },
)
const emit = defineEmits<{ select: [id: string] }>()
const focused = computed(() => currentStage(props.stages))
const visibleStages = computed(() =>
  props.compact ? (focused.value ? [focused.value] : []) : props.stages,
)
const achieved = computed(() => props.stages.filter((stage) => stage.status === 'achieved').length)
const stageNames = computed(
  () =>
    new Map(
      props.stages.flatMap((stage) => [
        [stage.id, stage.name],
        [stage.key, stage.name],
      ]),
    ),
)
// progress 仅显示阶段任务数量比例，不由进度推断验收结果。
function progress(stage: StageSummary) {
  return stage.total_tasks
    ? Math.min(100, Math.round((stage.completed_tasks / stage.total_tasks) * 100))
    : 0
}
// reasonText 保留服务端原因，未返回时明确说明而不编造失败或成功。
function reasonText(stage: StageSummary) {
  if (stage.reason) return stage.reason
  if (stage.status === 'achieved') return '服务端已验收通过'
  if (['needs_work', 'failed', 'cancelled'].includes(stage.status))
    return '服务端未返回具体原因，请查看真实事件与任务结果'
  return '尚未验收通过；具体原因以服务端后续快照为准'
}
</script>

<template>
  <section class="stage-summary" :class="{ compact }" aria-label="阶段验收摘要">
    <header class="stage-heading">
      <strong><SafetyCertificateOutlined />{{ compact ? '当前阶段' : '阶段验收报告' }}</strong
      ><span>{{ achieved }}/{{ stages.length }} 阶段通过 · 任务完成不等于验收通过</span>
    </header>
    <TransitionGroup name="stage" tag="div" class="stage-list">
      <article v-for="stage in visibleStages" :key="stage.id" class="stage-card">
        <div class="stage-title">
          <strong>{{ stage.name }}</strong
          ><a-tag :color="statusColor(stage.status)">{{ statusLabel(stage.status) }}</a-tag
          ><small>{{ stage.completed_tasks }}/{{ stage.total_tasks }} 任务完成</small>
        </div>
        <a-progress
          :percent="progress(stage)"
          :show-info="false"
          :stroke-width="3"
          :status="stage.status === 'failed' ? 'exception' : 'normal'"
        />
        <dl>
          <div>
            <dt>验收条件</dt>
            <dd>
              {{
                stage.required_facts.length
                  ? stage.required_facts.join(' · ')
                  : '服务端未声明必需事实'
              }}
            </dd>
          </div>
          <div>
            <dt>只读复核</dt>
            <dd>
              {{ stageReviewLabel(stage, workers)
              }}<button
                v-if="stage.review_task_id"
                type="button"
                @click="emit('select', stage.review_task_id)"
              >
                查看复核任务
              </button>
            </dd>
          </div>
          <div>
            <dt>{{ stage.status === 'achieved' ? '验收结论' : '未完成说明' }}</dt>
            <dd :class="{ warning: ['needs_work', 'failed'].includes(stage.status) }">
              {{ reasonText(stage) }}
            </dd>
          </div>
          <div v-if="!compact">
            <dt>精确目标</dt>
            <dd>{{ stage.targets.join(' · ') || '未返回目标' }}</dd>
          </div>
          <div v-if="!compact && stage.depends_on.length">
            <dt>依赖阶段</dt>
            <dd>{{ stage.depends_on.map((key) => stageNames.get(key) || key).join(' · ') }}</dd>
          </div>
        </dl>
      </article>
    </TransitionGroup>
  </section>
</template>

<style scoped>
.stage-summary {
  margin-bottom: 16px;
  padding: 14px 18px;
  background: var(--surface-panel);
  border: 1px solid var(--line-subtle);
  border-radius: 10px;
}
.stage-heading,
.stage-title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.stage-heading {
  justify-content: space-between;
  margin-bottom: 10px;
}
.stage-heading strong {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-primary);
  font-size: 13px;
}
.stage-heading .anticon {
  color: var(--accent);
}
.stage-heading > span,
.stage-title small {
  color: var(--text-tertiary);
  font-size: 12px;
}
.stage-list {
  position: relative;
  display: grid;
  gap: 12px;
}
.stage-card {
  min-width: 0;
  padding: 12px;
  background: var(--surface-inset);
  border: 1px solid var(--line-subtle);
  border-radius: 8px;
}
.compact .stage-card {
  padding: 0;
  background: none;
  border: 0;
}
.stage-title strong {
  color: var(--text-primary);
  font-size: 13px;
}
.stage-title .ant-tag {
  margin: 0;
}
.stage-title small {
  margin-left: auto;
}
.stage-card dl {
  display: grid;
  gap: 6px;
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 1.7;
}
.stage-card dl > div {
  display: grid;
  grid-template-columns: 66px minmax(0, 1fr);
  gap: 10px;
}
.stage-card dt {
  color: var(--text-tertiary);
}
.stage-card dd {
  margin: 0;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.stage-card dd.warning {
  color: var(--warning);
}
.stage-card button {
  padding: 0;
  margin-left: 10px;
  color: var(--accent);
  background: none;
  border: 0;
  cursor: pointer;
}
.stage-card button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
.stage-enter-active,
.stage-leave-active,
.stage-move {
  transition:
    opacity 160ms ease,
    transform 200ms ease;
}
.stage-enter-from,
.stage-leave-to {
  opacity: 0;
  transform: translateY(5px);
}
.stage-leave-active {
  position: absolute;
  width: 100%;
}
@media (prefers-reduced-motion: reduce) {
  .stage-enter-active,
  .stage-leave-active,
  .stage-move {
    transition: none;
  }
}
@media (max-width: 640px) {
  .stage-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 6px;
  }
  .stage-title small {
    margin-left: 0;
  }
}
</style>
