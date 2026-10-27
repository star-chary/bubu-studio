import type { ImageModelId, ImageOptions } from '../models/imageModels'
import type { StoredAsset } from '../api/assets'
import type { VideoMode, VideoModelId, VideoOptions } from '../models/videoModels'
import type { ModelSelection } from '../models/modelCatalog'
import type { PromptPart } from './prompt'

export type MediaKind = 'image' | 'video' | 'audio'
export const mediaKindLabel: Record<MediaKind, string> = { image: '图片', video: '视频', audio: '音频' }
// 展示框、连线端点和拖动对齐共享布局尺寸，标题不计入展示框。
export const MEDIA_NODE_HEADER_HEIGHT = 28

export interface MediaNodeData {
  kind: MediaKind
  name: string
  status: 'empty' | 'loading' | 'ready' | 'error'
  url?: string
  // 仅用于当前页面重新挂载预览，不写入画布快照或生成任务。
  previewAttempt?: number
  width?: number
  height?: number
  durationSeconds?: number
  error?: string
  prompt?: string
  promptParts?: PromptPart[]
  videoOptions?: VideoOptions
  videoModel?: VideoModelId
  videoMode?: VideoMode
  firstFrameId?: string
  imageModel?: ImageModelId
  imageOptions?: ImageOptions
  // 目录模型选择草稿：存在时节点以它为依据，旧 Ark 字段只保留历史值。
  modelSelection?: ModelSelection
  origin?: 'upload' | 'generated'
  asset?: StoredAsset
  storage?: { status: 'saving' | 'saved' | 'error'; error?: string }
  generation?: {
    taskId?: string
    taskStatus?: string
    updatedAt?: string
    createdAt?: string
    providerStatus?: string
    pollingError?: string
    status: 'generating' | 'idle' | 'error'
    prompt: string
    model?: string
    referenceKeys?: string[]
    error?: string
  }
  // 目标节点引用的素材 ID；缩略图与连线均由这里派生，不复制素材数据。
  referenceIds?: string[]
}

export interface MediaReference {
  id: string
  name: string
  kind: MediaKind
  url: string
  label: string
  error?: string
}

// 节点当前供应商：目录草稿（apimart）、历史 Ark 字段（ark）或尚未选择（none）。
export type NodeProvider = 'apimart' | 'ark' | 'none'
export function nodeProvider(data: Pick<MediaNodeData, 'modelSelection' | 'imageModel' | 'videoModel'>): NodeProvider {
  if (data.modelSelection) return data.modelSelection.modelId ? 'apimart' : 'none'
  return 'ark'
}
export function isApimartNode(data: Pick<MediaNodeData, 'modelSelection'>): boolean {
  return !!data.modelSelection?.modelId
}

export interface MediaDimensions {
  width: number
  height: number
  durationSeconds?: number
}

export function previewRetryURL(data: MediaNodeData): string | undefined {
  if (data.storage?.status === 'saved' && data.asset?.url) return data.asset.url
  return data.origin === 'generated' ? data.url : undefined
}

export function getMediaKind(file: File): MediaKind | null {
  if (file.type.startsWith('image/')) return 'image'
  if (file.type.startsWith('video/')) return 'video'
  if (['audio/mpeg', 'audio/mp3', 'audio/wav', 'audio/x-wav', 'audio/wave'].includes(file.type)) return 'audio'
  // 部分系统不会为本地文件提供 MIME；仅在缺失时按常见后缀判断，再由浏览器验证能否解码。
  if (!file.type) {
    if (/\.(png|jpe?g|webp|gif|avif|bmp|svg)$/i.test(file.name)) return 'image'
    if (/\.(mp4|webm|mov|m4v|ogv|ogg|mkv)$/i.test(file.name)) return 'video'
    if (/\.(wav|mp3)$/i.test(file.name)) return 'audio'
  }
  return null
}

export function getPreviewSize(data: MediaNodeData): MediaDimensions {
  if (data.kind === 'audio') return { width: 360, height: 150 }
  if (!data.width || !data.height) return { width: 480, height: 270 }
  const scale = Math.min(480 / data.width, 300 / data.height)
  return {
    width: Math.max(160, Math.round(data.width * scale)),
    height: Math.max(120, Math.round(data.height * scale)),
  }
}

export function getPreviewRect(position: { x: number; y: number }, data: MediaNodeData) {
  return { x: position.x, y: position.y + MEDIA_NODE_HEADER_HEIGHT, ...getPreviewSize(data) }
}
