<script setup lang="ts">
import { computed } from 'vue'
import type { Task, Fact, EngineEvent, Worker, Evidence } from '@/types/engine'
import {
  statusLabel,
  statusColor,
  typeNames,
  timeLabel,
  eventLabel,
  workerNameMap,
} from '@/utils/presentation'
const props = defineProps<{
  task: Task | null
  facts: Fact[]
  events: EngineEvent[]
  workers: Worker[]
  evidence: Evidence[]
}>()
defineEmits<{ close: [] }>()
const worker = computed(() => props.workers.find((w) => w.id === props.task?.assigned_worker_id))
const relatedFacts = computed(() => props.facts.filter((f) => f.source_task_id === props.task?.id))
const sharedFacts = computed(() =>
  props.facts.filter(
    (f) => f.content.target === props.task?.input.target && f.source_task_id !== props.task?.id,
  ),
)
const relatedEvents = computed(() =>
  props.events.filter((e) => e.task_id === props.task?.id).reverse(),
)
const relatedEvidence = computed(() => props.evidence.filter((e) => e.task_id === props.task?.id))
</script>
<template>
  <a-drawer :open="!!task" title="任务节点详情" :width="560" @close="$emit('close')">
    <template v-if="task">
      <div class="detail-top">
        <span class="eyebrow">{{ typeNames[task.task_type] || task.task_type }}</span
        ><a-tag :color="statusColor(task.status)">{{ statusLabel(task.status) }}</a-tag>
      </div>
      <h2>{{ task.name }}</h2>
      <p class="mono muted break-text">{{ task.id }}</p>
      <a-descriptions :column="1" size="small" bordered>
        <a-descriptions-item label="执行 Worker">{{
          worker ? workerNameMap(workers).get(worker.id) : '尚未分配'
        }}</a-descriptions-item>
        <a-descriptions-item label="执行器">{{
          task.task_type === 'stage_review' ||
          ['review', 'internal_review'].includes(task.input.executor || '')
            ? '阶段只读复核 · 不重新扫描'
            : task.input.executor === 'pi'
              ? 'Pi SDK · 真实只读观察'
              : 'Local · 历史受控执行'
        }}</a-descriptions-item>
        <a-descriptions-item label="授权目标">{{ task.input.target }}</a-descriptions-item>
        <a-descriptions-item label="所需能力">{{
          task.input.capabilities.join(' / ')
        }}</a-descriptions-item>
        <a-descriptions-item label="允许工具">{{
          task.input.tools.join(' / ')
        }}</a-descriptions-item>
        <a-descriptions-item label="优先级 / 重试"
          >{{ task.priority }} / {{ task.retry_count }} 次（最多
          {{ task.max_retries }}）</a-descriptions-item
        >
        <a-descriptions-item label="开始 / 结束"
          >{{ timeLabel(task.started_at) }} / {{ timeLabel(task.finished_at) }}</a-descriptions-item
        >
        <a-descriptions-item label="任务来源">{{
          task.parent_id ? '动态规划 · 父任务 ' + task.parent_id : 'Planner 初始计划'
        }}</a-descriptions-item>
      </a-descriptions>
      <a-tabs>
        <a-tab-pane key="facts" :tab="'产生的事实 · ' + relatedFacts.length">
          <a-empty v-if="!relatedFacts.length" description="此任务尚未提交事实" />
          <article v-for="fact in relatedFacts" :key="fact.id" class="detail-card">
            <div>
              <strong>{{ fact.fact_type }}</strong
              ><a-tag :color="statusColor(fact.status)">{{ statusLabel(fact.status) }}</a-tag>
            </div>
            <p>{{ fact.content.observation }}</p>
            <small>可信度 {{ Math.round(fact.confidence * 100) }}%</small>
          </article>
        </a-tab-pane>
        <a-tab-pane key="shared" :tab="'相关共享事实 · ' + sharedFacts.length">
          <a-alert
            type="info"
            message="相同授权目标的共享事实，不等同于历史执行时的完整 Context Snapshot。"
            class="mb-16"
          />
          <a-empty v-if="!sharedFacts.length" description="暂无相关共享事实" />
          <article v-for="fact in sharedFacts" :key="fact.id" class="detail-card">
            <strong>{{ fact.fact_type }}</strong>
            <p>{{ fact.content.observation }}</p>
          </article>
        </a-tab-pane>
        <a-tab-pane key="events" :tab="'执行轨迹 · ' + relatedEvents.length">
          <a-timeline
            ><a-timeline-item v-for="event in relatedEvents" :key="event.id"
              ><strong>{{ eventLabel(event.event_type) }}</strong>
              <p class="muted">{{ timeLabel(event.created_at) }} · #{{ event.sequence }}</p>
              <pre>{{ JSON.stringify(event.payload, null, 2) }}</pre>
            </a-timeline-item></a-timeline
          >
        </a-tab-pane>
        <a-tab-pane key="evidence" :tab="'证据索引 · ' + relatedEvidence.length"
          ><a-empty v-if="!relatedEvidence.length" description="暂无证据索引" />
          <article v-for="item in relatedEvidence" :key="item.id" class="detail-card">
            <strong>{{ item.evidence_type }}</strong>
            <p class="mono break-text">{{ item.content_ref }}</p>
            <small class="mono break-text"
              >内容大小 {{ item.metadata?.size ?? '—' }} 字节 · 摘要由服务端保管</small
            >
          </article>
          <p class="muted">证据内容保留在私有对象存储，不提供任意文件下载。</p></a-tab-pane
        >
      </a-tabs>
    </template>
  </a-drawer>
</template>
<style scoped>
.detail-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
h2 {
  margin: 12px 0 8px;
  color: var(--text-primary);
  font-size: 23px;
  font-weight: 620;
  overflow-wrap: anywhere;
}
.detail-card {
  margin-bottom: 12px;
  padding: 16px;
  background: var(--surface-inset);
  border: 1px solid var(--line-subtle);
  border-radius: 10px;
}
.detail-card > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.detail-card strong {
  color: var(--text-primary);
  font-size: 14px;
  overflow-wrap: anywhere;
}
.detail-card p {
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.8;
  overflow-wrap: anywhere;
}
.detail-card small {
  color: var(--text-tertiary);
  font-size: 12px;
}
pre {
  padding: 12px;
  overflow-wrap: anywhere;
  color: var(--text-secondary);
  background: var(--surface-inset);
  border: 1px solid var(--line-subtle);
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
}
</style>
