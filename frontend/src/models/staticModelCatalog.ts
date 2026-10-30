import type { CatalogModel, CatalogParameter, ModelSelection } from './modelCatalog'
import { APIMART_VIDEO_MODELS, apimartVideoDefinition } from './apimartVideoDefinitions'
import { apimartImageDefinition } from './apimartImageDefinitions'
import type { VideoMode } from './videoModels'
import billing from '../../../backend/internal/apimart/billing_models.json' with { type: 'json' }

export const MODEL_CONFIG_VERSION = 'static-models.2026-10-26'
type Field = Omit<CatalogParameter, 'required'> & { required?: boolean }
type Group = CatalogModel['group']
const models: CatalogModel[] = []
const five = ['16:9', '4:3', '1:1', '3:4', '9:16']
const seven = [...five, '3:2', '2:3']
const imageRatios = ['1:1', '4:3', '3:4', '16:9', '9:16', '3:2', '2:3', '4:5', '5:4', '21:9']

function choices(key: string, role: string, values: unknown[], initial: unknown, extra: Partial<Field> = {}): Field {
  const labels: Record<string, string> = { ratio: '比例', resolution: '清晰度', duration: '视频时长', audio: '生成音频', count: '生成数量' }
  return { key, label: labels[role]!, role, kind: 'enum', enum: values, ...(initial !== undefined ? { default: initial } : {}), ...extra }
}
function ratio(key: string, values: string[], initial?: string, note?: string): Field {
  return choices(key, 'ratio', values, initial, { aliases: ['size', 'aspect_ratio', 'ratio'].filter((alias) => alias !== key), ...(note ? { note } : {}) })
}
function clarity(values: string[], initial: string): Field { return choices('resolution', 'resolution', values, initial) }
function count(maximum: number, key = 'n', unit: '张' | '个' = '张'): Field {
  return maximum === 1
    ? { key, unit, label: '生成数量', role: 'count', kind: 'fixed', fixed: 1 }
    : { key, unit, label: '生成数量', role: 'count', kind: 'integer', minimum: 1, maximum, step: 1, default: 1 }
}
function add(category: 'image' | 'video', modelId: string, name: string, group: Group, path: string, fields: Field[], extra: Partial<CatalogModel> & { references?: NonNullable<CatalogModel['parameters']>['references'] } = {}) {
	const definition = category === 'image' ? apimartImageDefinition(modelId) : undefined
	const { references = definition ? { image: { max: definition.maxReferenceImages, info: 'complete' } } : {}, ...metadata } = extra
  models.push({
	  provider: 'apimart', modelId, optionId: modelId, name, group, category, generationEnabled: !!definition && modelId in billing.images,
    document: `https://docs.apimart.ai/en/api-reference/${category === 'image' ? 'images' : 'videos'}/${path}`,
    parameters: { status: 'ready', schemaVersion: MODEL_CONFIG_VERSION, references, parameters: fields.map((field) => ({ ...field, required: field.required ?? false })) },
    ...metadata,
  })
}

