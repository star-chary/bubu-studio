import { onBeforeUnmount, ref } from 'vue'
import { useVueFlow, type Node, type XYPosition } from '@vue-flow/core'
import { getMediaKind, getPreviewSize, previewRetryURL, isApimartNode, type MediaDimensions, type MediaKind, type MediaNodeData } from './media'
import { DEFAULT_IMAGE_MODEL, maxReferenceImages, normalizeImageOptions, type ImageModelId, type ImageOptions } from '../models/imageModels'
import { getStorageConfig, uploadAsset } from '../api/assets'
import { useCanvasSession } from './useCanvasSession'
import { submitTask, retryTaskStorage, type GenerationTask, type TaskInput, type TaskReference } from '../api/persistence'
import { VIDEO_MODEL, normalizeVideoOptions, type VideoMode, type VideoModelId, type VideoOptions } from '../models/videoModels'
import { apimartReferencesError, videoReferencesError } from './videoReferences'
import { hasPromptReferences, normalizeParts, promptText, type PromptPart } from './prompt'
import { normalizeParametersFor, parameterValuesFor, referenceCapability, validParameterValue, type ModelSelection } from '../models/modelCatalog'
import { MODEL_CONFIG_VERSION, catalogModelFor } from '../models/staticModelCatalog'
import { apimartVideoEnabled, apimartVideoInput } from '../models/apimartVideo'
import { apimartImageCount, apimartImageEnabled, apimartImageInput } from '../models/apimartImage'
import { apimartVideoModes, compatibleVideoMode } from '../models/apimartVideoDefinitions'
import type { CreditQuote } from '../api/credits'

interface AcceptedGeneration { input: TaskInput; quote?: CreditQuote; count?: number }

