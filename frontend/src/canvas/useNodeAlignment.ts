import { onBeforeUnmount, onMounted, shallowRef, watch, type Ref } from 'vue'
import { useVueFlow, type NodeDragEvent } from '@vue-flow/core'
import { getPreviewRect } from './media'
import { alignPreview, type AlignmentGuide, type AlignmentState } from './nodeAlignment'

export function useNodeAlignment(flowId: string, enabled: Ref<boolean>) {
  const { nodes, viewport, updateNode } = useVueFlow(flowId)
  const guides = shallowRef<AlignmentGuide[]>([])
  let draggingId: string | undefined
  let matches: AlignmentState = {}

  function clearGuides() {
    matches = {}
    guides.value = []
  }

  function stop() {
    draggingId = undefined
    clearGuides()
  }

  function start({ node, nodes: dragged }: NodeDragEvent) {
    stop()
    if (enabled.value && node?.type === 'media' && dragged.length === 1) draggingId = node.id
  }

  function drag({ node }: NodeDragEvent) {
    if (!enabled.value || !node || node.id !== draggingId) return
    // 当前画布是平铺节点。Vue Flow 每次先按指针写入原始 position，再发出 drag。
    // 必须替换 position 对象，不能原地修改：内部拖动记录仍需保留原始位置，才能自然脱离吸附。
    const raw = node.position
    const targets = nodes.value.filter((other) => other.id !== node.id && other.type === 'media' && !other.hidden)
      .map((other) => ({ id: other.id, ...getPreviewRect(other.position, other.data) }))
    const result = alignPreview(getPreviewRect(raw, node.data), targets, viewport.value.zoom, matches)
    matches = result.matches
    guides.value = result.guides
    if (result.offset.x || result.offset.y) updateNode(node.id, { position: { x: raw.x + result.offset.x, y: raw.y + result.offset.y } })
  }

  watch(enabled, (value) => { if (!value) stop() })
  watch(() => nodes.value.map((node) => node.id), (ids) => {
    if (draggingId && !ids.includes(draggingId)) stop()
    else if ((matches.x && !ids.includes(matches.x.targetId)) || (matches.y && !ids.includes(matches.y.targetId))) clearGuides()
  })
  function onVisibilityChange() { if (document.hidden) stop() }
  onMounted(() => {
    window.addEventListener('blur', stop)
    window.addEventListener('touchcancel', stop)
    document.addEventListener('visibilitychange', onVisibilityChange)
  })
  onBeforeUnmount(() => {
    stop()
    window.removeEventListener('blur', stop)
    window.removeEventListener('touchcancel', stop)
    document.removeEventListener('visibilitychange', onVisibilityChange)
  })
  return { guides, start, drag, stop }
}
