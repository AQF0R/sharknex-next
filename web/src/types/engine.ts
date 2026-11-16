import { z } from 'zod'

export const runSchema = z.object({
  id: z.string(),
  name: z.string(),
  objective: z.string().optional(),
  config: z
    .object({
      objective: z.string().optional(),
      summary: z.string().optional(),
      planning: z.object({ reason: z.string().optional() }).nullish(),
    })
    .nullish(),
  status: z.string(),
  source: z.string(),
  mode: z.string(),
  scope: z.object({
    targets: z.array(z.string()),
    tools: z.array(z.string()),
    tcp_ports: z.array(z.number()).optional(),
    authorization: z.object({ confirmed: z.boolean(), statement: z.string() }).optional(),
  }),
  max_workers: z.number(),
  max_tasks: z.number(),
  created_at: z.string(),
  started_at: z.string().nullable().optional(),
  finished_at: z.string().nullable().optional(),
  deadline_at: z.string().nullable().optional(),
})
export const taskSpecSchema = z.object({
  executor: z.enum(['local', 'pi', 'review', 'internal_review']).optional(),
  ports: z.array(z.number()).optional(),
  key: z.string(),
  goal: z.string(),
  type: z.string(),
  name: z.string(),
  target: z.string(),
  tools: z.array(z.string()),
  capabilities: z.array(z.string()),
  priority: z.number(),
  dependencies: z
    .array(z.object({ key: z.string(), type: z.string() }))
    .nullable()
    .optional(),
  delay_ms: z.number().optional(),
})
export const taskSchema = z.object({
  id: z.string(),
  run_id: z.string(),
  name: z.string(),
  task_type: z.string(),
  status: z.string(),
  parent_id: z.string().nullable(),
  goal_id: z.string().nullable(),
  priority: z.number(),
  assigned_worker_id: z.string().nullable(),
  retry_count: z.number(),
  max_retries: z.number(),
  input: taskSpecSchema,
  result: z.record(z.string(), z.unknown()),
  started_at: z.string().nullable(),
  finished_at: z.string().nullable(),
  idempotency_key: z.string(),
  created_at: z.string(),
})
export const eventSchema = z.object({
  id: z.string(),
  event_type: z.string(),
  sequence: z.number(),
  created_at: z.string(),
  task_id: z.string().nullable(),
  worker_id: z.string().nullable(),
  payload: z.record(z.string(), z.unknown()),
})
export const factSchema = z.object({
  id: z.string(),
  fact_type: z.string(),
  content: z.record(z.string(), z.unknown()),
  status: z.string(),
  source_task_id: z.string().nullable(),
  source_worker_id: z.string().nullable(),
  confidence: z.number(),
  created_at: z.string(),
})
export const goalSchema = z.object({
  id: z.string(),
  name: z.string(),
  status: z.string(),
  success_criteria: z.record(z.string(), z.unknown()).nullish(),
})
// 阶段验收以服务端快照为准，不能从任务完成数或事件动画推断通过。
export const stageSummarySchema = z.object({
  id: z.string(),
  key: z.string(),
  name: z.string(),
  status: z.enum([
    'pending',
    'running',
    'reviewing',
    'needs_work',
    'achieved',
    'failed',
    'cancelled',
  ]),
  depends_on: z.array(z.string()),
  targets: z.array(z.string()),
  required_facts: z.array(z.string()),
  completed_tasks: z.number(),
  total_tasks: z.number(),
  review_task_id: z.string().optional(),
  reviewer_id: z.string().optional(),
  review_mode: z.enum(['cross_review', 'self_review']).optional(),
  reason: z.string().optional(),
})
export const findingSchema = z.object({
  id: z.string(),
  title: z.string(),
  description: z.string(),
  status: z.string(),
  confidence: z.number(),
  source_task_id: z.string().nullable(),
})
export const workerSchema = z.object({
  id: z.string(),
  name: z.string(),
  status: z.string(),
  active_tasks: z.number(),
  max_concurrency: z.number(),
  last_heartbeat_at: z.string().nullable(),
})
export const proposalSchema = z.object({
  id: z.string(),
  worker_id: z.string(),
  parent_task_id: z.string().nullable(),
  status: z.string(),
  reason: z.string(),
  review_reason: z.string().nullable().optional(),
  proposed_task: taskSpecSchema,
})
export const evidenceSchema = z.object({
  id: z.string(),
  task_id: z.string().nullable(),
  fact_id: z.string().nullable(),
  evidence_type: z.string(),
  content_ref: z.string(),
  metadata: z.object({ size: z.number().optional() }).optional(),
  created_at: z.string(),
})
export const resultSchema = z.object({
  contract: z.string(),
  run: runSchema,
  goals: z.array(goalSchema).nullable(),
  stages: z.array(stageSummarySchema).nullish(),
  tasks: z.array(taskSchema).nullable(),
  facts: z.array(factSchema).nullable(),
  findings: z.array(findingSchema).nullable(),
  events: z.array(eventSchema).nullable(),
})
export type Run = z.infer<typeof runSchema>
export type Task = z.infer<typeof taskSchema>
export type TaskSpec = z.infer<typeof taskSpecSchema>
export type EngineEvent = z.infer<typeof eventSchema>
export type Fact = z.infer<typeof factSchema>
export type Worker = z.infer<typeof workerSchema>
export type Proposal = z.infer<typeof proposalSchema>
export type Evidence = z.infer<typeof evidenceSchema>
export type RunResult = z.infer<typeof resultSchema>
export type StageSummary = z.infer<typeof stageSummarySchema>
// 创建输入允许省略技术参数，返回快照仍要求服务端提供工具范围。
export interface RunScopeSpec {
  targets: string[]
  tools?: string[]
  tcp_ports?: number[]
  authorization?: { confirmed: boolean; statement: string }
}
export interface RunSpec {
  name: string
  objective?: string
  source: string
  mode: string
  scope: RunScopeSpec
  max_workers: number
  max_tasks: number
  deadline: string
  goals?: { key: string; name: string; required_facts: string[] }[]
  tasks?: TaskSpec[]
}
