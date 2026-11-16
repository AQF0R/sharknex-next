import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api, setToken } from '@/services/api'
import type {
  Run,
  RunResult,
  Worker,
  Proposal,
  Evidence,
  EngineEvent,
  RunSpec,
} from '@/types/engine'

// useConsoleStore 管理真实运行快照、增量事件和可取消的实时同步生命周期。
export const useConsoleStore = defineStore('console', () => {
  const runs = ref<Run[]>([])
  const result = ref<RunResult | null>(null)
  const workers = ref<Worker[]>([])
  const proposals = ref<Proposal[]>([])
  const evidence = ref<Evidence[]>([])
  const events = ref<EngineEvent[]>([])
  const runID = ref('')
  const connected = ref(false)
  const loading = ref(false)
  const paused = ref(false)
  const error = ref('')
  const updated = ref('')
  const cursor = ref(0)
  const operation = ref('')
  const operationRunID = ref('')
  const stages = computed(() => result.value?.stages || [])
  const tasks = computed(() => result.value?.tasks || [])
  const facts = computed(() => result.value?.facts || [])
  const currentRun = computed(() => result.value?.run || null)
  const selectedTask = ref('')
  const health = ref(false)
  let controller: AbortController | undefined
  let generation = 0
  let snapshotVersion = 0
  let connectionVersion = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let listTimer: ReturnType<typeof setTimeout> | undefined

  // refreshRuns 保留导航上下文，只更新最近一百个运行的服务端数据。
  async function refreshRuns() {
    const version = connectionVersion
    const data = await api.runs()
    if (version !== connectionVersion) return
    runs.value = data
  }
  // login 优先尝试安全 dev 代理，静态部署则使用用户输入的内存令牌。
  async function login(token = '') {
    ++connectionVersion
    setToken(token)
    error.value = ''
    try {
      await refreshRuns()
      health.value = true
      connected.value = true
      startListTimer()
    } catch (cause) {
      connected.value = false
      throw cause
    }
  }
  // startListTimer 低频刷新导航，不让旧的轮询堆积。
  function startListTimer() {
    clearTimeout(listTimer)
    listTimer = setTimeout(async () => {
      try {
        await refreshRuns()
      } catch {
        health.value = false
      } finally {
        if (connected.value) startListTimer()
      }
    }, 10000)
  }
  // loadSnapshot 批量刷新真实资源，只在事件变化时调用；旧响应不能覆盖新运行。
  async function loadSnapshot(
    id: string,
    version: number,
    signal: AbortSignal,
    checkpoint?: number,
  ) {
    const requestVersion = ++snapshotVersion
    // 快照不是一个数据库事务；先取事件边界，避免后读到的事件遮蔽尚未刷新到的状态。
    const safeCursor =
      checkpoint ?? ((await api.events(id, cursor.value, signal)).at(-1)?.sequence || cursor.value)
    const [data, ws, ps, es] = await Promise.all([
      api.result(id, signal),
      api.workers(id, signal),
      api.proposals(id, signal),
      api.evidence(id, signal),
    ])
    if (version !== generation || signal.aborted || requestVersion !== snapshotVersion) return
    result.value = data
    workers.value = ws
    proposals.value = ps
    evidence.value = es
    const history = data.events || []
    events.value = history.slice(-500)
    cursor.value = safeCursor
    updated.value = new Date().toISOString()
    health.value = true
  }
  // selectRun 切换运行时取消旧请求，首次载入后持续按游标补齐。
  async function selectRun(id: string) {
    stopSync()
    const version = ++generation
    runID.value = id
    result.value = null
    events.value = []
    workers.value = []
    proposals.value = []
    evidence.value = []
    cursor.value = 0
    selectedTask.value = ''
    error.value = ''
    if (!id) return
    controller = new AbortController()
    loading.value = true
    try {
      await loadSnapshot(id, version, controller.signal)
    } catch (cause) {
      if (version === generation && !(cause instanceof DOMException && cause.name === 'AbortError'))
        error.value = cause instanceof Error ? cause.message : '无法载入运行'
    } finally {
      if (version === generation) {
        loading.value = false
        schedule(version)
      }
    }
  }
  // schedule 合并同步操作，失败保留旧图并按固定上限退避重试。
  function schedule(version: number) {
    clearTimeout(timer)
    if (paused.value || version !== generation) return
    timer = setTimeout(() => void poll(version), error.value ? 3000 : 1000)
  }
  // poll 运行与规划中持续读取快照，避免事件缺失让阶段状态或取消结果停滞。
  async function poll(version: number) {
    if (version !== generation || paused.value || !controller) return
    try {
      const batch = await api.events(runID.value, cursor.value, controller.signal)
      if (version !== generation || paused.value) return
      if (
        batch.length ||
        !result.value ||
        ['created', 'planning', 'running'].includes(currentRun.value?.status || '')
      )
        await loadSnapshot(
          runID.value,
          version,
          controller.signal,
          batch.at(-1)?.sequence ?? cursor.value,
        )
      else {
        const ws = await api.workers(runID.value, controller.signal)
        if (version === generation && !paused.value) {
          workers.value = ws
          updated.value = new Date().toISOString()
        }
      }
      if (version === generation) {
        error.value = ''
        health.value = true
      }
    } catch (cause) {
      if (
        version === generation &&
        !(cause instanceof DOMException && cause.name === 'AbortError')
      ) {
        error.value = cause instanceof Error ? cause.message : '实时同步中断'
        health.value = false
      }
    } finally {
      schedule(version)
    }
  }
  // togglePause 冻结数据视图但保留游标，恢复时补齐离线期间的状态。
  function togglePause() {
    paused.value = !paused.value
    if (paused.value) {
      clearTimeout(timer)
      controller?.abort()
      controller = new AbortController()
    } else schedule(generation)
  }
  // refresh 复用当前运行上下文，刷新不清空节点或图的位置。
  async function refresh() {
    if (!runID.value || !controller) return
    const version = generation
    await loadSnapshot(runID.value, version, controller.signal)
    error.value = ''
  }
  // create 以创建响应为提交结果，导航刷新失败不能诱发重复创建。
  async function create(spec: RunSpec) {
    const connection = connectionVersion
    operation.value = 'create'
    try {
      const run = await api.create(spec)
      if (connection === connectionVersion) {
        runs.value = [run, ...runs.value.filter((item) => item.id !== run.id)].slice(0, 100)
        try {
          await refreshRuns()
        } catch {
          health.value = false
        }
      }
      return run
    } finally {
      if (connection === connectionVersion && operation.value === 'create') operation.value = ''
    }
  }
  // action 锁定目标并隔离切换后的响应，状态刷新失败不误报已接受的操作。
  async function action(kind: 'start' | 'cancel', id = runID.value) {
    if (!id) return
    if (operationRunID.value) throw new Error('已有运行操作正在提交，请稍后重试。')
    const version = generation
    const connection = connectionVersion
    const signal = id === runID.value ? controller?.signal : undefined
    operation.value = kind
    operationRunID.value = id
    try {
      await api.action(id, kind, signal)
      if (version === generation && id === runID.value && !signal?.aborted) {
        try {
          await refresh()
        } catch {
          error.value = '操作已受理，但快照同步失败，正在重试。'
          health.value = false
        }
      }
      if (connection === connectionVersion) {
        try {
          await refreshRuns()
        } catch {
          health.value = false
        }
      }
    } finally {
      if (connection === connectionVersion && operationRunID.value === id) {
        operation.value = ''
        operationRunID.value = ''
      }
    }
  }
  // exportResult 按需获取完整结果，不从截断的事件视图拼装归档。
  async function exportResult() {
    if (!runID.value) return
    const data = await api.fullResult(runID.value)
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
    )
    const link = document.createElement('a')
    link.href = url
    link.download = runID.value + '.json'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  // stopSync 清理轮询与旧网络请求，供切换运行及退出使用。
  function stopSync() {
    clearTimeout(timer)
    controller?.abort()
    controller = undefined
  }
  // dispose 退出时释放全部定时器和令牌，禁止跨连接泄漏上下文。
  function dispose() {
    ++connectionVersion
    ++generation
    stopSync()
    clearTimeout(listTimer)
    setToken('')
    connected.value = false
    result.value = null
    runs.value = []
    workers.value = []
    events.value = []
    proposals.value = []
    evidence.value = []
    runID.value = ''
    error.value = ''
    operation.value = ''
    operationRunID.value = ''
  }
  return {
    runs,
    result,
    workers,
    proposals,
    evidence,
    events,
    runID,
    connected,
    loading,
    paused,
    error,
    updated,
    cursor,
    operation,
    operationRunID,
    stages,
    tasks,
    facts,
    currentRun,
    selectedTask,
    health,
    login,
    refreshRuns,
    selectRun,
    togglePause,
    refresh,
    create,
    action,
    exportResult,
    dispose,
  }
})
