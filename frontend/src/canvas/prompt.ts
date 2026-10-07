import type { MediaNodeData } from './media'

// Names are fallback display text, never resource identity. Draft references follow
// a node; a later generation adapter must freeze its asset key at submission.
export type PromptPart = { type: 'text'; text: string } | {
  type: 'reference'; nodeId: string; kind: 'image' | 'video'; name: string
}
export interface MentionCandidate {
  id: string
  name: string
  kind: 'image' | 'video'
  url: string
  label: string
  selected: boolean
  error: string
}
export const hasPromptReferences = (parts?: PromptPart[]) => !!parts?.some((part) => part.type === 'reference')
export const promptText = (parts: PromptPart[]) => parts.map((part) => part.type === 'text' ? part.text : `@${part.name}`).join('')
export function normalizeParts(parts: PromptPart[]): PromptPart[] {
  const result: PromptPart[] = []
  for (const part of parts) {
    if (part.type === 'text') {
      if (!part.text) continue
      const last = result.at(-1)
      if (last?.type === 'text') last.text += part.text
      else result.push({ ...part })
    } else result.push({ ...part })
  }
  return result
}
export function draftParts(data: Pick<MediaNodeData, 'prompt' | 'promptParts'>): PromptPart[] {
  return normalizeParts(data.promptParts ?? [{ type: 'text', text: data.prompt ?? '' }])
}
// Logical editor offsets use one unit per chip, independent of its current label.
export const partLength = (part: PromptPart) => part.type === 'text' ? part.text.length : 1
export function sliceParts(parts: PromptPart[], start: number, end = Infinity): PromptPart[] {
  let offset = 0
  return parts.flatMap((part): PromptPart[] => {
    const from = offset
    offset += partLength(part)
    if (offset <= start || from >= end) return []
    return [part.type === 'reference' ? { ...part } : { type: 'text', text: part.text.slice(Math.max(0, start - from), end - from) }]
  })
}
