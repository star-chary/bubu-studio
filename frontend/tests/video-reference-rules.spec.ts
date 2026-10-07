import { test, expect } from './setup'
import { videoReferenceError, videoReferencesError } from '../src/canvas/videoReferences'
import type { MediaKind, MediaNodeData } from '../src/canvas/media'

function source(kind: MediaKind, i: number, durationSeconds = 2) {
  const id = `${kind}-${i}`
  const data: MediaNodeData = { kind, name: id, status: 'ready', storage: { status: 'saved' }, asset: {
    key: id, url: `/api/assets/content?key=${id}`, canvasId: 'canvas', nodeId: id,
    kind: `${kind}s`, bytes: 1000, source: 'uploads', contentType: kind === 'audio' ? 'audio/wav' : `${kind}/${kind === 'image' ? 'png' : 'mp4'}`,
    media: { durationSeconds }, videoReference: { eligible: true },
  } }
  return { id, data }
}

for (const [kind, max] of [['image', 30], ['video', 10], ['audio', 10]] as const) {
  test(`${kind} 参考数量上限 ${max} 与超限`, () => {
    const refs = Array.from({ length: max }, (_, i) => source(kind, i))
    expect(videoReferencesError(refs, 'canvas')).toBe('')
    expect(videoReferencesError([...refs, source(kind, max)], 'canvas')).toContain(`最多参考 ${max} 项`)
  })
}

test('音频单独引用、空参考、同类总时长与精度边界', () => {
  expect(videoReferencesError([], 'canvas')).toContain('至少 1 项')
  expect(videoReferencesError([source('audio', 0)], 'canvas')).toBe('')
  for (const kind of ['audio', 'video'] as const) {
    expect(videoReferencesError([source(kind, 0, 15), source(kind, 1, 15)], 'canvas')).toBe('')
    expect(videoReferencesError([source(kind, 0, 15), source(kind, 1, 15.0001)], 'canvas')).toContain('总时长不能超过 30 秒')
    for (const duration of [1.999, 30.001, NaN, Infinity]) expect(videoReferencesError([source(kind, 0, duration)], 'canvas')).toContain('2～30 秒')
  }
  expect(videoReferencesError([source('video', 0, 30), source('audio', 0, 30)], 'canvas')).toBe('')
})

test('Seedance 2.0 家族按较低素材数量、单条时长和同类总时长校验', () => {
  const model = 'doubao-seedance-2-0-260128'
  expect(videoReferencesError(Array.from({ length: 9 }, (_, i) => source('image', i)), 'canvas', model)).toBe('')
  expect(videoReferencesError(Array.from({ length: 10 }, (_, i) => source('image', i)), 'canvas', model)).toContain('最多参考 9 项图片')
  expect(videoReferencesError(Array.from({ length: 4 }, (_, i) => source('video', i)), 'canvas', model)).toContain('最多参考 3 项视频')
  expect(videoReferencesError([source('image', 0), ...Array.from({ length: 4 }, (_, i) => source('audio', i))], 'canvas', model)).toContain('最多参考 3 项音频')
  expect(videoReferencesError([source('audio', 0)], 'canvas', model)).toContain('音频参考须搭配')
  expect(videoReferencesError([source('image', 0), source('audio', 0, 16)], 'canvas', model)).toContain('2～15 秒')
  expect(videoReferencesError([source('image', 0), source('video', 0, 8), source('video', 1, 8)], 'canvas', model)).toContain('总时长不能超过 15 秒')
  expect(videoReferencesError([source('image', 0), source('video', 0, 15), source('audio', 0, 15)], 'canvas', model)).toBe('')
})

test('本地预览、服务端校验、归属、重复 Key 和旧元数据均不误报已就绪', () => {
  const ref = source('audio', 0)
  expect(videoReferenceError(ref.data, 'other-canvas', ref.id)).toContain('归属')
  expect(videoReferenceError(ref.data, 'canvas', 'other-node')).toContain('归属')
  expect(videoReferencesError([ref, ref], 'canvas')).toContain('不能重复引用')
  ref.data.asset!.videoReference = { eligible: false, reason: '参考音频不能超过 15 MB' }
  expect(videoReferencesError([ref], 'canvas')).toContain('15 MB')
  ref.data.asset!.media = undefined
  expect(videoReferencesError([ref], 'canvas')).toContain('缺少服务端媒体参数')
  ref.data.storage = { status: 'error' }
  expect(videoReferencesError([ref], 'canvas')).toContain('保存失败')
  ref.data.storage = { status: 'saving' }
  expect(videoReferencesError([ref], 'canvas')).toContain('正在保存')
  ref.data.storage = undefined
  expect(videoReferencesError([ref], 'canvas')).toContain('尚未保存')
})
