export interface AlignmentRect { x: number; y: number; width: number; height: number }
export interface AlignmentTarget extends AlignmentRect { id: string }
type Axis = 'x' | 'y'
type Anchor = 0 | 0.5 | 1
export interface AlignmentMatch { targetId: string; sourceAnchor: Anchor; targetAnchor: Anchor }
export interface AlignmentGuide { axis: Axis; coordinate: number; start: number; end: number }
export interface AlignmentState { x?: AlignmentMatch; y?: AlignmentMatch }

const anchors: Anchor[] = [0, 0.5, 1]
const SNAP_DISTANCE = 6
const RELEASE_DISTANCE = 10

function anchorPosition(rect: AlignmentRect, axis: Axis, anchor: Anchor) {
  return rect[axis] + rect[axis === 'x' ? 'width' : 'height'] * anchor
}

function findMatch(rect: AlignmentRect, targets: AlignmentTarget[], axis: Axis, zoom: number, previous?: AlignmentMatch) {
  const previousTarget = previous && targets.find((target) => target.id === previous.targetId)
  if (previous && previousTarget) {
    const delta = anchorPosition(previousTarget, axis, previous.targetAnchor) - anchorPosition(rect, axis, previous.sourceAnchor)
    // 进入和脱离用不同的距离，避免鼠标在临界点时反复吸附 / 释放。
    if (Math.abs(delta) * zoom <= RELEASE_DISTANCE) return { match: previous, target: previousTarget, delta }
  }

  let best: { match: AlignmentMatch; target: AlignmentTarget; delta: number; proximity: number } | undefined
  const otherAxis = axis === 'x' ? 'y' : 'x'
  for (const target of targets) {
    const proximity = Math.abs(anchorPosition(target, otherAxis, 0.5) - anchorPosition(rect, otherAxis, 0.5))
    for (const sourceAnchor of anchors) for (const targetAnchor of anchors) {
      const delta = anchorPosition(target, axis, targetAnchor) - anchorPosition(rect, axis, sourceAnchor)
      if (Math.abs(delta) * zoom > SNAP_DISTANCE) continue
      // 优先最小修正、邻近节点；多个锚点同时对齐时优先同类边 / 中心，最后保留稳定顺序。
      if (!best || Math.abs(delta) < Math.abs(best.delta) || (Math.abs(delta) === Math.abs(best.delta)
        && (proximity < best.proximity || (proximity === best.proximity && sourceAnchor === targetAnchor && best.match.sourceAnchor !== best.match.targetAnchor)))) {
        best = { match: { targetId: target.id, sourceAnchor, targetAnchor }, target, delta, proximity }
      }
    }
  }
  return best
}

export function alignPreview(rect: AlignmentRect, targets: AlignmentTarget[], zoom: number, previous: AlignmentState) {
  const x = findMatch(rect, targets, 'x', zoom, previous.x)
  const y = findMatch(rect, targets, 'y', zoom, previous.y)
  const offset = { x: x?.delta ?? 0, y: y?.delta ?? 0 }
  const snapped = { ...rect, x: rect.x + offset.x, y: rect.y + offset.y }
  const guides: AlignmentGuide[] = []
  if (x) guides.push({ axis: 'x', coordinate: anchorPosition(x.target, 'x', x.match.targetAnchor),
    start: Math.min(snapped.y, x.target.y), end: Math.max(snapped.y + snapped.height, x.target.y + x.target.height) })
  if (y) guides.push({ axis: 'y', coordinate: anchorPosition(y.target, 'y', y.match.targetAnchor),
    start: Math.min(snapped.x, y.target.x), end: Math.max(snapped.x + snapped.width, y.target.x + y.target.width) })
  return { offset, guides, matches: { x: x?.match, y: y?.match } }
}
