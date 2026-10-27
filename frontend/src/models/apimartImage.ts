import type { TaskInput } from '../api/persistence'
import { parameterValuesFor, validParameterValue, type ModelSelection } from './modelCatalog'
import { catalogModelFor } from './staticModelCatalog'
import { apimartImageDefinition } from './apimartImageDefinitions'

export function apimartImageEnabled(selection?: ModelSelection): boolean {
	return !!selection && selection.provider === 'apimart' && !!apimartImageDefinition(selection.modelId) && catalogModelFor(selection.modelId, 'image', selection.parameters)?.generationEnabled === true
}

export function apimartImageCount(selection: ModelSelection): number {
  const model = catalogModelFor(selection.modelId, 'image', selection.parameters)
  if (!model?.parameters) return 1
  const field = model.parameters.parameters.find((parameter) => parameter.role === 'count')
  const value = field ? parameterValuesFor(model, selection.parameters)[field.key] : 1
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : 1
}

export function apimartImageInput(selection: ModelSelection, input: Pick<TaskInput, 'canvasId' | 'nodeId' | 'prompt' | 'promptParts'>, referenceKeys: string[]): TaskInput | null {
  const model = catalogModelFor(selection.modelId, 'image', selection.parameters)
  const definition = apimartImageDefinition(selection.modelId)
  if (!apimartImageEnabled(selection) || !definition || !model?.parameters || !input.prompt.trim() || referenceKeys.length > definition.maxReferenceImages) return null
  const values = parameterValuesFor(model, selection.parameters)
  if (model.parameters.parameters.some((field) => !validParameterValue(field, values[field.key], values))) return null
  if (selection.modelId === 'seedream-5-0-lite' && values.size === 'auto' && !referenceKeys.length) return null
  // Only documented basic fields are sent. Each output has an independent task.
  const visible = new Set(model.parameters.parameters.map((field) => field.key))
  const parameters = Object.fromEntries(definition.parameters.map(({ key }) => {
    if (key === 'n') return [key, 1]
    if (key === 'hd' && !visible.has(key)) return [key, false]
    return [key, values[key]]
  }).filter(([, value]) => value !== undefined))
  return { ...input, provider: 'apimart', model: selection.modelId, parameters, ...(referenceKeys.length ? { referenceKeys } : {}) }
}
