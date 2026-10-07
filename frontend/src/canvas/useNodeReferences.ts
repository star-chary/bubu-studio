import { computed, ref, watch, type Ref } from 'vue'
import { MarkerType, useVueFlow, type Edge, type GraphNode } from '@vue-flow/core'
import { mediaKindLabel, type MediaKind, type MediaNodeData, type MediaReference } from './media'
import { videoReferenceError } from './videoReferences'
import { VIDEO_MODEL, videoModelDefinition } from '../models/videoModels'
import type { MentionCandidate } from './prompt'

type MediaGraphNode = GraphNode<MediaNodeData>
export type ReferenceCandidateState = 'target' | 'available' | 'chosen' | 'unavailable'

// 展示层与写入入口共用规则，不能只靠灰色样式阻止非法引用。
function unavailableReason(target: MediaGraphNode, source?: MediaGraphNode, keepExisting = false): string {
  if (!source || source.type !== 'media') return '素材已不存在'
  if (target.id === source.id) return '不能参考节点自身'
  if (target.data.kind === 'audio') return '音频只作为上传素材，不能引用其他节点'
  if (target.data.kind === 'image' && source.data.kind !== 'image') return '图片节点只能参考图片'
  if (target.data.kind === 'video' && !keepExisting) {
    if (target.data.videoMode === 'text') return '文生视频不使用参考素材，请先切换到全能参考'
  }
  const available = source.data.status === 'ready' || (keepExisting && source.data.status === 'loading')
  if (!available || !source.data.url) return '请选择已成功加载的素材，空节点或读取失败的素材不能作为参考'
  return ''
}

