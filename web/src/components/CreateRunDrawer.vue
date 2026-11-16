<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { Modal, message, type FormInstance } from 'ant-design-vue'
import { DownOutlined, InfoCircleOutlined } from '@ant-design/icons-vue'
import { useConsoleStore } from '@/stores/console'
import type { RunSpec } from '@/types/engine'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; created: [id: string] }>()
const store = useConsoleStore()
const defaults = {
  objective: '',
  targets: '',
  name: '',
  authorized: false,
  statement:
    '我确认已获得在上述目标内执行本次任务的授权，执行受服务端已实现能力、预算及截止时间限制。',
  concurrency: 3,
  maxTasks: 50,
  minutes: 10,
}
const draft = reactive({ ...defaults })
const initial = JSON.stringify(defaults)
const dirty = computed(() => JSON.stringify(draft) !== initial)
const form = ref<FormInstance>()
const errorPanel = ref<HTMLElement>()
const advancedOpen = ref(false)
const submitting = ref(false)
const submitError = ref('')
const createdRunID = ref('')
const locked = computed(() => submitting.value || Boolean(createdRunID.value))
const targetLines = computed(() =>
  draft.targets
    .split('\n')
    .map((value) => value.trim())
    .filter(Boolean),
)
// 授权绑定任务、目标、声明和有效时长，变更后必须重新确认。
watch(
  () => [draft.objective, draft.targets, draft.statement, draft.minutes],
  () => {
    draft.authorized = false
  },
)

