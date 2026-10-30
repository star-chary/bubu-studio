import type { TaskInput, TaskReference } from '../api/persistence'
import { parameterValuesFor, validParameterValue, type ModelSelection } from './modelCatalog'
import { catalogModelFor } from './staticModelCatalog'
import { apimartVideoDefinition } from './apimartVideoDefinitions'

export function apimartVideoEnabled(selection?: ModelSelection): boolean {
  return !!selection && selection.provider === 'apimart' && catalogModelFor(selection.modelId, 'video')?.generationEnabled === true
}

// The canvas draft remains the source of truth. Convert its native parameter
// names only when building an immutable submission; hidden fields stay saved.
export function apimartVideoInput(selection: ModelSelection, input: Pick<TaskInput, 'canvasId' | 'nodeId' | 'prompt' | 'promptParts' | 'mode'>, references: TaskReference[]): TaskInput | null {
  if (!apimartVideoEnabled(selection) || !input.prompt.trim() || [...input.prompt.trim()].length > 2000) return null
  const model = catalogModelFor(selection.modelId, 'video', selection.parameters, input.mode)!
  const definition = apimartVideoDefinition(selection.modelId)!
  if (!input.mode || !definition.modes.includes(input.mode)) return null
  const values = parameterValuesFor(model, selection.parameters)
  if (model.parameters!.parameters.some((field) => !validParameterValue(field, values[field.key], values))) return null
  if (input.mode === 'reference') {
    const images = references.filter((ref) => ref.kind === 'image').length
    const videos = references.filter((ref) => ref.kind === 'video').length
    if (!references.length || images > (definition.maxReferenceImages ?? 50) || videos > (definition.maxReferenceVideos ?? 0) || images + videos !== references.length) return null
    if (definition.motionControl && (images !== 1 || videos !== 1)) return null
  } else if (input.mode === 'image') {
    if (references.length !== 1 || references[0]?.kind !== 'image' || references[0]?.role !== 'first_frame') return null
    if (input.promptParts?.some((part) => part.type === 'reference' && part.nodeId !== references[0]?.nodeId)) return null
  } else if (input.promptParts?.some((part) => part.type === 'reference')) return null
  const parameters = Object.fromEntries(model.parameters!.parameters.map((field) => [field.key, values[field.key]]))
  const byRole = (role: string) => values[model.parameters!.parameters.find((field) => field.role === role)?.key ?? '']
  return {
    ...input, provider: 'apimart', model: model.modelId, parameters,
    ...(byRole('resolution') !== undefined ? { resolution: byRole('resolution') as string } : {}),
    ...(byRole('ratio') !== undefined ? { ratio: byRole('ratio') as string } : {}),
    ...(byRole('duration') !== undefined ? { duration: byRole('duration') as number } : {}),
    ...(byRole('audio') !== undefined ? { generateAudio: byRole('audio') as boolean } : {}),
    ...(input.mode !== 'text' ? { references } : {}),
  }
}
