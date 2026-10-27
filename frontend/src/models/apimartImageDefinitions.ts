import definitions from '../../../backend/internal/apimart/image_models.json' with { type: 'json' }

export interface APIMartImageDefinition {
  modelId: string
  maxReferenceImages: number
  parameters: { key: string }[]
}

export const APIMART_IMAGE_MODELS = definitions as readonly APIMartImageDefinition[]
export function apimartImageDefinition(id?: string): APIMartImageDefinition | undefined {
  return APIMART_IMAGE_MODELS.find((model) => model.modelId === id)
}
