// 静态模型配置与画布草稿的类型、参数纯函数。模型定义随前端发布。
export interface ReferenceCapability {
  max?: number | null
  info: 'complete' | 'not_provided' | string
  required?: boolean
}
export interface ParameterCondition {
  when: Record<string, unknown>
  enum?: unknown[]
  minimum?: number | null
  maximum?: number | null
}
export interface CatalogParameter {
  key: string
  aliases?: string[]
  label: string
  role: 'count' | 'resolution' | 'size' | 'ratio' | 'duration' | 'audio' | 'generic' | string
  kind: 'enum' | 'integer' | 'number' | 'boolean' | 'string' | 'fixed'
  enum?: unknown[]
  optionLabels?: Record<string, string>
  minimum?: number | null
  maximum?: number | null
  step?: number | null
  unit?: '张' | '个'
  default?: unknown
  fixed?: unknown
  required: boolean
  conditions?: ParameterCondition[]
  unresolved?: boolean
  // 官方文档补充的 UI 描述：离散特殊值与连续范围分开，避免 -1 进入时长滑块。
  specialValues?: { value: number; label: string }[]
  allowCustom?: boolean
  note?: string
}
export interface CatalogModelParameters {
  status: 'ready' | 'partial' | 'missing' | string
  schemaVersion?: string
  parameters: CatalogParameter[]
  references: { image?: ReferenceCapability; video?: ReferenceCapability; audio?: ReferenceCapability }
  unsupported?: string[]
}
export interface CatalogModel {
  provider: string
  modelId: string
  optionId: string
  category: 'image' | 'video' | string
  group: 'primary' | 'candidate' | 'specialized'
  document: string
  defaults?: Record<string, unknown>
  name?: string
  capabilityTags?: string[]
  generationEnabled: boolean
  videoModes?: readonly ('text' | 'reference' | 'image')[]
  parameters?: CatalogModelParameters
}
// 节点草稿：保存目录模型选择与用户参数。提示词与参考素材使用既有字段，不复制进来。
export interface ModelSelection {
  provider: 'apimart'
  modelId: string
  catalogVersion?: string
  schemaVersion?: string
  parameters: Record<string, unknown>
}

export const APIMART_PROVIDER = 'apimart'

export function modelDisplayName(model: CatalogModel): string {
  return model.name?.trim() || model.modelId
}

export function modelSummary(model: CatalogModel): string {
  const tags = model.capabilityTags?.filter(Boolean) ?? []
  const parts = tags.slice(0, 3)
  if (!parts.length && model.parameters?.parameters.length) {
    return model.parameters.parameters.map((p) => p.label).join(' / ')
  }
  return parts.join(' / ')
}

export function parameterLabel(model: CatalogModel, key: string): string {
  return model.parameters?.parameters.find((p) => p.key === key)?.label ?? key
}

// activeCondition 返回与当前参数值匹配的联动约束；无匹配时使用基础约束。
export function activeCondition(parameter: CatalogParameter, values: Record<string, unknown>): ParameterCondition | null {
  for (const condition of parameter.conditions ?? []) {
    if (Object.entries(condition.when).every(([key, value]) => deepEqual(values[key], value))) return condition
  }
  return null
}

export function parameterEnum(parameter: CatalogParameter, values: Record<string, unknown>): unknown[] | null {
  const condition = activeCondition(parameter, values)
  const enumValues = condition?.enum?.length ? condition.enum : parameter.enum
  return enumValues?.length ? enumValues : null
}

export function parameterRange(parameter: CatalogParameter, values: Record<string, unknown>): { minimum: number | null; maximum: number | null } {
  const condition = activeCondition(parameter, values)
  const minimum = condition?.minimum != null ? condition.minimum : parameter.minimum ?? null
  const maximum = condition?.maximum != null ? condition.maximum : parameter.maximum ?? null
  return { minimum, maximum }
}

export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b) return false
  if (typeof a === 'number' && typeof b === 'number') return Object.is(a, b)
  return JSON.stringify(a) === JSON.stringify(b)
}

