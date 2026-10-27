import { mediaKindLabel, type MediaKind, type MediaNodeData } from './media'
import { VIDEO_MODEL, videoModelDefinition, type VideoMode, type VideoModelId } from '../models/videoModels'
import { referenceCapability, type CatalogModel } from '../models/modelCatalog'

export const videoReferenceLimits: Record<MediaKind, number> = videoModelDefinition(VIDEO_MODEL).maxReferences

// referenceReadinessError 是素材就绪性检查（已保存、归属正确、可预览），与供应商无关。
export function referenceReadinessError(data: MediaNodeData, canvasId: string, nodeId: string): string {
  if (data.status !== 'ready' || data.generation?.status === 'generating') return '素材正在加载、生成或无法预览'
  if (data.storage?.status === 'saving') return '素材正在保存，请等待完成'
  if (data.storage?.status === 'error') return '素材保存失败，请重新保存或上传'
  const asset = data.asset
  if (!asset || data.storage?.status !== 'saved') return '素材尚未保存，请先保存或重新上传'
  if (asset.canvasId !== canvasId || asset.nodeId !== nodeId || asset.kind !== `${data.kind}s`) return '素材归属或类型不符，请重新上传'
  return ''
}

// videoReferenceError 在就绪性检查之上追加 Ark 专属的媒体参数校验。
export function videoReferenceError(data: MediaNodeData, canvasId: string, nodeId: string): string {
  const error = referenceReadinessError(data, canvasId, nodeId)
  if (error) return error
  const asset = data.asset
  if (!asset?.media || !asset.videoReference) return '缺少服务端媒体参数，请重新上传以校验视频参考要求'
  if (!asset.videoReference.eligible) return asset.videoReference.reason || '素材不符合视频参考要求'
  return ''
}

export function videoReferencesError(sources: { id: string; data: MediaNodeData }[], canvasId: string, modelId: VideoModelId = VIDEO_MODEL): string {
  const model = videoModelDefinition(modelId)
  if (!sources.length) return '全模态参考需要至少 1 项图片、视频或音频'
  const counts: Record<MediaKind, number> = { image: 0, video: 0, audio: 0 }
  const duration = { video: 0, audio: 0 }
  const keys = new Set<string>()
  for (const source of sources) {
    const kind = source.data.kind
    const label = `${mediaKindLabel[kind]}${++counts[kind]}`
    if (counts[kind] > model.maxReferences[kind]) return `${model.name} 最多参考 ${model.maxReferences[kind]} 项${mediaKindLabel[kind]}，请移除多余参考或切换模型`
    const error = videoReferenceError(source.data, canvasId, source.id)
    if (error) return `${label}：${error}`
    const asset = source.data.asset!
    if (keys.has(asset.key)) return `${label}：同一素材不能重复引用`
    keys.add(asset.key)
    if (kind !== 'image') {
      const seconds = asset.media?.durationSeconds
      if (seconds === undefined || !Number.isFinite(seconds) || seconds < 2 || seconds > model.maxReferenceSeconds) return `${label}：单个参考时长须为 2～${model.maxReferenceSeconds} 秒`
      duration[kind] += seconds
      if (duration[kind] > model.maxReferenceSeconds) return `参考${mediaKindLabel[kind]}总时长不能超过 ${model.maxReferenceSeconds} 秒，请移除或缩短素材`
    }
  }
  if (!model.audioOnlyReference && counts.image + counts.video === 0) return `${model.name} 的音频参考须搭配至少 1 项图片或视频`
  return ''
}

// apimartReferencesError 只采用目录模型明确提供的参考能力：
// 没有提供条数时不施加数量上限，也不套用 Ark 的时长与组合规则。
export function apimartReferencesError(sources: { id: string; data: MediaNodeData }[], canvasId: string, model?: CatalogModel, mode: VideoMode = 'reference'): string {
  if (!sources.length) return model?.generationEnabled ? mode === 'image' ? '请选择一张起始图片' : '全能参考需至少添加 1 张参考图片' : ''
  if (mode === 'image' && (sources.length !== 1 || sources[0]?.data.kind !== 'image')) return '图生视频需要一张起始图片'
  const name = model?.name?.trim() || model?.modelId || '该模型'
  const counts: Record<MediaKind, number> = { image: 0, video: 0, audio: 0 }
  const keys = new Set<string>()
  for (const source of sources) {
    const kind = source.data.kind
    const label = `${mediaKindLabel[kind]}${++counts[kind]}`
    const capability = referenceCapability(model, kind)
    if (mode !== 'image' && capability?.max === 0) return `${name} 当前不支持${mediaKindLabel[kind]}参考，请移除该参考素材`
    if (mode !== 'image' && capability?.max != null && counts[kind] > capability.max) return `${name} 最多参考 ${capability.max} 项${mediaKindLabel[kind]}，请移除多余参考或切换模型`
    const error = referenceReadinessError(source.data, canvasId, source.id)
    if (error) return `${label}：${error}`
    const asset = source.data.asset!
    if (keys.has(asset.key)) return `${label}：同一素材不能重复引用`
    keys.add(asset.key)
  }
  return ''
}

// apimartReferenceCapabilityText 生成面板上的素材能力说明；缺失时明确说明尚未提供。
export function apimartReferenceSummary(model?: CatalogModel): string {
  if (!model) return '参考素材能力信息尚未提供'
  const capabilities = model.parameters?.references
  const entries = ['image', 'video', 'audio'] as const
  const parts = entries.flatMap((kind) => {
    const capability = capabilities?.[kind]
    if (!capability || capability.max === 0) return []
    const label = mediaKindLabel[kind]
    const max = capability.max != null ? `最多 ${capability.max} 项` : '参考'
    return [`${label}：${max}`]
  })
  if (!parts.length) {
    const tags = model.capabilityTags?.filter((tag) => /Image|Video/i.test(tag))
    return tags?.length ? `${tags.join(' / ')}（条数限制尚未提供）` : '参考素材能力信息尚未提供'
  }
  return parts.join(' · ')
}
