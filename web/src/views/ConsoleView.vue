<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Modal, message, type TablePaginationConfig } from 'ant-design-vue'
import {
  DeploymentUnitOutlined,
  PlusOutlined,
  SearchOutlined,
  ExperimentOutlined,
  PlayCircleOutlined,
  PauseOutlined,
  SyncOutlined,
  DownloadOutlined,
  StopOutlined,
  ApiOutlined,
  LogoutOutlined,
  ArrowRightOutlined,
  CheckCircleOutlined,
  NodeIndexOutlined,
  TeamOutlined,
  DatabaseOutlined,
  BranchesOutlined,
  LinkOutlined,
  InfoCircleOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons-vue'
import TaskGraph from '@/components/TaskGraph.vue'
import PiExecutionPanel from '@/components/PiExecutionPanel.vue'
import AssetReportPanel from '@/components/AssetReportPanel.vue'
import StageSummaryPanel from '@/components/StageSummaryPanel.vue'
import CreateRunDrawer from '@/components/CreateRunDrawer.vue'
import NodeDetailDrawer from '@/components/NodeDetailDrawer.vue'
import { useConsoleStore } from '@/stores/console'
import {
  statusLabel,
  statusColor,
  timeLabel,
  typeNames,
  shortID,
  workerNameMap,
  eventReason,
} from '@/utils/presentation'

const store = useConsoleStore()
const route = useRoute()
const router = useRouter()
const search = ref('')
const createOpen = ref(false)
const token = ref('')
const connecting = ref(false)
const authError = ref('')
const activeTab = ref('facts')
const taskFilter = ref('all')
const filters = [
  { label: '全部任务', value: 'all' },
  { label: '执行中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '依赖阻塞', value: 'blocked' },
  { label: '失败', value: 'failed' },
]
const filteredRuns = computed(() =>
  store.runs.filter(
    (run) =>
      run.name.toLowerCase().includes(search.value.toLowerCase()) || run.id.includes(search.value),
  ),
)
const filteredTasks = computed(() =>
  store.tasks.filter((task) => taskFilter.value === 'all' || task.status === taskFilter.value),
)
const activeTasks = computed(
  () => store.tasks.filter((task) => ['running', 'claimed'].includes(task.status)).length,
)
const completedTasks = computed(
  () => store.tasks.filter((task) => task.status === 'completed').length,
)
const dynamicTasks = computed(() => store.tasks.filter((task) => task.parent_id).length)
const approvedProposals = computed(
  () => store.proposals.filter((proposal) => proposal.status === 'approved').length,
)
const progress = computed(() =>
  store.tasks.length ? Math.round((completedTasks.value / store.tasks.length) * 100) : 0,
)
const task = computed(() => store.tasks.find((item) => item.id === store.selectedTask) || null)
const workersMap = computed(() => workerNameMap(store.workers))
const isAuthorizedRun = computed(() =>
  Boolean(
    store.currentRun?.scope.tools.some((tool) => ['http.observe', 'tcp.discover'].includes(tool)),
  ),
)
const isPlanning = computed(() => store.currentRun?.status === 'planning')
const latestEvent = computed(() => store.events.at(-1))
const plannerEvent = computed(() =>
  [...store.events].reverse().find((event) => event.event_type.startsWith('planner.')),
)
const plannerFailure = computed(
  () =>
    store.currentRun?.config?.planning?.reason ||
    (plannerEvent.value?.event_type === 'planner.failed'
      ? eventReason(plannerEvent.value) || '服务端报告规划失败，未返回具体原因。'
      : ''),
)
const operationBusy = computed(() => Boolean(store.operationRunID))
const taskColumns = [
  { title: '任务节点', dataIndex: 'name', key: 'name' },
  { title: '能力', dataIndex: 'task_type', key: 'task_type' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '执行 Worker', dataIndex: 'assigned_worker_id', key: 'worker' },
  { title: '来源', key: 'source' },
  { title: '重试', dataIndex: 'retry_count', key: 'retry_count' },
]
const taskPagination = ref({ current: 1, pageSize: 8 })
// changePage 保留任务分页，不因数据刷新跳回首页。
function changePage(pagination: TablePaginationConfig) {
  taskPagination.value.current = pagination.current || 1
}
// selectRun 通过稳定 URL 查询保留运行选择，可使用浏览器前进和后退。
function selectRun(id: string) {
  void router.push({ query: { ...route.query, run: id } })
}
// changeTab 保持数据页签与导航记录一致，图组件不会因此卸载。
function changeTab(key: string) {
  void router.replace({ query: { ...route.query, tab: key } })
}
// connect 在 dev 代理自动连接失败时保留令牌输入，不将秘密写入本地存储。
async function connect() {
  connecting.value = true
  authError.value = ''
  try {
    await store.login(token.value)
    token.value = ''
    await activateInitial()
  } catch (cause) {
    authError.value = cause instanceof Error ? cause.message : '无法连接引擎'
  } finally {
    connecting.value = false
  }
}
// activateInitial 优先恢复深链接，否则选择最近的真实运行。
async function activateInitial() {
  const id = typeof route.query.run === 'string' ? route.query.run : ''
  if (id && /^[0-9a-f-]{36}$/i.test(id)) await store.selectRun(id)
  else if (store.runs[0]) await router.replace({ query: { ...route.query, run: store.runs[0].id } })
}
// perform 处理局部操作错误，不用全屏遮罩阻止观察其他节点。
async function perform(action: () => Promise<unknown>) {
  try {
    await action()
  } catch (cause) {
    message.error(cause instanceof Error ? cause.message : '操作失败，请重试')
  }
}
// cancelRun 锁定确认时的运行，规划中也可取消且不会误取消后来切换的运行。
function cancelRun() {
  const id = store.runID
  const name = store.currentRun?.name || '本次运行'
  Modal.confirm({
    title: `取消「${name}」？`,
    content: '规划与所有未结束任务将停止，当前租约失效。已保存的事实和执行轨迹会保留。',
    okText: '确认取消',
    cancelText: '继续运行',
    okButtonProps: { danger: true },
    onOk: () => perform(() => store.action('cancel', id)),
  })
}
// created 将新运行加入导航，不修改其他运行的图或筛选状态。
function created(id: string) {
  selectRun(id)
}
// disconnect 只退出当前浏览器连接，不取消后端运行。
function disconnect() {
  store.dispose()
  authError.value = ''
  token.value = ''
}
watch(
  () => route.query.run,
  async (value) => {
    if (store.connected && typeof value === 'string' && value !== store.runID) {
      if (!/^[0-9a-f-]{36}$/i.test(value)) {
        message.error('运行标识无效')
        return
      }
      await store.selectRun(value)
    }
  },
)
watch(
  () => route.query.tab,
  (value) => {
    activeTab.value =
      typeof value === 'string' &&
      ['facts', 'tasks', 'proposals', 'results', 'pi', 'assets'].includes(value)
        ? value
        : 'facts'
  },
  { immediate: true },
)
watch(taskFilter, () => {
  taskPagination.value.current = 1
})
onMounted(() => void connect())
onUnmounted(() => store.dispose())
</script>

<template>
  <div v-if="!store.connected" class="connection-page">
    <div class="connection-orbit" aria-hidden="true"><DeploymentUnitOutlined /></div>
    <div class="connection-card">
      <div class="brand-mark"><DeploymentUnitOutlined /></div>
      <span class="eyebrow">AUTONOMOUS OPERATIONS CONSOLE</span>
      <h1>看见协作发生的每一步。</h1>
      <p>连接引擎，沿着 Worker、任务与共享事实，观察真实运行中的动态协作网络。</p>
      <a-alert v-if="authError" type="warning" show-icon :message="authError" class="mb-16" />
      <a-form layout="vertical" @finish="connect">
        <a-form-item label="控制端令牌">
          <a-input-password
            v-model:value="token"
            placeholder="输入 .env 中的 ENGINE_API_TOKEN"
            autocomplete="off"
          />
        </a-form-item>
        <a-button type="primary" block size="large" html-type="submit" :loading="connecting">
          <ApiOutlined />连接协作引擎
        </a-button>
      </a-form>
      <div class="connection-note">
        <InfoCircleOutlined />dev
        模式通过本地安全代理自动连接；静态部署需要输入令牌。令牌仅保留在内存中。
      </div>
    </div>
  </div>
  <div v-else class="console-layout">
    <aside class="sidebar">
      <div class="sidebar-brand">
        <div class="brand-mark"><DeploymentUnitOutlined /></div>
        <div><strong>协同引擎</strong><span>AUTONOMOUS OPERATIONS</span></div>
        <small>V1</small>
      </div>
      <div class="workspace-label">工作空间 <span>LOCAL / DEV</span></div>
      <div class="sidebar-active">
        <NodeIndexOutlined />实时协作<span class="side-count">{{ store.runs.length }}</span>
      </div>
      <div class="sidebar-divider" />
      <div class="runs-title">
        <span>运行记录</span>
        <button aria-label="刷新运行列表" @click="perform(store.refreshRuns)">
          <SyncOutlined />
        </button>
      </div>
      <a-input
        v-model:value="search"
        placeholder="搜索名称或运行 ID"
        :bordered="false"
        class="run-search"
      >
        <template #prefix><SearchOutlined /></template>
      </a-input>
      <nav class="run-navigation" aria-label="运行记录">
        <TransitionGroup name="list">
          <button
            v-for="run in filteredRuns"
            :key="run.id"
            class="run-navigation-item"
            :class="{ selected: run.id === store.runID }"
            @click="selectRun(run.id)"
          >
            <span class="nav-status-dot" :class="run.status" />
            <div>
              <strong>{{ run.name }}</strong>
              <small
                ><span class="mono">{{ shortID(run.id) }}</span
                ><span>{{ statusLabel(run.status) }}</span></small
              >
            </div>
          </button>
        </TransitionGroup>
        <div v-if="!filteredRuns.length" class="sidebar-empty">暂无匹配的运行</div>
      </nav>
      <div class="sidebar-bottom">
        <span
          ><i class="live-dot" :class="{ paused: !store.health }" />{{
            store.health ? '引擎已连接' : '连接重试中'
          }}</span
        >
        <button aria-label="退出控制台连接" title="退出连接，不停止后端运行" @click="disconnect">
          <LogoutOutlined />
        </button>
      </div>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <div class="topbar-context">
          <strong>实时协作</strong><span>/</span
          ><span>{{ store.currentRun?.name || '运行观察' }}</span>
        </div>
        <div class="topbar-actions">
          <span class="environment-badge"
            ><SafetyCertificateOutlined />{{
              isAuthorizedRun ? '明确授权范围' : '历史受控运行'
            }}</span
          >
          <a-button type="primary" @click="createOpen = true"><PlusOutlined />新建任务</a-button>
        </div>
      </header>
      <main class="workspace">
        <a-alert
          v-if="store.error"
          type="error"
          show-icon
          :message="store.error"
          description="已有画布和数据保持不变；系统将自动重试，恢复后补齐事件。"
          class="mb-16"
        >
          <template #action
            ><a-button size="small" @click="perform(store.refresh)">重试</a-button></template
          >
        </a-alert>
        <a-alert
          v-if="!store.runs.length && !store.loading"
          type="info"
          show-icon
          message="尚未新建任务，点击右上角「新建任务」填写任务描述和精确授权范围。"
          class="mb-16"
        />
        <section class="run-summary" aria-label="当前运行">
          <div class="run-heading">
            <div class="run-icon"><ExperimentOutlined /></div>
            <div class="run-title">
              <div class="run-kicker">
                {{ store.currentRun ? 'RUN / ' + shortID(store.currentRun.id) : 'RUN CONTEXT' }}
              </div>
              <h1>
                {{
                  store.currentRun?.name || (store.loading ? '正在载入运行…' : '准备开始新的协作')
                }}
                <a-tag v-if="store.currentRun" :color="statusColor(store.currentRun.status)">
                  {{ statusLabel(store.currentRun.status) }}
                </a-tag>
              </h1>
              <p v-if="store.currentRun" class="scope-target">
                <LinkOutlined />{{ store.currentRun.scope.targets.join(' · ') }}
              </p>
              <p v-else>新建任务后，这里将显示授权目标与实时执行上下文。</p>
            </div>
          </div>
          <div class="run-actions">
            <a-button
              v-if="store.currentRun?.status === 'created'"
              type="primary"
              :loading="store.operation === 'start' && store.operationRunID === store.runID"
              :disabled="operationBusy"
              @click="perform(() => store.action('start'))"
              ><PlayCircleOutlined />启动运行</a-button
            >
            <a-button
              v-if="isPlanning || store.currentRun?.status === 'running'"
              danger
              :disabled="operationBusy"
              :loading="store.operation === 'cancel' && store.operationRunID === store.runID"
              @click="cancelRun"
              ><StopOutlined />取消</a-button
            >
            <a-tooltip
              :title="store.paused ? '恢复后将补齐所有变化' : '仅暂停界面同步，不停止 Worker'"
            >
              <a-button :disabled="!store.currentRun" @click="store.togglePause">
                <PlayCircleOutlined v-if="store.paused" /><PauseOutlined v-else />
                {{ store.paused ? '恢复同步' : '暂停视图' }}
              </a-button>
            </a-tooltip>
            <a-button :disabled="!store.currentRun" @click="perform(store.exportResult)">
              <DownloadOutlined />导出结果
            </a-button>
          </div>
        </section>
        <Transition name="panel" mode="out-in">
          <a-alert
            v-if="isPlanning"
            key="planning"
            type="info"
            show-icon
            class="mb-16"
            message="Planner 处理中 · 正在生成并校验真实计划"
            description="任务尚未生成时不会播放虚构协作。规划异步运行，可取消；任务、阶段及错误将随服务端快照更新。"
          />
          <a-alert
            v-else-if="plannerFailure"
            key="planner-failed"
            type="error"
            show-icon
            class="mb-16"
            message="Planner 规划失败"
            :description="plannerFailure"
          />
        </Transition>
        <Transition name="panel">
          <StageSummaryPanel
            v-if="store.stages.length"
            :stages="store.stages"
            :workers="store.workers"
            compact
            @select="store.selectedTask = $event"
          />
        </Transition>
        <section class="metrics-strip" aria-label="运行关键指标">
          <div class="metric-item">
            <div class="metric-label">任务完成度 <CheckCircleOutlined /></div>
            <div class="metric-value">
              {{ completedTasks }}<small>/ {{ store.tasks.length }}</small>
              <a-progress
                :percent="progress"
                :show-info="false"
                :stroke-width="4"
                size="small"
                class="metric-progress"
              />
            </div>
            <span class="metric-foot">{{
              isPlanning
                ? 'Planner 处理中 · 等待任务生成'
                : progress + '% 任务完成 · 阶段验收独立判断'
            }}</span>
          </div>
          <div class="metric-item">
            <div class="metric-label">实时执行 <TeamOutlined /></div>
            <div class="metric-value">
              {{ activeTasks }}<small>个活跃任务</small
              ><span class="metric-chip">{{ store.workers.length }} WORKERS</span>
            </div>
            <span class="metric-foot"
              >并发上限 {{ store.currentRun?.max_workers || '—' }} · 按能力动态认领</span
            >
          </div>
          <div class="metric-item">
            <div class="metric-label">共享事实 <DatabaseOutlined /></div>
            <div class="metric-value">{{ store.facts.length }}<small>条观察</small></div>
            <span class="metric-foot">Blackboard 共享事实，不共享对话上下文</span>
          </div>
          <div class="metric-item">
            <div class="metric-label">动态规划 <BranchesOutlined /></div>
            <div class="metric-value">{{ dynamicTasks }}<small>个增量节点</small></div>
            <span class="metric-foot"
              >{{ approvedProposals }} 条建议获批 · 共 {{ store.proposals.length }} 条建议</span
            >
          </div>
        </section>
        <div class="sync-bar">
          <div>
            <span class="live-dot" :class="{ paused: store.paused || !store.health }" />
            <strong>{{
              store.paused ? '视图已暂停' : store.error ? '同步重试中' : '实时同步中'
            }}</strong>
            <span>HTTP 增量事件 · 1s</span><span class="sync-divider" />
            <span>最后更新 {{ timeLabel(store.updated) }}</span>
          </div>
          <span
            ><SafetyCertificateOutlined />{{
              isAuthorizedRun
                ? '授权范围内真实执行 · 不代表漏洞确认'
                : '历史受控运行 · 按原任务契约查看'
            }}</span
          >
        </div>
        <div class="observation-grid">
          <TaskGraph
            :key="store.runID"
            :tasks="store.tasks"
            :workers="store.workers"
            :events="store.events"
            :stages="store.stages"
            :facts="store.facts"
            :run-status="store.currentRun?.status || ''"
            :planner-failure="plannerFailure"
            :selected="store.selectedTask"
            :paused="store.paused"
            @select="store.selectedTask = $event"
          />
        </div>
        <section class="data-panel">
          <a-tabs :active-key="activeTab" @change="changeTab">
            <a-tab-pane key="facts" :tab="'共享知识 · ' + store.facts.length">
              <a-empty v-if="!store.facts.length" description="等待 Worker 提交共享事实" />
              <div v-else class="facts-grid">
                <article v-for="fact in store.facts" :key="fact.id" class="fact-card">
                  <div>
                    <span class="fact-type"><DatabaseOutlined />{{ fact.fact_type }}</span>
                    <a-tag :color="statusColor(fact.status)">{{ statusLabel(fact.status) }}</a-tag>
                  </div>
                  <p>{{ fact.content.observation }}</p>
                  <footer>
                    <span>可信度 {{ Math.round(fact.confidence * 100) }}%</span>
                    <button
                      v-if="fact.source_task_id"
                      @click="store.selectedTask = fact.source_task_id"
                    >
                      来源任务 <ArrowRightOutlined />
                    </button>
                  </footer>
                </article>
              </div>
            </a-tab-pane>
            <a-tab-pane key="tasks" :tab="'任务列表 · ' + store.tasks.length">
              <div class="table-toolbar">
                <span class="muted">点击节点名称查看执行详情</span>
                <a-select
                  v-model:value="taskFilter"
                  :options="filters"
                  class="task-filter"
                  aria-label="任务状态筛选"
                />
              </div>
              <a-table
                :columns="taskColumns"
                :data-source="filteredTasks"
                row-key="id"
                :pagination="taskPagination"
                :scroll="{ x: 800 }"
                size="middle"
                @change="changePage"
              >
                <template #bodyCell="{ column, record }">
                  <a-button
                    v-if="column.key === 'name'"
                    type="link"
                    class="task-name-link"
                    @click="store.selectedTask = record.id"
                    >{{ record.name }}</a-button
                  >
                  <a-tag v-else-if="column.key === 'status'" :color="statusColor(record.status)">
                    {{ statusLabel(record.status) }}
                  </a-tag>
                  <span v-else-if="column.key === 'task_type'">{{
                    typeNames[record.task_type] || record.task_type
                  }}</span>
                  <span v-else-if="column.key === 'worker'">{{
                    workersMap.get(record.assigned_worker_id) || '—'
                  }}</span>
                  <span v-else-if="column.key === 'source'">{{
                    record.parent_id ? '动态生成' : '初始计划'
                  }}</span>
                </template>
              </a-table>
            </a-tab-pane>
            <a-tab-pane key="proposals" :tab="'协作建议 · ' + store.proposals.length">
              <a-empty v-if="!store.proposals.length" description="Worker 尚未提出协作建议" />
              <article v-for="proposal in store.proposals" :key="proposal.id" class="proposal-card">
                <div>
                  <BranchesOutlined /><strong>{{ proposal.proposed_task.name }}</strong>
                  <a-tag :color="statusColor(proposal.status)">{{
                    statusLabel(proposal.status)
                  }}</a-tag>
                </div>
                <p>{{ proposal.reason }}</p>
                <p class="muted">Planner 审核：{{ proposal.review_reason || '等待规则审核' }}</p>
                <small>
                  来源 {{ workersMap.get(proposal.worker_id) || shortID(proposal.worker_id) }} ·
                  目标
                  {{ proposal.proposed_task.target }}
                </small>
              </article>
              <p class="muted">
                界面最多展示 200 条建议；任务创建仍需经过 Planner 审核和预算检查。
              </p>
            </a-tab-pane>
            <a-tab-pane key="assets" tab="资产报告"
              ><AssetReportPanel
                :key="store.runID"
                :facts="store.facts"
                :evidence="store.evidence"
                :tasks="store.tasks"
                :run="store.currentRun"
            /></a-tab-pane>
            <a-tab-pane key="pi" tab="Pi 执行"
              ><PiExecutionPanel :key="store.runID" :tasks="store.tasks" :events="store.events"
            /></a-tab-pane>
            <a-tab-pane key="results" tab="目标与结果">
              <div class="result-overview">
                <h3>任务完成不等于阶段验收通过</h3>
                <p>
                  阶段按服务端快照展示必需事实、只读复核与未完成原因。单执行者自检不代表多人共识；阶段通过也不自动构成安全漏洞确认。
                </p>
              </div>
              <Transition name="panel" mode="out-in">
                <StageSummaryPanel
                  v-if="store.stages.length"
                  key="stages"
                  :stages="store.stages"
                  :workers="store.workers"
                  @select="store.selectedTask = $event"
                />
                <div v-else key="legacy-goals">
                  <p class="muted">
                    {{
                      isPlanning
                        ? 'Planner 正在生成阶段，尚无验收结果。'
                        : store.currentRun?.objective || store.currentRun?.config?.objective
                          ? '服务端尚未返回阶段摘要；不推断验收通过。'
                          : '此历史运行没有阶段摘要，以下仅展示原目标状态，不视为阶段验收。'
                    }}
                  </p>
                  <div class="goal-grid">
                    <div v-for="goal in store.result?.goals || []" :key="goal.id" class="goal-card">
                      <CheckCircleOutlined /><strong>{{ goal.name }}</strong>
                      <a-tag :color="statusColor(goal.status)">{{
                        statusLabel(goal.status)
                      }}</a-tag>
                    </div>
                  </div>
                </div>
              </Transition>
              <h4 class="finding-heading">安全发现 · {{ store.result?.findings?.length || 0 }}</h4>
              <a-empty
                v-if="!store.result?.findings?.length"
                description="本次运行尚无安全发现；只读观察与端口开放不自动构成漏洞"
              />
              <article
                v-for="finding in store.result?.findings || []"
                :key="finding.id"
                class="fact-card"
              >
                <div>
                  <strong>{{ finding.title }}</strong>
                  <a-tag :color="statusColor(finding.status)">{{
                    statusLabel(finding.status)
                  }}</a-tag>
                </div>
                <p>{{ finding.description }}</p>
              </article>
              <a-button :disabled="!store.currentRun" @click="perform(store.exportResult)">
                <DownloadOutlined />导出完整结果 JSON
              </a-button>
            </a-tab-pane>
          </a-tabs>
        </section>
        <footer class="workspace-footer">
          <span>自主协同引擎 · 模块化单体 / PostgreSQL / Redis</span>
          <span
            >事件游标 #{{ store.cursor }} ·
            {{ latestEvent ? timeLabel(latestEvent.created_at) : '尚无事件' }}</span
          >
        </footer>
      </main>
    </div>
    <CreateRunDrawer :open="createOpen" @close="createOpen = false" @created="created" />
    <NodeDetailDrawer
      :task="task"
      :facts="store.facts"
      :events="store.events"
      :workers="store.workers"
      :evidence="store.evidence"
      @close="store.selectedTask = ''"
    />
  </div>
</template>
