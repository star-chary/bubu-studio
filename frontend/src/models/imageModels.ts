// 展示名称与供应商 ID 分开；Lite 沿用已验证可用的原 ID。
export const IMAGE_MODELS = [
  { id: 'doubao-seedream-5-0-260128', name: 'Doubao-Seedream-5.0-lite', maxReferenceImages: 14, resolutions: ['2K', '3K', '4K'] },
  { id: 'doubao-seedream-5-0-pro-260628', name: 'Doubao-Seedream-5.0-pro', maxReferenceImages: 10, resolutions: ['1K', '1.5K', '2K'] },
] as const

export type ImageModelId = typeof IMAGE_MODELS[number]['id']
export const DEFAULT_IMAGE_MODEL: ImageModelId = IMAGE_MODELS[0].id

export const IMAGE_RATIOS = ['1:1', '9:16', '16:9', '3:4', '4:3', '3:2', '2:3', '21:9', 'adaptive'] as const
export const IMAGE_COUNTS = [1, 2, 4] as const
export interface ImageOptions {
  resolution: '1K' | '1.5K' | '2K' | '3K' | '4K'
  ratio: typeof IMAGE_RATIOS[number]
  count: typeof IMAGE_COUNTS[number]
}
export type ImageGenerationOptions = Pick<ImageOptions, 'resolution' | 'ratio'>
export const DEFAULT_IMAGE_OPTIONS: ImageOptions = { resolution: '2K', ratio: 'adaptive', count: 1 }

export function imageModelDefinition(id: ImageModelId) {
  return IMAGE_MODELS.find((model) => model.id === id) ?? IMAGE_MODELS[0]
}

export function normalizeImageOptions(id: ImageModelId, options?: Partial<ImageOptions>): ImageOptions {
  const candidate = { ...DEFAULT_IMAGE_OPTIONS, ...options }
  const resolutions: readonly string[] = imageModelDefinition(id).resolutions
  return {
    resolution: resolutions.includes(candidate.resolution) ? candidate.resolution : '2K',
    ratio: IMAGE_RATIOS.includes(candidate.ratio) ? candidate.ratio : 'adaptive',
    count: IMAGE_COUNTS.includes(candidate.count) ? candidate.count : 1,
  }
}

export function maxReferenceImages(model: ImageModelId) {
  return imageModelDefinition(model).maxReferenceImages
}