export function validParameterValue(parameter: CatalogParameter, value: unknown, values: Record<string, unknown>): boolean {
  if (value === undefined || value === null) return false
  if (parameter.specialValues?.some((option) => deepEqual(option.value, value))) return true
  switch (parameter.kind) {
    case 'fixed':
      return deepEqual(value, parameter.fixed)
    case 'enum': {
      const options = parameterEnum(parameter, values)
      return !!options?.some((option) => deepEqual(option, value)) || (parameter.allowCustom === true && typeof value === 'string' && value.trim().length > 0)
    }
    case 'integer':
      if (!Number.isInteger(value)) return false
      return withinRange(value as number, parameterRange(parameter, values)) && matchesStep(value as number, parameter.step)
    case 'number':
      if (!Number.isFinite(value)) return false
      return withinRange(value as number, parameterRange(parameter, values)) && matchesStep(value as number, parameter.step)
    case 'boolean':
      return typeof value === 'boolean'
    case 'string':
      return typeof value === 'string'
  }
  return false
}

function matchesStep(value: number, step: number | null | undefined): boolean {
  if (step == null || !Number.isFinite(step) || step <= 0) return true
  const units = value / step
  return Math.abs(units - Math.round(units)) < 1e-8
}

function withinRange(value: number, range: { minimum: number | null; maximum: number | null }): boolean {
  if (range.minimum != null && value < range.minimum) return false
  if (range.maximum != null && value > range.maximum) return false
  return true
}

// 本地面板初值与型号固定项；未指定初值的参数保持未选择。
export function defaultParametersFor(model: CatalogModel): Record<string, unknown> {
  const parameters: Record<string, unknown> = { ...model.defaults }
  for (const parameter of model.parameters?.parameters ?? []) {
    if (parameter.kind === 'fixed') {
      parameters[parameter.key] = parameter.fixed
    } else if (parameter.default !== undefined && parameter.default !== null) {
      parameters[parameter.key] = parameter.default
    }
  }
  return parameters
}

// normalizeParametersFor 在切换模型时保留仍合法的旧值；不兼容的值调整为
// 上游默认值，没有默认值时回到待选择状态，并报告调整明细。
export function normalizeParametersFor(model: CatalogModel, previous: Record<string, unknown> | undefined): { value: Record<string, unknown>; adjusted: string[] } {
  const value: Record<string, unknown> = { ...model.defaults }
  const adjusted: string[] = []
  const candidates = { ...defaultParametersFor(model) }
  for (const parameter of model.parameters?.parameters ?? []) {
    if (parameter.kind === 'fixed') { value[parameter.key] = parameter.fixed; continue }
    const old = previous?.[parameter.key] ?? parameter.aliases?.map((key) => previous?.[key]).find((entry) => entry !== undefined)
    if (old !== undefined && validParameterValue(parameter, old, value)) {
      value[parameter.key] = old
      continue
    }
    if (candidates[parameter.key] !== undefined && validParameterValue(parameter, candidates[parameter.key], value)) {
      if (old !== undefined) adjusted.push(parameter.label)
      value[parameter.key] = candidates[parameter.key]
    }
  }
  return { value, adjusted }
}

// 历史草稿按原样保存；仅在读取面板时补缺省值、识别旧字段别名。
export function parameterValuesFor(model: CatalogModel, saved: Record<string, unknown>): Record<string, unknown> {
  const values = { ...defaultParametersFor(model), ...saved }
  for (const field of model.parameters?.parameters ?? []) {
    if (field.kind === 'fixed') { values[field.key] = field.fixed; continue }
    if (saved[field.key] !== undefined) continue
    const alias = field.aliases?.find((key) => saved[key] !== undefined)
    if (alias) values[field.key] = saved[alias]
  }
  return values
}

export function referenceCapability(model: CatalogModel | undefined, kind: 'image' | 'video' | 'audio'): ReferenceCapability | undefined {
  return model?.parameters?.references?.[kind]
}

// 参数摘要用于面板按钮与模型列表：固定值与枚举展示实际可选值，不编造。
export function parameterSummary(model: CatalogModel, values: Record<string, unknown>): string {
  const parts: string[] = []
  for (const parameter of model.parameters?.parameters ?? []) {
    if (parameter.kind === 'fixed') continue
    // 摘要只保留常用画面参数，避免素材地址、任务 ID、随机种子撑开操作栏。
    if (!['ratio', 'resolution', 'size', 'duration', 'count'].includes(parameter.role)) continue
    const value = values[parameter.key]
    if (value === undefined || value === null) continue
    const special = parameter.specialValues?.find((option) => deepEqual(option.value, value))
    if (special) parts.push(special.label)
    else if (parameter.role === 'count') parts.push(`${String(value)}${parameter.unit ?? '张'}`)
    else if (parameter.role === 'duration') parts.push(`${String(value)}s`)
    else parts.push(parameter.optionLabels?.[String(value)] ?? String(value))
  }
  return parts.join(' · ')
}
