import { mediaKindLabel, type MediaKind, type MediaNodeData } from './media'
import { VIDEO_MODEL, videoModelDefinition, type VideoModelId } from '../models/videoModels'

export const videoReferenceLimits: Record<MediaKind, number> = videoModelDefinition(VIDEO_MODEL).maxReferences

// Selection is an editable draft. Only durable, server-inspected assets count
// as ready references; this UI preflight is not the future submit API's guard.
export function videoReferenceError(data: MediaNodeData, canvasId: string, nodeId: string): string {
  if (data.status !== 'ready' || data.generation?.status === 'generating') return '素材正在加载、生成或无法预览'
  if (data.storage?.status === 'saving') return '素材正在保存，请等待完成'
  if (data.storage?.status === 'error') return '素材保存失败，请重新保存或上传'
  const asset = data.asset
  if (!asset || data.storage?.status !== 'saved') return '素材尚未保存，请先保存或重新上传'
  if (asset.canvasId !== canvasId || asset.nodeId !== nodeId || asset.kind !== `${data.kind}s`) return '素材归属或类型不符，请重新上传'
  if (!asset.media || !asset.videoReference) return '缺少服务端媒体参数，请重新上传以校验视频参考要求'
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