// 仅开放产品当前支持的五类参数；比例采用文档允许值的常用集合，不开放任意像素输入。
add('image', 'seedream-5-0-lite', 'Seedream 5.0 Lite', 'primary', 'seedream-5-lite/generation', [
  ratio('size', ['auto', ...seven, '2:1', '1:2', '21:9'], '1:1'), clarity(['2K', '3K', '4K'], '2K'), count(15),
])
add('image', 'seedream-5-0-pro', 'Seedream 5.0 Pro', 'primary', 'seedream-5-0-pro/generation', [
  ratio('size', ['auto', ...seven, '2:1', '1:2', '21:9'], 'auto'), clarity(['1K', '1.5K', '2K'], '1K'), count(1),
])
add('image', 'gemini-nano-banana-2.1', 'Nano Banana 2.1', 'primary', 'gemini-nano-banana-2.1/generation', [
  ratio('size', ['auto', ...imageRatios, '1:4', '4:1', '1:8', '8:1'], 'auto'), clarity(['1K', '2K', '4K'], '1K'), count(4),
])
for (const variant of ['flare', 'sunburst']) add('image', `gpt-image-2.5-${variant}`, `GPT Image 2.5 ${variant === 'flare' ? 'Flare' : 'Sunburst'}`, 'primary', 'gpt-image-2.5/generation', [
  ratio('size', ['auto', ...imageRatios, '2:1', '1:2', '3:1', '1:3', '9:21'], 'auto'), clarity(['1k', '2k', '4k'], '1k'), count(4),
])
for (const klein of [false, true]) add('image', `flux-3-image${klein ? '-klein' : ''}`, `FLUX 3 Image${klein ? ' Klein' : ''}`, klein ? 'candidate' : 'primary', `flux-3-image${klein ? '-klein' : ''}/generation`, [
  ratio('aspect_ratio', ['auto', '21:9', '2:1', '16:9', '3:2', '7:5', '4:3', '5:4', '1:1', '4:5', '3:4', '5:7', '2:3', '9:16', '1:2', '9:21'], 'auto'),
  clarity(['768sq', '1k', '1.5k', '2k', '4k'], '1k'), count(1),
])
add('image', 'qwen-image-3.0-pro', 'Qwen Image 3.0 Pro', 'primary', 'qwen-image-3.0/generation', [ratio('size', seven, '1:1'), clarity(['1K', '2K'], '1K'), count(6)])
add('image', 'z-image-turbo', 'Z Image Turbo', 'primary', 'z-image-turbo/generation', [ratio('size', seven, '1:1'), clarity(['1K', '2K'], '1K')])
add('image', 'gemini-3-pro-image-preview', 'Nano Banana Pro', 'candidate', 'gemini-3-pro/generation', [ratio('size', ['auto', ...imageRatios], '1:1'), clarity(['1K', '2K', '4K'], '1K'), count(1)])
for (const variant of ['flex', 'max']) add('image', `flux-2-${variant}`, `FLUX 2 ${variant === 'flex' ? 'Flex' : 'Max'}`, 'candidate', 'flux-2/generation', [
  ratio('size', ['auto', ...seven, '21:9', '9:21'], '1:1'), clarity(['1MP', '2MP', '3MP', '4MP'], '2MP'), count(1),
])
for (const version of ['8.2', '8.1', '7', 'niji7']) {
  const niji = version === 'niji7'
  const fields: Field[] = [ratio('size', [...seven, '21:9'], '1:1')]
  // --hd 仅 v8.1/v8.2 支持；--q 是质量/算力，不冒充像素清晰度。
  if (version.startsWith('8.')) fields.push(choices('hd', 'resolution', [false, true], false, { optionLabels: { false: '标准', true: '高清' } }))
  add('image', 'midjourney', niji ? 'Niji 7' : `Midjourney v${version}`, 'candidate', 'midjourney/imagine', fields, {
    optionId: `midjourney-${version}`, defaults: { version: niji ? '7' : version, niji },
  })
}

for (const definition of APIMART_VIDEO_MODELS) {
  models.push({
    provider: 'apimart', category: 'video', optionId: definition.modelId,
    modelId: definition.modelId, name: definition.name, group: definition.group,
    document: definition.document, generationEnabled: definition.generationEnabled && definition.modelId in billing.videos, videoModes: definition.modes,
    parameters: {
      // Automatic duration cannot be priced before submission. Preserve the
      // adapter contract for existing tasks, but do not offer it for new ones.
      status: 'ready', schemaVersion: MODEL_CONFIG_VERSION, parameters: definition.parameters.map((field) => {
        if (definition.modelId === 'seedance-2.5' && field.role === 'resolution') return { ...field, enum: ['480p'], default: '480p' }
        return definition.modelId in billing.videos && field.role === 'duration' ? { ...field, specialValues: [] } : field
      }),
      references: { image: { max: definition.maxReferenceImages, info: 'complete' }, video: { max: definition.maxReferenceVideos ?? 0, info: 'complete' }, audio: { max: 0, info: 'complete' } },
    },
  })
}

export const STATIC_MODELS: readonly CatalogModel[] = models
export function catalogModelsFor(category: 'image' | 'video'): CatalogModel[] { return models.filter((model) => model.category === category && model.generationEnabled) }

// modelId 是供应商身份；optionId 只用于菜单，同一 Midjourney 接口有四个版本选项。
export function catalogModelFor(modelId: string, category: 'image' | 'video', parameters?: Record<string, unknown>, videoMode?: VideoMode): CatalogModel | undefined {
  if (modelId !== 'midjourney') {
    const model = models.find((item) => item.category === category && item.optionId === modelId)
    const overrides = videoMode === 'image' ? apimartVideoDefinition(modelId)?.imageParameters : undefined
    if (!model?.parameters || !overrides) return model
    return { ...model, parameters: { ...model.parameters, parameters: model.parameters.parameters.map((field) =>
      field.key in overrides ? { ...field, kind: 'fixed', fixed: overrides[field.key] } : field) } }
  }
  const version = String(parameters?.version ?? '').replace(/^v/i, '')
  const optionId = parameters?.niji === true ? `midjourney-niji${version}` : `midjourney-${version}`
  return models.find((model) => model.category === category && model.optionId === optionId)
}
export function modelOptionIdFor(selection: ModelSelection, category: 'image' | 'video'): string {
  return catalogModelFor(selection.modelId, category, selection.parameters)?.optionId ?? selection.modelId
}
