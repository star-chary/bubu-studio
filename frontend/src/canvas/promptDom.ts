import { normalizeParts, partLength, type PromptPart } from './prompt'

export interface EditorSelection { anchor: number; focus: number }
const chip = (node: Node): node is HTMLElement => node instanceof HTMLElement && node.dataset.promptReference === 'true'
const tail = (node: Node) => node instanceof HTMLElement && node.dataset.promptTail === 'true'
export function readPromptDOM(root: Node): PromptPart[] {
  // Browsers leave a filler BR after Select All -> Backspace. It is an empty
  // editor, unlike our real newline text followed by the marked line-box tail.
  if (root instanceof HTMLElement && root.childNodes.length === 1 && root.firstChild?.nodeName === 'BR') return []
  const parts: PromptPart[] = []
  function visit(node: Node) {
    if (tail(node)) return
    if (chip(node)) {
      parts.push({ type: 'reference', nodeId: node.dataset.nodeId!, kind: node.dataset.kind as 'image' | 'video', name: node.dataset.name! })
    } else if (node.nodeType === Node.TEXT_NODE) parts.push({ type: 'text', text: node.textContent ?? '' })
    else if (node.nodeName === 'BR') parts.push({ type: 'text', text: '\n' })
    else {
      // Enter and paste are controlled by the editor. This also tolerates block
      // wrappers produced by accessibility tools or browser editing commands.
      if (['DIV', 'P'].includes(node.nodeName) && parts.length) parts.push({ type: 'text', text: '\n' })
      node.childNodes.forEach(visit)
    }
  }
  root.childNodes.forEach(visit)
  return normalizeParts(parts)
}
function length(node: Node): number {
  if (tail(node)) return 0
  if (chip(node) || node.nodeName === 'BR') return 1
  if (node.nodeType === Node.TEXT_NODE) return node.textContent?.length ?? 0
  return Array.from(node.childNodes).reduce((sum, child) => sum + length(child), 0)
}
function offset(root: Node, target: Node, targetOffset: number): number {
  const prefix = document.createRange()
  prefix.setStart(root, 0)
  prefix.setEnd(target, targetOffset)
  return readPromptDOM(prefix.cloneContents()).reduce((sum, part) => sum + partLength(part), 0)
}
export function editorSelection(root: HTMLElement): EditorSelection | null {
  const selection = window.getSelection()
  if (!selection?.anchorNode || !selection.focusNode || !root.contains(selection.anchorNode) || !root.contains(selection.focusNode)) return null
  return { anchor: offset(root, selection.anchorNode, selection.anchorOffset), focus: offset(root, selection.focusNode, selection.focusOffset) }
}
export function restoreEditorSelection(root: HTMLElement, selection: EditorSelection) {
  function point(value: number): [Node, number] {
    let remaining = Math.max(0, value)
    function visit(node: Node): [Node, number] | null {
      if (node.nodeType === Node.TEXT_NODE) {
        const size = length(node)
        if (remaining <= size) return [node, remaining]
        remaining -= size
      } else if (chip(node) || node.nodeName === 'BR') {
        const index = Array.prototype.indexOf.call(node.parentNode!.childNodes, node)
        if (remaining === 0) return [node.parentNode!, index]
        remaining--
        if (remaining === 0) return [node.parentNode!, index + 1]
      } else {
        for (const child of node.childNodes) { const result = visit(child); if (result) return result }
      }
      return null
    }
    return visit(root) ?? [root, root.childNodes.length]
  }
  const [anchor, anchorOffset] = point(selection.anchor)
  const [focus, focusOffset] = point(selection.focus)
  window.getSelection()?.setBaseAndExtent(anchor, anchorOffset, focus, focusOffset)
}
