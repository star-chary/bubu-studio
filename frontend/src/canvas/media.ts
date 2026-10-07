import type { ImageModelId } from '../models/imageModels'
import type { StoredAsset } from '../api/assets'
import type { VideoMode, VideoModelId, VideoOptions } from '../models/videoModels'
import type { PromptPart } from './prompt'

export type MediaKind = 'image' | 'video' | 'audio'
export const mediaKindLabel: Record<MediaKind, string> = { image: '图片', video: '视频', audio: '音频' }

export interface MediaNodeData {
  kind: MediaKind
  name: string
  status: 'empty' | 'loading' | 'ready' | 'error'
  url?: string
  width?: number
  height?: number
  durationSeconds?: number
  error?: string
  prompt?: string
  promptParts?: PromptPart[]
  videoOptions?: VideoOptions
  videoModel?: VideoModelId
  videoMode?: VideoMode
  imageModel?: ImageModelId
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

export interface MediaDimensions {
  width: number
  height: number
  durationSeconds?: number
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