// isIPv4 只接受标准十进制单主机，不接受网段、缩写或八进制地址。
function isIPv4(value: string) {
  return (
    /^(?:\d{1,3}\.){3}\d{1,3}$/.test(value) &&
    value.split('.').every((part) => Number(part) <= 255 && String(Number(part)) === part)
  )
}
// buildScope 仅校验目标与授权，工具、端口和步骤由服务端 Planner 决定。
function buildScope(): RunSpec['scope'] {
  const targets = [...new Set(targetLines.value)]
  if (!targets.length || targets.length > 10)
    throw new Error('请输入 1–10 个精确 HTTP/HTTPS URL，或一个裸 IPv4 地址。')
  const authorization = { confirmed: draft.authorized, statement: draft.statement.trim() }
  if (!authorization.confirmed || !authorization.statement)
    throw new Error('请确认绑定本次任务、目标与有效时长的授权声明。')
  if (targets.some(isIPv4)) {
    if (targets.length !== 1) throw new Error('裸 IPv4 仅支持单目标，不可与其他地址混填。')
    return { targets, authorization }
  }
  for (const target of targets) {
    try {
      const url = new URL(target)
      if (
        !/^https?:\/\//i.test(target) ||
        /[\s\\]/.test(target) ||
        !['http:', 'https:'].includes(url.protocol) ||
        !url.hostname ||
        url.hostname.includes('*') ||
        url.username ||
        url.password ||
        url.hash ||
        target.includes('#')
      )
        throw new Error()
    } catch {
      throw new Error(
        '请填写完整、精确的 HTTP/HTTPS URL，不接受裸域名、网段、通配符、账号密码或片段。HTTP 范围仍由服务端规范化校验。',
      )
    }
  }
  return { targets, authorization }
}
// updateConcurrency 保持并发输入为可校验的整数。
function updateConcurrency(value: number | string | null) {
  draft.concurrency = Number(value) || 1
}
// updateMaxTasks 保存任务预算，不预设前端任务数量。
function updateMaxTasks(value: number | string | null) {
  draft.maxTasks = Number(value) || 1
}
// updateMinutes 保留截止时长并触发范围授权重新确认。
function updateMinutes(value: number | string | null) {
  draft.minutes = Number(value) || 1
}
// close 关闭时保留草稿或已创建的运行，避免失败后重复创建。
function close() {
  if (submitting.value) return
  if (createdRunID.value) {
    emit('close')
  } else if (dirty.value) {
    Modal.confirm({
      title: '暂时关闭创建面板？',
      content: '未提交的任务描述和授权范围将保留，不会创建或启动运行。',
      okText: '保留草稿并关闭',
      cancelText: '继续编辑',
      onOk: () => emit('close'),
    })
  } else emit('close')
}
// showError 保留输入并让键盘焦点到达可重试的错误摘要。
async function showError(cause: unknown) {
  submitError.value = cause instanceof Error ? cause.message : '请求失败，草稿已保留，请重试。'
  await nextTick()
  errorPanel.value?.focus()
}
// finish 完成提交后才清理草稿，创建与启动失败均不清理。
function finish(started: boolean) {
  emit('created', createdRunID.value)
  emit('close')
  createdRunID.value = ''
  submitError.value = ''
  Object.assign(draft, defaults)
  advancedOpen.value = false
  form.value?.clearValidate()
  message.success(
    started ? '启动已受理，Planner 将异步生成并校验计划' : '运行已创建，启动后才进行规划',
  )
}
// retryStart 仅重试已创建运行的启动，不重复提交创建请求。
async function retryStart() {
  if (submitting.value || !createdRunID.value) return
  submitting.value = true
  submitError.value = ''
  try {
    await store.action('start', createdRunID.value)
    finish(true)
  } catch (cause) {
    await showError(cause)
  } finally {
    submitting.value = false
  }
}
// viewCreated 返回已创建运行，保留从运行页重新启动的能力。
function viewCreated() {
  if (!createdRunID.value || submitting.value) return
  finish(false)
}
// submit 只提交任务描述和范围，由服务端在 start 后规划真实任务。
async function submit(start: boolean) {
  if (locked.value) return
  submitting.value = true
  submitError.value = ''
  try {
    await form.value?.validate()
    const objective = draft.objective.trim()
    if (!objective || objective.length > 4000) throw new Error('任务描述须为 1–4000 字。')
    const scope = buildScope()
    if (
      !Number.isInteger(draft.concurrency) ||
      draft.concurrency < 1 ||
      draft.concurrency > 16 ||
      !Number.isInteger(draft.maxTasks) ||
      draft.maxTasks < 1 ||
      draft.maxTasks > 1000 ||
      !Number.isInteger(draft.minutes) ||
      draft.minutes < 1 ||
      draft.minutes > 120
    ) {
      throw new Error(
        '请检查高级选项：并发 1–16、任务预算 1–1000、截止时长 1–120 分钟，均须为整数。',
      )
    }
    const spec: RunSpec = {
      objective,
      name: draft.name.trim() || objective.replace(/\s+/g, ' ').slice(0, 60),
      source: 'web',
      mode: 'normal',
      scope,
      max_workers: draft.concurrency,
      max_tasks: draft.maxTasks,
      deadline: new Date(Date.now() + draft.minutes * 60000).toISOString(),
    }
    const run = await store.create(spec)
    createdRunID.value = run.id
    emit('created', run.id)
    if (start) await store.action('start', run.id)
    finish(start)
  } catch (cause) {
    if (cause instanceof Error) await showError(cause)
    else {
      submitError.value = '请检查任务描述、授权声明及高级选项，输入已保留。'
      await nextTick()
      errorPanel.value?.focus()
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <a-drawer
    :open="props.open"
    title="新建任务"
    :width="560"
    :closable="!submitting"
    :keyboard="!submitting"
    :mask-closable="false"
    @close="close"
  >
    <div class="drawer-intro">
      <span class="eyebrow">NEW COLLABORATIVE RUN</span>
      <h2>描述任务，声明范围</h2>
      <p>
        创建只保存任务。启动后由 Planner 生成并校验计划，Scheduler
        分配执行者；阶段复核与验收以服务端结果为准。
      </p>
    </div>
    <a-alert
      type="info"
      show-icon
      message="真实执行 · 仅限明确授权范围"
      description="工具、端口与步骤由 Planner 决定，受服务端当前能力与预算限制。未配置能力会明确拒绝，不回退演示；可能产生模型费用。"
    />
    <a-form ref="form" :model="draft" layout="vertical" class="create-form" :disabled="locked">
      <a-form-item
        label="任务描述"
        name="objective"
        :rules="[
          { required: true, whitespace: true, message: '请描述需要完成的任务' },
          { max: 4000, message: '任务描述不能超过 4000 字' },
        ]"
      >
        <a-textarea
          v-model:value="draft.objective"
          :rows="4"
          :maxlength="4000"
          show-count
          placeholder="说明想了解什么、期望的结果及限制；不要填写密钥或密码"
        />
      </a-form-item>
      <a-form-item
        label="准确目标 / 授权范围 · 每行一个"
        name="targets"
        :rules="[{ required: true, whitespace: true, message: '请填写精确授权目标' }]"
      >
        <a-textarea
          v-model:value="draft.targets"
          :rows="3"
          placeholder="填写 1–10 个完整 HTTP/HTTPS URL，或一个裸 IPv4 地址"
          aria-describedby="target-boundary"
        />
      </a-form-item>
      <p id="target-boundary" class="field-help">
        仅接受精确 URL 或单 IPv4，不支持裸域名、网段或通配符；具体限制可写在任务描述中。
      </p>
      <a-form-item
        label="授权声明 · 随运行保存"
        name="statement"
        :rules="[
          { required: true, whitespace: true, message: '请填写授权声明' },
          { max: 300, message: '授权声明不能超过 300 字' },
        ]"
      >
        <a-textarea v-model:value="draft.statement" :maxlength="300" :rows="3" />
      </a-form-item>
      <a-checkbox v-model:checked="draft.authorized"
        >我确认已获授权在上述目标内执行本次任务</a-checkbox
      >
      <p class="field-help authorization-note">
        授权有效至本次运行截止时间（{{ draft.minutes }}
        分钟后）；更改任务、目标、声明或有效时长须重新确认。
      </p>
      <button
        type="button"
        class="advanced-toggle"
        :disabled="locked"
        :aria-expanded="advancedOpen"
        aria-controls="create-advanced"
        @click="advancedOpen = !advancedOpen"
      >
        <span>高级选项 · 名称、预算、并发与截止时间</span
        ><DownOutlined :class="{ expanded: advancedOpen }" />
      </button>
      <Transition name="disclosure">
        <div v-if="advancedOpen" id="create-advanced" class="advanced-panel">
          <div class="advanced-content">
            <a-form-item
              label="运行名称 · 留空自动生成"
              name="name"
              :rules="[{ max: 100, message: '名称不能超过 100 字' }]"
            >
              <a-input
                v-model:value="draft.name"
                :maxlength="100"
                placeholder="默认取任务描述摘要"
              />
            </a-form-item>
            <div class="form-grid">
              <a-form-item label="并行任务上限"
                ><a-input-number
                  :value="draft.concurrency"
                  @update:value="updateConcurrency"
                  :min="1"
                  :max="16"
                  :precision="0"
              /></a-form-item>
              <a-form-item label="任务总预算"
                ><a-input-number
                  :value="draft.maxTasks"
                  @update:value="updateMaxTasks"
                  :min="1"
                  :max="1000"
                  :precision="0"
              /></a-form-item>
              <a-form-item label="截止时间 · 分钟后"
                ><a-input-number
                  :value="draft.minutes"
                  @update:value="updateMinutes"
                  :min="1"
                  :max="120"
                  :precision="0"
              /></a-form-item>
            </div>
            <p class="field-help">
              默认并发 {{ defaults.concurrency }}、预算 {{ defaults.maxTasks }}、{{
                defaults.minutes
              }}
              分钟。阶段补充与只读复核也受预算和截止时间限制。
            </p>
          </div>
        </div>
      </Transition>
    </a-form>
    <div class="planning-note">
      <InfoCircleOutlined /><span
        >前端不预置 Tasks
        或验收事实。任务计划与阶段状态由后端返回，任务完成不等于阶段验收通过。</span
      >
    </div>
    <Transition name="notice">
      <div v-if="submitError" ref="errorPanel" tabindex="-1" class="submit-error" role="alert">
        <a-alert
          type="error"
          show-icon
          :message="createdRunID ? '运行已创建，但启动未成功' : '创建未完成'"
          :description="submitError"
        />
        <p class="field-help">
          {{
            createdRunID
              ? '请重试启动，或返回运行页继续。不会重复创建，也不会静默切换到演示。'
              : '草稿与授权范围已保留。请按错误提示修正或配置后端能力后重试。'
          }}
        </p>
      </div>
    </Transition>
    <template #footer
      ><div class="drawer-actions">
        <a-button :disabled="submitting" @click="close">关闭</a-button>
        <template v-if="createdRunID">
          <a-button :disabled="submitting" @click="viewCreated">查看已创建运行</a-button>
          <a-button type="primary" :loading="submitting" @click="retryStart">重试启动</a-button>
        </template>
        <template v-else>
          <a-button :disabled="submitting" @click="submit(false)">仅创建</a-button>
          <a-button type="primary" :loading="submitting" @click="submit(true)">创建并启动</a-button>
        </template>
      </div></template
    >
  </a-drawer>
</template>

<style scoped>
.drawer-intro h2 {
  margin: 8px 0;
  color: var(--text-primary);
  font-size: 21px;
  font-weight: 620;
}
.drawer-intro p {
  margin-bottom: 18px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.8;
}
.create-form {
  margin-top: 22px;
}
.field-help {
  color: var(--text-tertiary);
  font-size: 12px;
  line-height: 1.7;
}
.authorization-note {
  margin-top: 8px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
.form-grid :deep(.ant-input-number) {
  width: 100%;
}
.advanced-toggle {
  display: flex;
  width: 100%;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-top: 16px;
  padding: 12px 0;
  color: var(--text-secondary);
  background: transparent;
  border: 0;
  border-top: 1px solid var(--line-subtle);
  text-align: left;
  cursor: pointer;
}
.advanced-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.advanced-toggle:focus-visible,
.submit-error:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
.advanced-toggle .anticon {
  transition: transform var(--motion-base) ease;
}
.advanced-toggle .expanded {
  transform: rotate(180deg);
}
.advanced-panel {
  display: grid;
  grid-template-rows: 1fr;
}
.advanced-content {
  min-height: 0;
  overflow: hidden;
}
.disclosure-enter-active,
.disclosure-leave-active {
  transition:
    grid-template-rows 220ms ease,
    opacity 160ms ease;
}
.disclosure-enter-from,
.disclosure-leave-to {
  grid-template-rows: 0fr;
  opacity: 0;
}
.planning-note {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 14px;
  color: var(--text-secondary);
  background: var(--surface-inset);
  border: 1px solid var(--line-strong);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.8;
}
.planning-note .anticon {
  color: var(--accent);
}
.submit-error {
  margin-top: 16px;
}
.drawer-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.notice-enter-active,
.notice-leave-active {
  transition:
    opacity 150ms ease,
    transform 150ms ease;
}
.notice-enter-from,
.notice-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
@media (prefers-reduced-motion: reduce) {
  .notice-enter-active,
  .notice-leave-active,
  .disclosure-enter-active,
  .disclosure-leave-active,
  .advanced-toggle .anticon {
    transition: none;
  }
}
@media (max-width: 480px) {
  .form-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