export function useMediaNodes(flowId: string, requestedId: string, temporary: boolean) {
  const { nodes, addNodes, removeNodes, findNode, updateNodeData } = useVueFlow(flowId)
  const importNotice = ref('')
  const urls = new Map<string, string>()
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
    // 新节点由用户明确选择供应商和型号，不自动回退到 Ark 默认模型。
    appendNode({ kind, name: `${kind === 'image' ? '图片' : '视频'}节点 ${sequence}`, status: 'empty',
      modelSelection: { provider: 'apimart', modelId: '', parameters: {} },
      ...(kind === 'video' ? { videoMode: 'reference' as const } : {}) }, position)
  }

  function deleteNodes(ids: string[]) {
    removeNodes(ids)
    for (const id of ids) {
      uploadRequests.get(id)?.abort()
      uploadFiles.delete(id)
      queuedUploads.delete(id)
      const queued = uploadQueue.indexOf(id)
      if (queued !== -1) uploadQueue.splice(queued, 1)
      const url = urls.get(id)
      if (url) URL.revokeObjectURL(url)
      urls.delete(id)
    }
    // 已提交的任务仍由服务端执行和结算；applyTask 会忽略已删除的节点。
  }

  function updatePrompt(id: string, parts: PromptPart[]) {
    const normalized = normalizeParts(parts)
    if (findNode(id)) updateNodeData(id, { prompt: promptText(normalized), promptParts: hasPromptReferences(normalized) ? normalized : undefined })
  }

  function updateImageModel(id: string, imageModel: ImageModelId) {
    const node = findNode(id)
    if (node?.data.kind === 'image' && node.data.origin !== 'upload') {
      // 选择 Ark 模型即离开目录草稿；历史 Ark 字段恢复为选择依据。
      updateNodeData(id, { imageModel, imageOptions: normalizeImageOptions(imageModel, node.data.imageOptions), modelSelection: undefined })
    }
  }

  function updateImageOptions(id: string, options: ImageOptions) {
    const node = findNode(id)
    if (node?.data.kind === 'image' && node.data.origin !== 'upload' && !isApimartNode(node.data)) updateNodeData(id, { imageOptions: normalizeImageOptions(node.data.imageModel ?? DEFAULT_IMAGE_MODEL, options) })
  }

  // 选择目录模型：合法旧值保留，不兼容值调整为上游默认或待选择状态。
  function updateModelSelection(id: string, modelId: string) {
    const node = findNode(id)
    if (!node || node.data.origin === 'upload' || node.data.kind === 'audio') return
    const videoMode = node.data.kind === 'video' ? compatibleVideoMode(node.data.videoMode ?? 'reference', apimartVideoModes(modelId)) : undefined
    const model = catalogModelFor(modelId, node.data.kind as 'image' | 'video', undefined, videoMode)
    if (!model || model.provider !== 'apimart') return
    const { value } = normalizeParametersFor(model, node.data.modelSelection?.parameters)
    const selection: ModelSelection = {
      provider: 'apimart', modelId: model.modelId, parameters: value,
      catalogVersion: MODEL_CONFIG_VERSION,
      schemaVersion: model.parameters?.schemaVersion || undefined,
    }
    updateNodeData(id, { modelSelection: selection, ...(node.data.kind === 'video' ? { videoMode } : {}) })
  }

  function updateModelParameter(id: string, key: string, value: unknown) {
    const node = findNode(id)
    const selection = node?.data.modelSelection
    if (!node || !selection?.modelId || node.data.origin === 'upload') return
    const model = catalogModelFor(selection.modelId, node.data.kind as 'image' | 'video', selection.parameters, node.data.videoMode)
    const field = model?.parameters?.parameters.find((parameter) => parameter.key === key)
    if (!model || !field || field.kind === 'fixed') return
    const effective = { ...parameterValuesFor(model, selection.parameters), [key]: value }
    if (value !== undefined && !validParameterValue(field, value, effective)) return
    const parameters = { ...selection.parameters, [key]: value }
    for (const alias of field.aliases ?? []) delete parameters[alias]
    // 编辑基础项时只调整受其联动影响的基础值；历史高级字段完整保留。
    const normalized = normalizeParametersFor(model, effective).value
    for (const other of model.parameters?.parameters ?? []) {
      if (other.key === key || other.kind === 'fixed') continue
      if (!validParameterValue(other, effective[other.key], effective) && normalized[other.key] !== undefined) parameters[other.key] = normalized[other.key]
    }
    updateNodeData(id, { modelSelection: { ...selection, parameters } })
  }

  function referenceInputFor(id: string): { keys: string[]; error: string } {
    const target = findNode(id)
    if (!target) return { keys: [], error: '节点已不存在' }
    const ids: string[] = target.data.referenceIds ?? []
    const apimart = isApimartNode(target.data)
    const limit = apimart ? undefined : maxReferenceImages(target.data.imageModel ?? DEFAULT_IMAGE_MODEL)
    const capability = apimart ? referenceCapability(catalogModelFor(target.data.modelSelection?.modelId ?? '', 'image', target.data.modelSelection?.parameters), 'image') : undefined
    const effectiveLimit = apimart ? (capability?.max ?? undefined) : limit
    const fail = (error: string) => ({ keys: [], error })
    if (effectiveLimit === 0 && ids.length) return fail('所选模型不支持参考图片，请移除参考或切换模型')
    if (effectiveLimit !== undefined && ids.length > effectiveLimit) return fail(`所选模型最多支持 ${effectiveLimit} 张参考图片，请移除多余参考或切换模型`)
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
      if (!apimart) {
        // Ark 的格式与尺寸约束只适用于 Ark 模型；目录模型的素材能力以目录信息为准。
        if (!['image/jpeg', 'image/png', 'image/webp', 'image/gif', 'image/bmp'].includes(asset.contentType)) return fail('参考图片请使用 PNG、JPEG、WebP、GIF 或 BMP 格式')
        const { width = 0, height = 0 } = source.data
        if (width <= 14 || height <= 14 || width * height > 36_000_000 || width > 16 * height || height > 16 * width) return fail('参考图片宽高须大于 14 像素、宽高比在 1:16～16:1 之间，总像素不超过 3600 万')
      }
      if (keys.includes(asset.key)) return fail('参考图片不能重复')
      keys.push(asset.key)
    }
    return { keys, error: '' }
  }

  function referenceErrorFor(id: string) {
    const target = findNode(id)
    if (!target) return ''
    const mode = target.data.modelSelection ? compatibleVideoMode(target.data.videoMode ?? 'reference', apimartVideoModes(target.data.modelSelection.modelId)) : target.data.videoMode
    // 已接入的文生模式忽略素材连线，但不能提交提示词中的素材标签。
    if (target.data.kind === 'video' && (!target.data.modelSelection || apimartVideoEnabled(target.data.modelSelection)) && mode === 'text') {
      return target.data.promptParts?.some((part: PromptPart) => part.type === 'reference')
        ? '文生视频不能包含素材标签，请先移除提示词中的 @ 素材标签' : ''
    }
    for (const part of target.data.promptParts ?? []) {
      if (part.type !== 'reference') continue
      const source = findNode(part.nodeId)
      if (!target.data.referenceIds?.includes(part.nodeId) || !source || source.data.kind !== part.kind) return '提示词中的素材标签已失效，请重新选择参考素材'
    }
    if (target.data.kind !== 'video') {
      const references = referenceInputFor(id)
      if (references.error) return references.error
      const selection = target.data.modelSelection
      if (selection?.modelId === 'seedream-5-0-lite' && !references.keys.length) {
        const model = catalogModelFor(selection.modelId, 'image', selection.parameters)
        if (model && parameterValuesFor(model, selection.parameters).size === 'auto') return '该模型的自动比例需要参考图片，请添加参考图片或选择固定比例'
      }
      return ''
    }
    if (mode === 'image' && (!target.data.firstFrameId || !target.data.referenceIds?.includes(target.data.firstFrameId))) return '请明确选择一张起始图片'
    if (mode === 'image' && target.data.promptParts?.some((part: PromptPart) => part.type === 'reference' && part.nodeId !== target.data.firstFrameId)) return '图生视频只能引用选定的起始图片，请移除其他素材标签'
    const ids: string[] = mode === 'image' ? [target.data.firstFrameId] : target.data.referenceIds ?? []
    const sources = ids.map((sourceId) => findNode(sourceId))
    if (sources.some((source) => !source || source.id === id)) return '参考素材已不存在或引用了自身'
    if (target.data.modelSelection) {
      // 目录草稿节点不套用 Ark 素材规则；限制只来自选中模型的目录信息。
      if (!isApimartNode(target.data)) return ''
      return apimartReferencesError(sources as { id: string; data: MediaNodeData }[], canvasId.value, catalogModelFor(target.data.modelSelection.modelId, 'video'), mode)
    }
    return videoReferencesError(sources as { id: string; data: MediaNodeData }[], canvasId.value, target.data.videoModel ?? VIDEO_MODEL)
  }

  function generationInputFor(id: string): TaskInput | null {
    const node = findNode(id)
    if (!session.ready.value || !session.enabled.value || !node || node.data.origin === 'upload' || node.data.kind === 'audio') return null
    const apimart = node.data.kind === 'video' ? apimartVideoEnabled(node.data.modelSelection) : apimartImageEnabled(node.data.modelSelection)
    if (node.data.modelSelection && !apimart) return null
    const prompt = node.data.promptParts?.length ? (node.data.prompt ?? '') : (node.data.prompt ?? '').trim()
    const promptParts = node.data.promptParts?.map((part: PromptPart) => ({ ...part }))
    if ([...prompt.trim()].length > 2000 || referenceErrorFor(id)) return null
    if (node.data.kind === 'image') {
      if (!prompt) return null
      const referenceKeys = referenceInputFor(id).keys
      if (apimart) return apimartImageInput(node.data.modelSelection!, { prompt, promptParts, canvasId: canvasId.value, nodeId: id }, referenceKeys)
      const { resolution, ratio } = normalizeImageOptions(node.data.imageModel ?? DEFAULT_IMAGE_MODEL, node.data.imageOptions)
      return { prompt, ...(promptParts?.length ? { promptParts } : {}), model: node.data.imageModel ?? DEFAULT_IMAGE_MODEL,
        imageOptions: { resolution, ratio }, canvasId: canvasId.value, nodeId: id, ...(referenceKeys.length ? { referenceKeys } : {}) }
    }
    const mode = apimart ? compatibleVideoMode(node.data.videoMode ?? 'reference', apimartVideoModes(node.data.modelSelection!.modelId))! : node.data.videoMode ?? 'reference'
    if (apimart) {
      const ids = mode === 'image' ? [node.data.firstFrameId!] : mode === 'reference' ? node.data.referenceIds ?? [] : []
      const references: (TaskReference | null)[] = ids.map((sourceId: string) => {
        const source = findNode(sourceId)
        return source?.data.asset ? { nodeId: sourceId, assetKey: source.data.asset.key, kind: source.data.kind, ...(mode === 'image' ? { role: 'first_frame' as const } : {}) } : null
      })
      if (references.some((ref) => !ref)) return null
      return apimartVideoInput(node.data.modelSelection!, { prompt, promptParts, canvasId: canvasId.value, nodeId: id, mode }, references as NonNullable<TaskInput['references']>)
    }
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
    const quote = accepted.quote
    const node = findNode(id)
    if (!node || node.data.kind !== 'image' || generationRequests.has(id) || node.data.generation?.status === 'generating' || node.data.status === 'loading') return
    const input = acceptedInputFor(id, accepted)
    if (!input) return
    if (!quote) return
    const options = normalizeImageOptions(node.data.imageModel ?? DEFAULT_IMAGE_MODEL, node.data.imageOptions)
    const count = accepted.count ?? 1
    const currentCount = input.provider === 'apimart' ? apimartImageCount(node.data.modelSelection!) : options.count
    if (count !== currentCount) return
    const prompt = input.prompt
    const model = input.model
    const referenceKeys = input.referenceKeys ?? []
    if (nodes.value.length + count - 1 > 300) {
      updateNodeData(id, { generation: { status: 'error', prompt, model, error: '每个画布最多 300 个节点，请减少生成数量或清理节点。' } })
      return
    }
    // 每张保留独立任务、资源和积分流水；批量只是同一输入快照的多次单图提交。
    const ids = [id]
    let slot = 1
    for (let index = 1; index < count; index++) {
      let position: XYPosition
      do {
        position = { x: node.position.x + (slot % 2) * 512, y: node.position.y + Math.floor(slot / 2) * 364 }
        slot++
      } while (nodes.value.some((other) => {
        const size = getPreviewSize(other.data)
        return position.x < other.position.x + size.width + 24 && position.x + 504 > other.position.x
          && position.y < other.position.y + size.height + 48 && position.y + 348 > other.position.y
      }))
      ids.push(appendNode({ kind: 'image', name: `${node.data.name} · ${index + 1}`, status: 'empty', prompt,
        promptParts: input.promptParts?.map((part) => ({ ...part })), imageModel: node.data.imageModel,
        ...(node.data.modelSelection ? { modelSelection: { ...node.data.modelSelection, parameters: { ...node.data.modelSelection.parameters, ...(node.data.modelSelection.parameters.n !== undefined ? { n: 1 } : {}) } } } : {}),
        imageOptions: { ...options, count: 1 }, referenceIds: [...(node.data.referenceIds ?? [])] }, position))
    }
    const jobs = ids.map((nodeId) => ({ nodeId, taskId: crypto.randomUUID(), controller: new AbortController() }))
    for (const job of jobs) {
      generationRequests.set(job.nodeId, job.controller)
      updateNodeData(job.nodeId, { generation: { status: 'generating', prompt, model, referenceKeys } })
    }
    let submitted = 0
    try {
      await session.flush()
      for (const job of jobs) {
        if (disposed) return
        if (!findNode(job.nodeId)) throw new Error('目标节点已移除，剩余图片未提交。')
        updateNodeData(job.nodeId, { generation: { status: 'generating', taskId: job.taskId, prompt, model, referenceKeys } })
        const billing = { acceptedPoints: quote.points, priceVersion: quote.priceVersion }
        const task = await submitTask(job.taskId, { ...input, nodeId: job.nodeId, ...billing })
        submitted++
        if (!disposed) session.track(task, true)
      }
    } catch (error) {
      if (disposed) return
      const message = error instanceof Error ? error.message : '图片提交失败，请稍后重试。'
      if (count > 1) importNotice.value = `本批共 ${count} 张，已确认提交 ${submitted} 张。${message} 请查看各节点或任务记录，避免重复生成。`
      for (const [index, job] of jobs.entries()) {
        if (index < submitted || !findNode(job.nodeId)) continue
        updateNodeData(job.nodeId, { generation: { ...findNode(job.nodeId)!.data.generation,
          status: 'error', prompt, model, referenceKeys,
          error: index === submitted ? message : '本批提交已停止，此图片尚未提交，可单独生成。' } })
      }
    } finally {
      for (const job of jobs) if (generationRequests.get(job.nodeId) === job.controller) generationRequests.delete(job.nodeId)
    }
  }

  function updateVideoOptions(id: string, options: VideoOptions) {
    const node = findNode(id)
    if (node?.data.kind === 'video' && !isApimartNode(node.data)) updateNodeData(id, { videoOptions: normalizeVideoOptions(node.data.videoModel ?? VIDEO_MODEL, options) })
  }

  function updateVideoModel(id: string, model: VideoModelId) {
    const node = findNode(id)
    if (node?.data.kind === 'video') {
      // 选择 Ark 模型即离开目录草稿。
      updateNodeData(id, { videoModel: model, videoMode: compatibleVideoMode(node.data.videoMode ?? 'reference', ['text', 'reference']), videoOptions: normalizeVideoOptions(model, node.data.videoOptions), modelSelection: undefined })
    }
  }

  function updateVideoMode(id: string, mode: VideoMode) {
    const node = findNode(id)
    if (!node || node.data.kind !== 'video') return
    const modes = node.data.modelSelection ? apimartVideoModes(node.data.modelSelection.modelId) : ['text', 'reference']
    if (modes.includes(mode)) updateNodeData(id, { videoMode: mode })
  }

  function updateFirstFrame(id: string, sourceId: string) {
    const node = findNode(id)
    if (node?.data.kind !== 'video' || !apimartVideoModes(node.data.modelSelection?.modelId).includes('image') || !node.data.referenceIds?.includes(sourceId) || findNode(sourceId)?.data.kind !== 'image') return
    updateNodeData(id, { firstFrameId: sourceId })
  }

  async function generateNodeVideo(id: string, accepted: AcceptedGeneration) {
    const node = findNode(id)
    if (!node || node.data.kind !== 'video' || generationRequests.has(id) || node.data.generation?.status === 'generating') return
    const input = acceptedInputFor(id, accepted)
    if (!input) return
    if (!accepted.quote) return
    const prompt = input.prompt
    const taskId = crypto.randomUUID()
    generationRequests.set(id, new AbortController())
    updateNodeData(id, { generation: { taskId, status: 'generating', prompt, model: input.model } })
    try {
      await session.flush()
      if (disposed || !findNode(id)) return
      const billing = { acceptedPoints: accepted.quote.points, priceVersion: accepted.quote.priceVersion }
      const task = await submitTask(taskId, { ...input, ...billing }, 'video')
      if (!disposed) session.track(task, true)
    }
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
      updateNodeData(id, { generation, origin: 'generated', url: result.url, status: reuse ? 'ready' : 'loading', error: undefined,
        width: reuse ? current.data.width : undefined, height: reuse ? current.data.height : undefined,
        asset: result.asset, storage: result.asset ? { status: 'saved' } : result.storageError ? { status: 'error', error: result.storageError } : undefined })
    } else updateNodeData(id, { generation })
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
      if (disposed || controller.signal.aborted || !findNode(id)) return
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
    const retryURL = previewRetryURL(node.data)
    uploadRequests.get(id)?.abort()
    uploadFiles.delete(id)
    const url = urls.get(id)
    if (url) URL.revokeObjectURL(url)
    urls.delete(id)
    updateNodeData(id, {
      status: 'error',
      url: retryURL,
      storage: node.data.origin === 'generated' || node.data.storage?.status === 'saved' ? node.data.storage : undefined,
      error: retryURL ? `${node.data.storage?.status === 'saved' ? '素材已保存' : '素材已生成'}，预览加载失败，请重新加载。` : '无法预览此文件，请检查文件是否损坏或格式是否受浏览器支持。',
    })
  }

  function retryPreview(id: string) {
    const node = findNode(id)
    if (!node || !['loading', 'error'].includes(node.data.status)) return
    const url = previewRetryURL(node.data)
    if (!url) return
    // 只重新读取已有地址；不提交生成、上传或重试转存请求。
    updateNodeData(id, { url, status: 'loading', error: undefined, previewAttempt: (node.data.previewAttempt ?? 0) + 1 })
  }

  function mediaReady(id: string, dimensions: MediaDimensions) {
    const node = findNode(id)
    if (!node || node.data.status !== 'loading') return
    if (node.data.kind === 'audio' ? !dimensions.durationSeconds || !Number.isFinite(dimensions.durationSeconds) : dimensions.width <= 0 || dimensions.height <= 0) {
      failMedia(id)
      return
    }
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
    for (const url of urls.values()) URL.revokeObjectURL(url)
    urls.clear()
  })

  const hasLocalMedia = () => uploadFiles.size > 0
  const isSubmitting = () => generationRequests.size > 0
  return { nodes, session, importNotice, createEmptyNode, deleteNodes, importFiles, mediaReady, failMedia, retryPreview, updatePrompt, updateImageModel, updateImageOptions, updateVideoModel, updateVideoMode, updateFirstFrame, updateVideoOptions, updateModelSelection, updateModelParameter, generationInputFor, generateNode, retryVideoStorage, saveUploadedNode, referenceErrorFor, hasLocalMedia, isSubmitting }
}
