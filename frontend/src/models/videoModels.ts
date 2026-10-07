export const VIDEO_MODEL = 'doubao-seedance-2-5-260628'
export type VideoModelId = typeof VIDEO_MODEL | 'doubao-seedance-2-0-260128' | 'doubao-seedance-2-0-fast-260128' | 'doubao-seedance-2-0-mini-260615'
export type VideoMode = 'reference' | 'text'
export interface VideoOptions {
  resolution: '480p' | '720p'
  ratio: '16:9' | '4:3' | '1:1' | '3:4' | '9:16' | '21:9' | 'adaptive'
  duration: number
  generateAudio: boolean
}
export const DEFAULT_VIDEO_OPTIONS: VideoOptions = { resolution: '480p', ratio: '16:9', duration: 4, generateAudio: true }

export interface VideoModelDefinition {
  id: VideoModelId
  name: string
  minDuration: number
  maxDuration: number
  maxReferences: { image: number; video: number; audio: number }
  maxReferenceSeconds: number
  audioOnlyReference: boolean
  resolutions: readonly VideoOptions['resolution'][]
  ratios: readonly VideoOptions['ratio'][]
}

const ratios: readonly VideoOptions['ratio'][] = ['adaptive', '16:9', '4:3', '1:1', '3:4', '9:16', '21:9']
const resolutions: readonly VideoOptions['resolution'][] = ['480p', '720p']

export const VIDEO_MODELS: readonly VideoModelDefinition[] = [
  { id: VIDEO_MODEL, name: 'Seedance 2.5', minDuration: 4, maxDuration: 30,
    maxReferences: { image: 30, video: 10, audio: 10 }, maxReferenceSeconds: 30, audioOnlyReference: true, resolutions, ratios },
  { id: 'doubao-seedance-2-0-260128', name: 'Seedance 2.0', minDuration: 4, maxDuration: 15,
    maxReferences: { image: 9, video: 3, audio: 3 }, maxReferenceSeconds: 15, audioOnlyReference: false, resolutions, ratios },
  { id: 'doubao-seedance-2-0-fast-260128', name: 'Seedance 2.0 fast', minDuration: 4, maxDuration: 15,
    maxReferences: { image: 9, video: 3, audio: 3 }, maxReferenceSeconds: 15, audioOnlyReference: false, resolutions, ratios },
  { id: 'doubao-seedance-2-0-mini-260615', name: 'Seedance 2.0 mini', minDuration: 4, maxDuration: 15,
    maxReferences: { image: 9, video: 3, audio: 3 }, maxReferenceSeconds: 15, audioOnlyReference: false, resolutions, ratios },
]

export function videoModelDefinition(id?: string): VideoModelDefinition {
  return VIDEO_MODELS.find((model) => model.id === id) ?? VIDEO_MODELS[0]!
}

export function normalizeVideoOptions(modelId: VideoModelId, options?: Partial<VideoOptions>): VideoOptions {
  const model = videoModelDefinition(modelId)
  const candidate = { ...DEFAULT_VIDEO_OPTIONS, ...options }
  return {
    resolution: model.resolutions.includes(candidate.resolution) ? candidate.resolution : model.resolutions[0]!,
    ratio: model.ratios.includes(candidate.ratio) ? candidate.ratio : model.ratios[0]!,
    duration: Number.isFinite(candidate.duration) ? Math.max(model.minDuration, Math.min(model.maxDuration, Math.round(candidate.duration))) : model.minDuration,
    generateAudio: candidate.generateAudio,
  }
}