export function useNodeReferences(flowId: string, canvasId: Ref<string>) {
  const { nodes, findNode, updateNodeData } = useVueFlow(flowId)

  function capacityReason(target: MediaGraphNode, source: MediaGraphNode): string {
    if (target.data.kind !== 'video' || target.data.referenceIds?.includes(source.id)) return ''
    const model = videoModelDefinition(target.data.videoModel ?? VIDEO_MODEL)
    const count = (target.data.referenceIds ?? []).filter((id) => findNode(id)?.data.kind === source.data.kind).length
    const limit = model.maxReferences[source.data.kind]
    return count >= limit ? `${model.name} 最多参考 ${limit} 项${mediaKindLabel[source.data.kind]}，请移除多余参考或切换模型` : ''
  }
  const referenceTargetId = ref<string | null>(null)
  const feedback = ref('')
  const targetNode = computed<MediaGraphNode | undefined>(() =>
    referenceTargetId.value ? findNode(referenceTargetId.value) : undefined,
  )
  const isSelecting = computed(() => !!targetNode.value)

  function referencesFor(targetId: string): MediaReference[] {
    const target: MediaGraphNode | undefined = findNode(targetId)
    if (!target) return []
    const counts: Record<MediaKind, number> = { image: 0, video: 0, audio: 0 }
    return (target.data.referenceIds ?? []).flatMap((id) => {
      const source: MediaGraphNode | undefined = findNode(id)
      if (!source || target.data.kind === 'audio' || target.id === id || (target.data.kind === 'image' && source.data.kind !== 'image')) return []
      if (!source.data.asset && unavailableReason(target, source, true)) return []
      const kind = source.data.kind
      const index = ++counts[kind]
      return [{ id, name: source.data.name, kind, url: source.data.url ?? '',
        label: target.data.kind === 'video' ? `${mediaKindLabel[kind]}${index}` : String(index),
        error: target.data.kind === 'video' ? videoReferenceError(source.data, canvasId.value, id) : undefined }]
    })
  }

  const referenceEdges = computed<Edge[]>(() => nodes.value.flatMap((target) =>
    referencesFor(target.id).map((source) => ({
      id: `reference:${source.id}:${target.id}`,
      source: source.id,
      target: target.id,
      sourceHandle: 'reference-out',
      targetHandle: 'reference-in',
      type: 'default',
      selectable: false,
      focusable: false,
      updatable: false,
      deletable: false,
      style: { stroke: '#648bc1', strokeWidth: 1.5 },
      markerEnd: { type: MarkerType.ArrowClosed, color: '#648bc1', width: 14, height: 14 },
      ariaLabel: `${source.name} 参考到 ${target.data.name}`,
    })),
  ))

  function beginSelection(targetId: string) {
    const node = findNode(targetId)
    if (!node || node.type !== 'media' || node.data.kind === 'audio' || (node.data.kind === 'video' && node.data.videoMode === 'text')) return
    referenceTargetId.value = targetId
    feedback.value = ''
  }

  function finishSelection() {
    referenceTargetId.value = null
    feedback.value = ''
  }

  function selectReference(sourceId: string) {
    const target = targetNode.value
    if (!target) return
    const source: MediaGraphNode | undefined = findNode(sourceId)
    const reason = unavailableReason(target, source) || (source && capacityReason(target, source))
    if (reason) {
      feedback.value = reason
      return
    }
    const ids = target.data.referenceIds ?? []
    if (ids.includes(sourceId)) {
      feedback.value = '此素材已添加，可继续选择其他素材'
      return
    }
    updateNodeData(target.id, { referenceIds: [...ids, sourceId] })
    feedback.value = `已添加 ${source!.data.name}，可继续选择`
  }

  function removeReference(targetId: string, sourceId: string) {
    const target: MediaGraphNode | undefined = findNode(targetId)
    if (!target) return
    updateNodeData(targetId, { referenceIds: (target.data.referenceIds ?? []).filter((id) => id !== sourceId) })
    feedback.value = ''
  }

  function mentionCandidatesFor(targetId: string): MentionCandidate[] {
    const target: MediaGraphNode | undefined = findNode(targetId)
    if (!target) return []
    const selected = referencesFor(targetId)
    return nodes.value.flatMap((source): MentionCandidate[] => {
      if (source.type !== 'media' || source.id === targetId || !['image', 'video'].includes(source.data.kind) || (!source.data.url && !source.data.asset)) return []
      const reference = selected.find((item) => item.id === source.id)
      const asset = source.data.asset
      let error = unavailableReason(target, source) || capacityReason(target, source)
      if (!error && source.data.generation?.status === 'generating') error = '素材正在生成，请稍后选择'
      if (!error && (!asset || source.data.storage?.status !== 'saved')) error = source.data.storage?.status === 'saving' ? '素材正在保存' : '素材尚未保存，请先保存或重新上传'
      if (!error && (asset.canvasId !== canvasId.value || asset.nodeId !== source.id || asset.kind !== `${source.data.kind}s`)) error = '素材归属或类型不符'
      return [{ id: source.id, name: source.data.name, kind: source.data.kind, url: source.data.url ?? '',
        label: reference ? `${mediaKindLabel[source.data.kind as MediaKind]}${reference.label.replace(/^(图片|视频)/, '')}` : '',
        selected: !!reference, error }]
    })
  }

  // The menu and the write entry use the same live candidates, including saved
  // asset ownership. No selection-mode switch or second reference store.
  function addMentionReference(targetId: string, sourceId: string): string {
    const candidate = mentionCandidatesFor(targetId).find((item) => item.id === sourceId)
    if (!candidate || candidate.error) return candidate?.error || '素材已不存在'
    const target = findNode(targetId)!
    const ids: string[] = target.data.referenceIds ?? []
    if (!ids.includes(sourceId)) updateNodeData(targetId, { referenceIds: [...ids, sourceId] })
    return ''
  }

  function candidateState(sourceId: string): ReferenceCandidateState | undefined {
    const target = targetNode.value
    if (!target) return undefined
    if (sourceId === target.id) return 'target'
    const source = findNode(sourceId)
    if (unavailableReason(target, source) || (source && capacityReason(target, source))) return 'unavailable'
    return target.data.referenceIds?.includes(sourceId) ? 'chosen' : 'available'
  }

  // 新图加载期间保留已有关系；读取失败或素材被移除才清理，避免重新生成时误删连线。
  watch(() => nodes.value.map((node) => [node.id, node.type, node.data.kind, node.data.status, node.data.url]), () => {
    for (const target of nodes.value) {
      const ids: string[] = target.data.referenceIds ?? []
      // A durable source remains referenced through temporary preview/network
      // failures. Its load status must not erase saved relationships.
      const validIds = ids.filter((id) => {
        const source = findNode(id)
        if (source?.data.asset && id !== target.id && target.data.kind !== 'audio' && (target.data.kind !== 'image' || source.data.kind === 'image')) return true
        return !unavailableReason(target, source, true)
      })
      if (validIds.length !== ids.length) updateNodeData(target.id, { referenceIds: validIds })
    }
    if (referenceTargetId.value && !targetNode.value) finishSelection()
  })

  return { targetNode, isSelecting, feedback, referenceEdges, referencesFor, beginSelection, finishSelection, selectReference, removeReference, candidateState, mentionCandidatesFor, addMentionReference }
}
