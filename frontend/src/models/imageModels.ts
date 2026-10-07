// 展示名称与供应商 ID 分开；Lite 沿用已验证可用的原 ID。
export const IMAGE_MODELS = [
  { id: 'doubao-seedream-5-0-260128', name: 'Doubao-Seedream-5.0-lite', maxReferenceImages: 14 },
  { id: 'doubao-seedream-5-0-pro-260628', name: 'Doubao-Seedream-5.0-pro', maxReferenceImages: 10 },
] as const

export type ImageModelId = typeof IMAGE_MODELS[number]['id']
export const DEFAULT_IMAGE_MODEL: ImageModelId = IMAGE_MODELS[0].id

export function maxReferenceImages(model: ImageModelId) {
  return IMAGE_MODELS.find((item) => item.id === model)!.maxReferenceImages
}
