import { onBeforeUnmount, ref } from 'vue'
import { useVueFlow, type Node, type XYPosition } from '@vue-flow/core'
import { getMediaKind, type MediaDimensions, type MediaKind, type MediaNodeData } from './media'
import { DEFAULT_IMAGE_MODEL, maxReferenceImages, type ImageModelId } from '../models/imageModels'
import { getStorageConfig, uploadAsset } from '../api/assets'
import { useCanvasSession } from './useCanvasSession'
import { submitTask, retryTaskStorage, type GenerationTask, type TaskInput } from '../api/persistence'
import { VIDEO_MODEL, normalizeVideoOptions, type VideoMode, type VideoModelId, type VideoOptions } from '../models/videoModels'
import { videoReferencesError } from './videoReferences'
import { hasPromptReferences, normalizeParts, promptText, type PromptPart } from './prompt'
import type { CreditQuote } from '../api/credits'

interface AcceptedGeneration { input: TaskInput; quote: CreditQuote }

export function useMediaNodes(flowId: string, requestedId: string, temporary: boolean) {
  const { nodes, addNodes, findNode, updateNodeData } = useVueFlow(flowId)
  const importNotice = ref('')
  const urls = new Map<string, string>()
  const loadTimers = new Map<string, ReturnType<typeof setTimeout>>()
  const generationRequests = new Map<string, AbortController>()
  const session = useCanvasSession(flowId, requestedId, temporary, applyTask)
  const canvasId = session.canvasId
  const uploadFiles = new Map<string, File>()
  const uploadRequests = new Map<string, AbortController>()
  const uploadQueue: string[] = []
  const queuedUploads = new Set<string>()
  let activeUploads = 0
  let disposed = false
  let sequence = 0

  function appendNode(data: MediaNodeData, position: XYPosition) {
    const id = crypto.randomUUID()
    const node: Node<MediaNodeData> = {
      id,
      type: 'media',
      position: { ...position },
      data,
      connectable: false,
      ariaLabel: data.name,
    }
    addNodes(node)
    return id
  }

  function createEmptyNode(kind: Exclude<MediaKind, 'audio'>, position: XYPosition) {
    sequence = Math.max(sequence, ...nodes.value.map((node) => Number(/^(?:图片|视频)节点 (\d+)$/.exec(node.data.name)?.[1] ?? 0))) + 1
    appendNode({ kind, name: `${kind === 'image' ? '图片' : '视频'}节点 ${sequence}`, status: 'empty',
      ...(kind === 'video' ? { videoModel: VIDEO_MODEL, videoMode: 'reference' as const } : {}) }, position)
  }

  function updatePrompt(id: string, parts: PromptPart[]) {
    const normalized = normalizeParts(parts)
    if (findNode(id)) updateNodeData(id, { prompt: promptText(normalized), promptParts: hasPromptReferences(normalized) ? normalized : undefined })
  }

  function updateImageModel(id: string, imageModel: ImageModelId) {
    const node = findNode(id)
    if (node?.data.kind === 'image' && node.data.origin !== 'upload') updateNodeData(id, { imageModel })
  }

  function referenceInputFor(id: string): { keys: string[]; error: string } {
    const target = findNode(id)
    if (!target) return { keys: [], error: '节点已不存在' }
    const ids: string[] = target.data.referenceIds ?? []
    const limit = maxReferenceImages(target.data.imageModel ?? DEFAULT_IMAGE_MODEL)
    const fail = (error: string) => ({ keys: [], error })
    if (ids.length > limit) return fail(`所选模型最多支持 ${limit} 张参考图片，请移除多余参考或切换模型`)
    const keys: string[] = []
    for (const sourceId of ids) {
      const source = findNode(sourceId)
      if (!source || sourceId === id || source.data.kind !== 'image') return fail('请选择其他图片节点作为参考')
      if (source.data.status !== 'ready' || source.data.generation?.status === 'generating') return fail('参考图片正在生成或加载，请等待完成')
      if (source.data.storage?.status === 'saving') return fail('参考图片正在保存，请等待完成后生成')
      if (source.data.storage?.status === 'error') return fail('参考图片保存失败，请先在素材节点重新保存或重新上传')
      const asset = source.data.asset
      if (!asset || source.data.storage?.status !== 'saved') return fail('参考图片尚未保存，请先保存或重新上传素材')
      if (asset.canvasId !== canvasId.value || asset.nodeId !== sourceId || asset.kind !== 'images') return fail('参考图片归属无效，请重新上传')
      if (!['image/jpeg', 'image/png', 'image/webp', 'image/gif', 'image/bmp'].includes(asset.contentType)) return fail('参考图片请使用 PNG、JPEG、WebP、GIF 或 BMP 格式')
      const { width = 0, height = 0 } = source.data
      if (width <= 14 || height <= 14 || width * height > 36_000_000 || width > 16 * height || height > 16 * width) return fail('参考图片宽高须大于 14 像素、宽高比在 1:16～16:1 之间，总像素不超过 3600 万')
      if (keys.includes(asset.key)) return fail('参考图片不能重复')
      keys.push(asset.key)
    }
    return { keys, error: '' }
  }

  function referenceErrorFor(id: string) {
    const target = findNode(id)
    if (target?.data.kind === 'video' && target.data.videoMode === 'text') {
      return target.data.promptParts?.some((part: PromptPart) => part.type === 'reference')
        ? '文生视频不能包含素材标签，请先移除提示词中的 @ 素材标签' : ''
    }
    for (const part of target?.data.promptParts ?? []) {
      if (part.type !== 'reference') continue
      const source = findNode(part.nodeId)
      if (!target?.data.referenceIds?.includes(part.nodeId) || !source || source.data.kind !== part.kind) return '提示词中的素材标签已失效，请重新选择参考素材'
    }
    if (target?.data.kind !== 'video') return referenceInputFor(id).error
    const ids: string[] = target.data.referenceIds ?? []
    const sources = ids.map((sourceId) => findNode(sourceId))
    if (sources.some((source) => !source || source.id === id)) return '参考素材已不存在或引用了自身'
    return videoReferencesError(sources as { id: string; data: MediaNodeData }[], canvasId.value, target.data.videoModel ?? VIDEO_MODEL)
  }

  function generationInputFor(id: string): TaskInput | null {
    const node = findNode(id)
    if (!session.ready.value || !session.enabled.value || !node || node.data.origin === 'upload' || node.data.kind === 'audio') return null
    const prompt = node.data.promptParts?.length ? (node.data.prompt ?? '') : (node.data.prompt ?? '').trim()
    const promptParts = node.data.promptParts?.map((part: PromptPart) => ({ ...part }))
    if ([...prompt.trim()].length > 2000 || referenceErrorFor(id)) return null
    if (node.data.kind === 'image') {
      if (!prompt) return null
      const referenceKeys = referenceInputFor(id).keys
      return { prompt, ...(promptParts?.length ? { promptParts } : {}), model: node.data.imageModel ?? DEFAULT_IMAGE_MODEL,
        canvasId: canvasId.value, nodeId: id, ...(referenceKeys.length ? { referenceKeys } : {}) }
    }
    const mode = node.data.videoMode ?? 'reference'
    if (mode === 'text' && !prompt.trim()) return null
    const model = node.data.videoModel ?? VIDEO_MODEL
    const options = normalizeVideoOptions(model, node.data.videoOptions)
    if (mode === 'text') return { prompt, ...(promptParts?.length ? { promptParts } : {}), model,
      canvasId: canvasId.value, nodeId: id, mode, ...options }
    const references = (node.data.referenceIds ?? []).map((sourceId: string) => {
      const source = findNode(sourceId)
      return source?.data.asset ? { nodeId: sourceId, assetKey: source.data.asset.key, kind: source.data.kind } : null
    })
    if (references.some((reference: unknown) => !reference)) return null
    return { prompt, ...(promptParts?.length ? { promptParts } : {}), model, canvasId: canvasId.value, nodeId: id,
      mode, ...options,
      references: references as NonNullable<TaskInput['references']>,
    }
  }

  function acceptedInputFor(id: string, accepted: AcceptedGeneration): TaskInput | null {
    const current = generationInputFor(id)
    return current && JSON.stringify(current) === JSON.stringify(accepted.input) ? accepted.input : null
  }

  async function generateNodeImage(id: string, accepted: AcceptedGeneration) {
    const node = findNode(id)
    if (!node || node.data.kind !== 'image' || generationRequests.has(id) || node.data.generation?.status === 'generating' || node.data.status === 'loading') return
    const input = acceptedInputFor(id, accepted)
    if (!input) return
    const prompt = input.prompt
    const model = input.model
    const referenceKeys = input.referenceKeys ?? []
    const controller = new AbortController()
    generationRequests.set(id, controller)
    // 固定本次提示词、模型和目标 ID；等待期间修改草稿或模型只影响下一次请求。
    updateNodeData(id, { generation: { status: 'generating', prompt, model, referenceKeys } })
    try {
      await session.flush()
      const taskId = crypto.randomUUID()
      updateNodeData(id, { generation: { status: 'generating', taskId, prompt, model, referenceKeys } })
      const task = await submitTask(taskId, { ...input, acceptedPoints: accepted.quote.points, priceVersion: accepted.quote.priceVersion })
      if (!disposed) session.track(task, true)
    } catch (error) {
      if (generationRequests.get(id) !== controller || !findNode(id)) return
      updateNodeData(id, {
        generation: {
          status: 'error', prompt, model, referenceKeys,
          error: error instanceof Error ? error.message : '图片提交失败，请稍后重试。',
        },
      })
    } finally {
      if (generationRequests.get(id) === controller) generationRequests.delete(id)
    }
  }

  function updateVideoOptions(id: string, options: VideoOptions) {
    const node = findNode(id)
    if (node?.data.kind === 'video') updateNodeData(id, { videoOptions: normalizeVideoOptions(node.data.videoModel ?? VIDEO_MODEL, options) })
  }

  function updateVideoModel(id: string, model: VideoModelId) {
    const node = findNode(id)
    if (node?.data.kind === 'video') updateNodeData(id, { videoModel: model, videoOptions: normalizeVideoOptions(model, node.data.videoOptions) })
  }

  function updateVideoMode(id: string, mode: VideoMode) {
    if (findNode(id)?.data.kind === 'video') updateNodeData(id, { videoMode: mode })
  }

  async function generateNodeVideo(id: string, accepted: AcceptedGeneration) {
    const node = findNode(id)
    if (!node || node.data.kind !== 'video' || generationRequests.has(id) || node.data.generation?.status === 'generating') return
    const input = acceptedInputFor(id, accepted)
    if (!input) return
    const prompt = input.prompt
    const taskId = crypto.randomUUID()
    generationRequests.set(id, new AbortController())
    updateNodeData(id, { generation: { taskId, status: 'generating', prompt, model: input.model } })
    try { await session.flush(); const task = await submitTask(taskId, { ...input, acceptedPoints: accepted.quote.points, priceVersion: accepted.quote.priceVersion }, 'video'); if (!disposed) session.track(task, true) }
    catch (error) { if (!disposed && findNode(id)) updateNodeData(id, { generation: { taskId, status: 'error', prompt, model: input.model, error: error instanceof Error ? error.message : '视频提交失败' } }) }
    finally { generationRequests.delete(id) }
  }

  async function retryVideoStorage(taskId: string) {
    const node = nodes.value.find((n) => n.data.generation?.taskId === taskId)
    try { const task = await retryTaskStorage(taskId); if (!disposed) session.track(task) }
    catch (error) { if (node) updateNodeData(node.id, { generation: { ...node.data.generation!, error: error instanceof Error ? error.message : '保存重试失败' } }) }
  }

  function generateNode(id: string, accepted: AcceptedGeneration) { return findNode(id)?.data.kind === 'video' ? generateNodeVideo(id, accepted) : generateNodeImage(id, accepted) }

  function applyTask(task: GenerationTask, force = false) {
    const id = task.input.nodeId
    const current = findNode(id)
    if (!current || current.data.kind !== (task.kind ?? 'image') || task.input.canvasId !== canvasId.value) return
    const previous = current.data.generation
    if (!force && previous?.status === 'generating' && previous.taskId && previous.taskId !== task.id) return
    if (!force && previous?.taskId === task.id && previous.updatedAt && previous.updatedAt >= task.updatedAt) return
    if (!force && previous?.taskId !== task.id && previous?.createdAt && previous.createdAt >= task.createdAt) return
    const running = ['queued', 'preparing', 'submitting', 'running', 'saving'].includes(task.status)
    const failed = ['failed', 'interrupted', 'storage_failed'].includes(task.status)
    const generation: MediaNodeData['generation'] = {
      updatedAt: task.updatedAt, createdAt: task.createdAt, providerStatus: task.providerStatus, pollingError: task.pollingError?.message,
      taskId: task.id, taskStatus: task.status, status: running ? 'generating' : failed ? 'error' : 'idle',
      prompt: task.input.prompt, model: task.input.model, referenceKeys: task.input.referenceKeys,
      error: task.error?.message,
    }
    if (task.status === 'succeeded' && task.result?.url) {
      const result = task.result
      const reuse = current.data.url === result.url && current.data.status === 'ready'
      clearLoadTimer(id)
      updateNodeData(id, { generation, origin: 'generated', url: result.url, status: reuse ? 'ready' : 'loading', error: undefined,
        width: reuse ? current.data.width : undefined, height: reuse ? current.data.height : undefined,
        asset: result.asset, storage: result.asset ? { status: 'saved' } : result.storageError ? { status: 'error', error: result.storageError } : undefined })
      if (!reuse) loadTimers.set(id, setTimeout(() => failMedia(id), 30_000))
    } else updateNodeData(id, { generation })
  }

  function clearLoadTimer(id: string) {
    const timer = loadTimers.get(id)
    if (timer) clearTimeout(timer)
    loadTimers.delete(id)
  }

  function saveUploadedNode(id: string) {
    if (disposed || !uploadFiles.has(id) || queuedUploads.has(id) || uploadRequests.has(id)) return
    uploadQueue.push(id)
    queuedUploads.add(id)
    pumpUploads()
  }

  function pumpUploads() {
    while (!disposed && activeUploads < 2 && uploadQueue.length) {
      const id = uploadQueue.shift()!
      activeUploads++
      void persistUpload(id).finally(() => {
        queuedUploads.delete(id)
        activeUploads--
        pumpUploads()
      })
    }
  }

  async function persistUpload(id: string) {
    let timeout: ReturnType<typeof setTimeout> | undefined
    try {
      const config = await getStorageConfig()
      const file = uploadFiles.get(id)
      const node = findNode(id)
      if (disposed || !file || !node || node.data.status !== 'ready') return
      if (!config.enabled) {
        updateNodeData(id, { storage: undefined })
        return
      }
      const limit = node.data.kind === 'image' ? config.maxImageBytes : node.data.kind === 'audio' ? (config.maxAudioBytes ?? 15_000_000) : config.maxVideoBytes
      if (file.size > limit) throw new Error(node.data.kind === 'image' ? '图片不能超过 20 MiB，当前仅本地预览。' : node.data.kind === 'audio' ? '音频不能超过 15 MB，当前仅本地预览。' : '视频不能超过 200 MiB，当前仅本地预览。')
      const controller = new AbortController()
      uploadRequests.set(id, controller)
      updateNodeData(id, { storage: { status: 'saving' } })
      await session.flush()
      timeout = setTimeout(() => controller.abort(), 125_000)
      const asset = await uploadAsset(file, { canvasId: canvasId.value, nodeId: id }, controller.signal)
      if (disposed || !findNode(id)) return
      // 保存成功仍复用本地预览，避免正在播放的视频重置，也不重复下载刚上传的文件。
      updateNodeData(id, { asset, storage: { status: 'saved' } })
      uploadFiles.delete(id)
    } catch (error) {
      if (!disposed && findNode(id)?.data.status === 'ready') updateNodeData(id, { storage: { status: 'error', error: uploadRequests.get(id)?.signal.aborted ? '保存超时，文件仍可本地预览，请重新保存。' : error instanceof Error ? error.message : '文件保存失败，请重新保存。' } })
    } finally {
      if (timeout) clearTimeout(timeout)
      uploadRequests.delete(id)
    }
  }

  function failMedia(id: string) {
    const node = findNode(id)
    if (!node || node.data.status === 'empty' || node.data.status === 'error') return
    clearLoadTimer(id)
    uploadRequests.get(id)?.abort()
    uploadFiles.delete(id)
    const url = urls.get(id)
    if (url) URL.revokeObjectURL(url)
    urls.delete(id)
    updateNodeData(id, {
      status: 'error',
      url: undefined,
      storage: node.data.storage?.status === 'saved' ? node.data.storage : undefined,
      error: node.data.origin === 'generated' ? '素材已生成，但预览加载失败或链接已失效。' : '无法预览此文件，请检查文件是否损坏或格式是否受浏览器支持。',
    })
  }

  function mediaReady(id: string, dimensions: MediaDimensions) {
    const node = findNode(id)
    if (!node || node.data.status !== 'loading') return
    if (node.data.kind === 'audio' ? !dimensions.durationSeconds || !Number.isFinite(dimensions.durationSeconds) : dimensions.width <= 0 || dimensions.height <= 0) {
      failMedia(id)
      return
    }
    clearLoadTimer(id)
    updateNodeData(id, { ...dimensions, status: 'ready' })
    if (uploadFiles.has(id)) saveUploadedNode(id)
  }

  function importFiles(files: File[], position: XYPosition) {
    const rejected: string[] = []
    let index = 0
    for (const file of files) {
      const kind = getMediaKind(file)
      if (!kind || !file.size) {
        rejected.push(file.name)
        continue
      }
      // 一次选择的素材按两列放置，预留最大预览尺寸，异步读完也不会相互遮挡。
      const target = { x: position.x + (index % 2) * 512, y: position.y + Math.floor(index / 2) * 364 }
      const url = URL.createObjectURL(file)
      const id = appendNode({ kind, name: file.name, url, origin: 'upload', status: 'loading' }, target)
      urls.set(id, url)
      uploadFiles.set(id, file)
      loadTimers.set(id, setTimeout(() => failMedia(id), 30_000))
      index += 1
    }
    importNotice.value = rejected.length
      ? `已跳过 ${rejected.length} 个文件：${rejected.join('、')}。请选择非空的图片、视频或 WAV/MP3 音频。`
      : ''
  }

  onBeforeUnmount(() => {
    disposed = true
    for (const controller of uploadRequests.values()) controller.abort()
    uploadRequests.clear()
    uploadFiles.clear()
    uploadQueue.length = 0
    queuedUploads.clear()
    for (const controller of generationRequests.values()) controller.abort()
    generationRequests.clear()
    for (const timer of loadTimers.values()) clearTimeout(timer)
    for (const url of urls.values()) URL.revokeObjectURL(url)
    loadTimers.clear()
    urls.clear()
  })

  const hasLocalMedia = () => uploadFiles.size > 0
  const isSubmitting = () => generationRequests.size > 0
  return { nodes, session, importNotice, createEmptyNode, importFiles, mediaReady, failMedia, updatePrompt, updateImageModel, updateVideoModel, updateVideoMode, updateVideoOptions, generationInputFor, generateNode, retryVideoStorage, saveUploadedNode, referenceErrorFor, hasLocalMedia, isSubmitting }
}
