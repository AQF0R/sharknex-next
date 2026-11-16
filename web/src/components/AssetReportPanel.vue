<script setup lang="ts">
import { computed } from 'vue'
import { z } from 'zod'
import type { Fact, Evidence, Run, Task } from '@/types/engine'
const props = defineProps<{ facts: Fact[]; evidence: Evidence[]; run: Run | null; tasks: Task[] }>()
const portSchema = z.object({
  port: z.number(),
  state: z.enum(['open', 'closed', 'timeout', 'error']),
  service: z.string(),
  hint: z.string(),
  banner: z.string().optional(),
  http_status: z.number().optional(),
  server: z.string().optional(),
  powered_by: z.string().optional(),
  title: z.string().optional(),
  fingerprint: z.string().optional(),
  basis: z.string().optional(),
  tls: z.string().optional(),
  note: z.string().optional(),
})
const reportSchema = z.object({
  version: z.literal('asset-discovery/v1'),
  ip: z.string(),
  requested_ports: z.array(z.number()),
  ports: z.array(portSchema),
  complete: z.boolean(),
  started_at: z.string(),
  finished_at: z.string(),
  limitations: z.string(),
})
const reports = computed(() =>
  props.facts.flatMap((fact) => {
    if (!['services_discovered', 'discovery_partial'].includes(fact.fact_type)) return []
    try {
      const parsed = reportSchema.safeParse(JSON.parse(String(fact.content.observation)))
      return parsed.success
        ? [
            {
              fact,
              ...parsed.data,
              evidenceCount: props.evidence.filter((x) => x.fact_id === fact.id).length,
            },
          ]
        : []
    } catch {
      return []
    }
  }),
)
const isDiscovery = computed(
  () =>
    Boolean(props.run?.scope.tools.includes('tcp.discover')) ||
    props.tasks.some((x) => x.input.tools.includes('tcp.discover')),
)
const columns = [
  { title: 'TCP端口', dataIndex: 'port', key: 'port', width: 100 },
  { title: '实测状态', key: 'state', width: 110 },
  { title: '实测协议 / 端口提示', key: 'service', width: 160 },
  { title: '指纹与证据依据', key: 'fingerprint' },
  { title: 'HTTP / TLS / 限制', key: 'detail' },
]
const stateNames = { open: '开放', closed: '明确拒绝', timeout: '超时·未知', error: '错误·未知' }
// count 仅统计实测状态，不从模型摘要推导结果。
function count(ports: z.infer<typeof portSchema>[], state: string) {
  return ports.filter((x) => x.state === state).length
}
</script>
<template>
  <section class="asset-panel">
    <a-alert
      type="info"
      show-icon
      message="资产发现结果，不等于漏洞报告"
      description="仅检查声明的 TCP 端口；超时不能判定关闭。协议、软件自报指纹与端口猜测分开显示；未执行认证尝试或漏洞利用。"
    />
    <div v-if="run?.scope.authorization" class="authorization-record">
      <strong>本 Run 已记录授权声明</strong>
      <p>{{ run.scope.authorization.statement }}</p>
      <small
        >目标：{{ run.scope.targets.join('、') }} · 端口：{{
          run.scope.tcp_ports?.join(', ') || '未授权端口扫描'
        }}
        · 截止：{{
          run.deadline_at ? new Date(run.deadline_at).toLocaleString('zh-CN') : '以运行截止时间为准'
        }}</small
      >
    </div>
    <Transition name="report" mode="out-in">
      <a-empty
        v-if="!reports.length"
        key="empty"
        :description="
          isDiscovery
            ? run?.status === 'planning'
              ? 'Planner 正在规划授权端口任务，尚无探测报告。'
              : '尚无可展示的探测报告；请查看阶段、任务状态和真实执行轨迹，失败不代表目标安全。'
            : '本次范围未授权 TCP 探测。新建任务时可声明单 IPv4 与明确端口；HTTP URL 仅做只读观察。'
        "
      />
      <div v-else key="reports">
        <article v-for="report in reports" :key="report.fact.id" class="report-card">
          <header>
            <h3>{{ report.ip }}</h3>
            <a-tag :color="report.complete ? 'success' : 'warning'">{{
              report.complete ? '声明端口探测完成' : '覆盖不完整'
            }}</a-tag>
          </header>
          <p class="summary">
            覆盖 {{ report.ports.length }} / {{ report.requested_ports.length }} 个端口 · 开放
            {{ count(report.ports, 'open') }} · 明确拒绝 {{ count(report.ports, 'closed') }} · 超时
            {{ count(report.ports, 'timeout') }} · 错误 {{ count(report.ports, 'error') }} ·
            关联证据 {{ report.evidenceCount }}
          </p>
          <p class="muted">
            采集：{{ new Date(report.started_at).toLocaleString('zh-CN') }} —
            {{ new Date(report.finished_at).toLocaleTimeString('zh-CN') }}
          </p>
          <a-table
            :columns="columns"
            :data-source="report.ports"
            row-key="port"
            :pagination="false"
            size="small"
            :scroll="{ x: 900 }"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'state'"
                ><a-tag
                  :color="
                    record.state === 'open'
                      ? 'success'
                      : record.state === 'closed'
                        ? 'default'
                        : 'warning'
                  "
                  >{{ stateNames[record.state as keyof typeof stateNames] }}</a-tag
                ></template
              >
              <template v-else-if="column.key === 'service'"
                ><strong>{{ record.service || '未确定' }}</strong>
                <div class="muted">端口提示：{{ record.hint || '无' }}（猜测）</div></template
              >
              <template v-else-if="column.key === 'fingerprint'">
                <strong>{{
                  record.fingerprint || (record.banner ? '服务自报 Banner' : '无可靠产品指纹')
                }}</strong>
                <div v-if="record.banner">Banner：{{ record.banner }}</div>
                <div v-if="record.title">标题：{{ record.title }}</div>
                <div class="muted">
                  {{ record.basis || '仅记录连接状态，未确认具体产品或版本' }}
                </div>
              </template>
              <template v-else-if="column.key === 'detail'">
                <div v-if="record.http_status">HTTP {{ record.http_status }}</div>
                <div v-if="record.tls">{{ record.tls }}</div>
                <div>{{ record.note || '—' }}</div>
              </template>
            </template>
          </a-table>
          <p class="muted">{{ report.limitations }}</p>
          <small
            >来源任务 {{ report.fact.source_task_id }} · 事实
            {{ report.fact.id }}。原始结构化证据可从任务详情查看，也可在「目标与结果」导出完整
            JSON。</small
          >
        </article>
      </div>
    </Transition>
  </section>
</template>
<style scoped>
.asset-panel {
  min-height: 240px;
}
.authorization-record {
  margin: 16px 0;
  padding: 14px 16px;
  background: var(--bg-subtle, #f6f8fa);
  border-radius: 8px;
  overflow-wrap: anywhere;
}
.authorization-record p {
  margin: 6px 0;
}
.report-card {
  padding: 16px 0;
  overflow-wrap: anywhere;
}
.report-card header {
  display: flex;
  align-items: center;
  gap: 12px;
}
.report-card h3 {
  margin: 0;
}
.summary {
  margin: 12px 0 6px;
}
.muted,
small {
  color: var(--text-secondary, #64748b);
}
.report-enter-active,
.report-leave-active {
  transition:
    opacity 160ms ease,
    transform 160ms ease;
}
.report-enter-from,
.report-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
@media (prefers-reduced-motion: reduce) {
  .report-enter-active,
  .report-leave-active {
    transition: none;
  }
  .report-enter-from,
  .report-leave-to {
    transform: none;
  }
}
</style>
