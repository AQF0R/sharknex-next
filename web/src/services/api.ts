import { z } from 'zod'
import {
  runSchema,
  resultSchema,
  eventSchema,
  workerSchema,
  proposalSchema,
  evidenceSchema,
  type RunSpec,
} from '@/types/engine'

let bearer = ''
// setToken 只在页面内存中保存认证，不写入构建产物、URL 或浏览器存储。
export function setToken(token: string) {
  bearer = token
}
// request 在传输边界处理认证、取消、服务错误和运行时结构校验。
async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  signal?: AbortSignal,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (bearer) headers.Authorization = 'Bearer ' + bearer
  let response: Response
  try {
    response = await fetch('/api/v1' + path, {
      method: body === undefined ? 'GET' : 'POST',
      headers,
      signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (cause) {
    if (signal?.aborted) throw cause
    throw new Error('无法连接引擎，请检查本地服务与网络')
  }
  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    if (signal?.aborted) throw new DOMException('请求已取消', 'AbortError')
    throw new Error('引擎返回了非 JSON 响应，请检查代理或服务状态')
  }
  const envelope = z
    .object({ data: z.unknown().optional(), error: z.string().optional() })
    .parse(payload)
  if (!response.ok)
    throw new Error(
      response.status === 401 ? '请使用控制端令牌连接引擎' : envelope.error || '服务暂时不可用',
    )
  const result = schema.safeParse(envelope.data)
  if (!result.success) {
    const field = result.error.issues[0]?.path.join('.') || 'data'
    throw new Error('引擎返回的数据结构与控制台契约不一致：' + path.split('?')[0] + ' · ' + field)
  }
  return result.data
}
export const api = {
  runs: (signal?: AbortSignal) => request('/runs?limit=100', z.array(runSchema), signal),
  result: (id: string, signal?: AbortSignal) =>
    request('/runs/' + id + '/result', resultSchema, signal),
  fullResult: (id: string) =>
    request(
      '/runs/' + id + '/result',
      z.custom<unknown>((data) => resultSchema.safeParse(data).success),
    ),
  events: (id: string, after: number, signal?: AbortSignal) =>
    request('/runs/' + id + '/events?limit=200&after=' + after, z.array(eventSchema), signal),
  workers: (id: string, signal?: AbortSignal) =>
    request('/runs/' + id + '/workers?limit=200', z.array(workerSchema), signal),
  proposals: (id: string, signal?: AbortSignal) =>
    request('/runs/' + id + '/proposals?limit=200', z.array(proposalSchema), signal),
  evidence: (id: string, signal?: AbortSignal) =>
    request('/runs/' + id + '/evidence?limit=200', z.array(evidenceSchema), signal),
  create: (spec: RunSpec) => request('/runs', runSchema, undefined, spec),
  // action 让切换运行或断开连接取消过期操作响应，不影响后端已接受的动作。
  action: (id: string, action: 'start' | 'cancel', signal?: AbortSignal) =>
    request('/runs/' + id + '/' + action, z.string(), signal, {}),
}
