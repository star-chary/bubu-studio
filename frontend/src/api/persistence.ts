import { authFetch, AuthError } from '../auth/session'
import type { GeneratedImage } from './images'
import type { PromptPart } from '../canvas/prompt'
import type { VideoMode, VideoOptions } from '../models/videoModels'
import type { StoredAsset } from './assets'
import type { MediaNodeData } from '../canvas/media'

export interface CanvasSnapshot {
  nodes: { id: string; position: { x: number; y: number }; data: Pick<MediaNodeData, 'kind' | 'name' | 'prompt' | 'promptParts' | 'imageModel' | 'videoModel' | 'videoMode' | 'videoOptions' | 'origin' | 'referenceIds'> }[]
  viewport: { x: number; y: number; zoom: number }
}
export interface TaskReference { nodeId: string; assetKey: string; kind: 'image' | 'video' | 'audio' }
export interface TaskInput extends Partial<VideoOptions> { prompt: string; promptParts?: PromptPart[]; model: string; canvasId: string; nodeId: string; referenceKeys?: string[]; references?: TaskReference[]; mode?: VideoMode }
export interface GenerationTask {
  id: string
  kind?: 'image' | 'video'
  creditPoints?: number
  creditPriceVersion?: string
  creditStatus?: 'legacy' | 'reserved' | 'review' | 'settled' | 'released'
  compiledPrompt?: string
  bindings?: TaskReference[]
  providerTaskId?: string
  providerStatus?: string
  pollingError?: { code: string; message: string }
  input: TaskInput
  status: 'queued' | 'preparing' | 'submitting' | 'running' | 'saving' | 'succeeded' | 'failed' | 'interrupted' | 'storage_failed'
  result?: Partial<GeneratedImage> & { model: string; durationSeconds?: number; width?: number; height?: number; hasAudio?: boolean }
  error?: { code: string; message: string }
  createdAt: string
  startedAt?: string
  finishedAt?: string
  updatedAt: string
}
export interface SavedCanvas {
  id: string; title?: string; version: number; snapshot: CanvasSnapshot; updatedAt: string
  assets: StoredAsset[]; tasks: GenerationTask[]; results: GenerationTask[]
}
export interface CanvasSummary {
  id: string; title: string; updatedAt: string; nodeCount: number
  preview: { position: { x: number; y: number }; kind: 'image' | 'video' | 'audio' }[]
}
export class PersistenceError extends Error {
  constructor(message: string, public code: string, public status: number) { super(message) }
}
export async function persistenceRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await authFetch(path, { cache: 'no-store', ...init, signal: init?.signal ?? AbortSignal.timeout(15_000) })
  const body = await response.json().catch(() => null)
  if (!response.ok) throw new PersistenceError(body?.error?.message || '保存服务暂时不可用，请稍后重试。', body?.error?.code || 'REQUEST_FAILED', response.status)
  if (!body) throw new Error('保存服务返回了无效数据。')
  return body as T
}
export function writeJSON<T>(path: string, body: unknown, method = 'POST') {
  return persistenceRequest<T>(path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
}
export const getTask = (id: string) => persistenceRequest<GenerationTask>(`/api/tasks/${id}`)
export const retryTaskStorage = (id: string) => writeJSON<GenerationTask>(`/api/tasks/${id}/storage-retries`, {})
export async function submitTask(id: string, input: TaskInput & { acceptedPoints: number; priceVersion: string }, kind: 'image' | 'video' = 'image'): Promise<GenerationTask> {
  const endpoint = `/api/${kind}s/generations`
  try { return await writeJSON<GenerationTask>(endpoint, { taskId: id, ...input }) }
  catch (error) {
    if (error instanceof PersistenceError || error instanceof AuthError) throw error
    // A lost HTTP response does not mean the server rejected the task. Only
    // repeat the same idempotency key; never invent a second paid task here.
    try { return await getTask(id) }
    catch (lookupError) {
      if (lookupError instanceof PersistenceError && lookupError.status === 404) return writeJSON<GenerationTask>(endpoint, { taskId: id, ...input })
      throw new Error('任务提交结果暂时无法确认，请恢复连接后刷新查询任务记录。')
    }
  }
}
