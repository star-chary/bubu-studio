import definitions from '../../../backend/internal/apimart/video_models.json' with { type: 'json' }
import type { CatalogModel, CatalogParameter } from './modelCatalog'
import type { VideoMode } from './videoModels'

// Shared with the Go adapter at build time; entering a canvas needs no request.
export interface APIMartVideoDefinition {
  modelId: string
  name: string
  group: CatalogModel['group']
  document: string
  generationEnabled: boolean
  modes: VideoMode[]
  maxReferenceImages: number | null
  maxReferenceVideos?: number
  maxReferenceVideoSeconds?: number
  motionControl?: boolean
  parameters: CatalogParameter[]
  imageParameters?: Record<string, unknown>
}

export const APIMART_VIDEO_MODELS = definitions as unknown as readonly APIMartVideoDefinition[]
export function apimartVideoDefinition(id?: string): APIMartVideoDefinition | undefined {
  return APIMART_VIDEO_MODELS.find((model) => model.modelId === id)
}

export function apimartVideoModes(id?: string): readonly VideoMode[] {
  const model = apimartVideoDefinition(id)
  return model?.generationEnabled ? model.modes : []
}

export function compatibleVideoMode(current: VideoMode | undefined, modes: readonly VideoMode[]): VideoMode | undefined {
  if (current && modes.includes(current)) return current
  return modes.includes('text') ? 'text' : modes[0]
}

export const VIDEO_MODES: readonly { value: VideoMode; name: string; description: string }[] = [
  { value: 'text', name: '文生视频', description: '根据提示词生成视频' },
  { value: 'reference', name: '全能参考', description: '参考素材内容生成新的画面' },
  { value: 'image', name: '图生视频', description: '以指定图片作为起始画面' },
]
