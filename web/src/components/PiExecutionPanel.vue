<script setup lang="ts">
import { computed } from 'vue'
import type { EngineEvent, Task } from '@/types/engine'
import { eventLabel, timeLabel } from '@/utils/presentation'

const props = defineProps<{ events: EngineEvent[]; tasks: Task[] }>()
const piTasks = computed(() => props.tasks.filter((task) => task.input.executor === 'pi'))
const taskNames = computed(() => new Map(props.tasks.map((task) => [task.id, task.name])))
const events = computed(() =>
  props.events
    .filter(
      (event) =>
        event.event_type.startsWith('agent.') ||
        (event.event_type === 'tool.called' &&
          ['http.observe', 'tcp.discover'].includes(String(event.payload.tool))),
    )
    .slice(-100)
    .reverse(),
)
const usage = computed(() => {
  const latest = new Map<string, number>()
  for (const event of props.events) {
    if (event.event_type !== 'agent.usage') continue
    const tokens = event.payload.tokens
    if (typeof tokens === 'number' && Number.isFinite(tokens))
      latest.set(event.task_id + ':' + String(event.payload.generation), tokens)
  }
  return [...latest.values()].reduce((total, tokens) => total + tokens, 0)
})
const columns = [
  { title: '时间', key: 'time', width: 100 },
  { title: '任务', key: 'task', width: 180 },
  { title: '步骤', key: 'event', width: 160 },
  { title: '详情', key: 'detail' },
]
// detail 将受限服务端审计转成可读摘要，不推测未提供的失败原因。
function detail(event: EngineEvent) {
  const p = event.payload
  if (event.event_type === 'agent.usage')
    return `累计 ${p.tokens} Token · 输入 ${p.input_tokens} / 输出 ${p.output_tokens} · 轮次 ${p.turns}`
  if (event.event_type === 'agent.discovery') return `低速探测 TCP ${p.port} · ${p.stage}`
  if (event.event_type === 'agent.turn') return `模型轮次 ${p.turn}`
  if (event.event_type === 'agent.started') return `模型：${p.model} · 租约代数 ${p.generation}`
  if (event.event_type === 'agent.tool_result')
    return `${p.tool} · ${p.success ? '成功' : '失败'}${typeof p.reason === 'string' ? ' · ' + p.reason : ''}`
  if (event.event_type === 'tool.called') return `${p.tool} · ${p.target}`
  if (event.event_type === 'agent.failed' && typeof p.reason === 'string') return p.reason
  return `工具调用 ${p.tool_calls} 次 · ${p.observed ? '已取得真实观察' : '未取得真实观察'}`
}
</script>
<template>
  <section class="pi-panel">
    <a-alert
      type="info"
      show-icon
      message="真实 Pi 执行轨迹，不展示思维链或模型密钥"
      description="这里只汇总当前已加载事件（最近 500 条）中的用量，不是完整账单；价格未知。真实响应见「共享知识」，也可点击任务查看证据与完整事件载荷。失败原因未细分时，请检查后端配置、授权范围、模型兼容性及预算。"
    />
    <p class="pi-summary">
      Pi 任务 {{ piTasks.length }} 个 · 已加载事件 Token 合计 {{ usage.toLocaleString('zh-CN') }} ·
      展示最近 100 条 Pi 轨迹
    </p>
    <a-table
      :columns="columns"
      :data-source="events"
      row-key="id"
      size="small"
      :pagination="{ pageSize: 10, showSizeChanger: false }"
      :scroll="{ x: 700 }"
    >
      <template #emptyText
        ><a-empty
          description="尚无执行事件。启动后先由 Planner 规划，再产生真实任务轨迹；后端未配置执行能力会明确拒绝，不回退演示。"
      /></template>
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'time'">{{ timeLabel(record.created_at) }}</template>
        <template v-else-if="column.key === 'task'">{{
          taskNames.get(record.task_id) || '—'
        }}</template>
        <template v-else-if="column.key === 'event'"
          ><a-tag
            :color="
              record.event_type === 'agent.failed'
                ? 'error'
                : record.event_type === 'agent.completed'
                  ? 'success'
                  : 'default'
            "
            >{{ eventLabel(record.event_type) }}</a-tag
          ></template
        >
        <template v-else-if="column.key === 'detail'"
          ><span class="pi-detail">{{ detail(record) }}</span></template
        >
      </template>
    </a-table>
  </section>
</template>
<style scoped>
.pi-summary {
  margin: 16px 0;
  color: var(--text-secondary);
  line-height: 1.8;
}
.pi-detail {
  overflow-wrap: anywhere;
}
</style>
