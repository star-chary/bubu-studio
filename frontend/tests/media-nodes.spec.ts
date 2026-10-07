import type { Page } from '@playwright/test'
import { expect, test } from './setup'
import { fileURLToPath } from 'node:url'

const imagePath = fileURLToPath(new URL('./fixtures/sample-image.png', import.meta.url))
const videoPath = fileURLToPath(new URL('./fixtures/sample-video.mp4', import.meta.url))

async function openUpload(page: Page, x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('menuitem', { name: '上传', exact: true }).click()
  return chooser
}

async function addEmptyNode(page: Page, name: '图片' | '视频', x: number, y: number) {
  await page.mouse.click(x, y, { button: 'right' })
  await page.getByRole('menuitem', { name: '添加节点', exact: true }).click()
  await page.getByRole('menuitem', { name, exact: true }).click()
}

test('创建两种空节点，缩放和平移后落点正确，节点可独立拖动', async ({ page }, testInfo) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  await page.mouse.move(300, 180)
  await page.mouse.down()
  await page.mouse.move(396, 228, { steps: 8 })
  await page.mouse.up()
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')

  await addEmptyNode(page, '图片', 90, 180)
  const image = page.getByRole('article', { name: '图片节点 1', exact: true })
  await expect(image.getByLabel('空图片节点')).toBeVisible()
  const box = await image.boundingBox()
  expect(box!.x).toBeCloseTo(90, 1)
  expect(box!.y).toBeCloseTo(180, 1)
  await addEmptyNode(page, '视频', 790, 180)
  await expect(page.getByRole('article', { name: '视频节点 2', exact: true }).getByLabel('空视频节点')).toBeVisible()
  await expect(page.locator('.empty-guide')).toHaveCount(0)
  await expect(page.locator('.media-node button, .media-node input, .media-node textarea')).toHaveCount(0)
  // 引用连线有定位端点，但不允许手动拖线，未引用时端点隐藏。
  await expect(page.locator('.vue-flow__handle.connectable')).toHaveCount(0)
  await expect(page.locator('.reference-handle.is-visible')).toHaveCount(0)

  await page.mouse.move(180, 260)
  await page.mouse.down()
  await page.mouse.move(270, 320, { steps: 10 })
  await page.mouse.up()
  await expect.poll(async () => Math.round((await image.boundingBox())!.x)).toBe(180)
  expect((await image.boundingBox())!.y).toBeCloseTo(240, 1)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
  await page.screenshot({ path: testInfo.outputPath('empty-media-nodes.png') })
})

test('一次上传图片和视频，显示预览与尺寸，播放视频不拖动画布，可重复上传', async ({ page }, testInfo) => {
  const errors: string[] = []
  const writes: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    if (['POST', 'PUT', 'PATCH'].includes(request.method())) writes.push(request.url())
  })
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  const chooser = await openUpload(page, 100, 180)
  expect(chooser.isMultiple()).toBe(true)
  await chooser.setFiles([imagePath, videoPath])
  await expect(page.locator('.vue-flow__node')).toHaveCount(2)
  const image = page.getByRole('article', { name: 'sample-image.png', exact: true })
  const videoNode = page.getByRole('article', { name: 'sample-video.mp4', exact: true })
  await expect(image).toContainText('960 × 540')
  await expect(videoNode).toContainText('320 × 240')
  await expect(image.getByRole('img')).toBeVisible()
  await expect(image.getByRole('img')).toHaveAttribute('src', /^blob:/)
  const video = videoNode.locator('video')
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  await expect(page.locator('.media-node').getByRole('status')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('uploaded-media-nodes.png') })

  const before = await videoNode.boundingBox()
  const viewportBefore = await page.locator('.vue-flow__transformationpane').getAttribute('style')
  await videoNode.getByRole('button', { name: '播放视频', exact: true }).click()
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false)
  await videoNode.getByRole('button', { name: '暂停视频', exact: true }).click()
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  expect(await videoNode.boundingBox()).toEqual(before)
  expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewportBefore)

  // 标题仍能拖动，播放按钮不会移动节点。
  await page.mouse.move(before!.x + 90, before!.y + 12)
  await page.mouse.down()
  await page.mouse.move(before!.x + 140, before!.y + 52, { steps: 8 })
  await page.mouse.up()
  await expect.poll(async () => Math.round((await videoNode.boundingBox())!.x - before!.x)).toBe(50)

  const repeatChooser = await openUpload(page, 100, 550)
  await repeatChooser.setFiles(imagePath)
  await expect(page.getByRole('article', { name: 'sample-image.png', exact: true })).toHaveCount(2)
  expect(writes).toEqual([])
  expect(errors).toEqual([])
  await page.reload()
  await expect(page.locator('.vue-flow__node')).toHaveCount(0)
})

test('视频画面点击不播放，暂停和播放时均可拖动，只有播放按钮控制播放', async ({ page }, testInfo) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  const chooser = await openUpload(page, 200, 160)
  await chooser.setFiles(videoPath)
  const node = page.getByRole('article', { name: 'sample-video.mp4', exact: true })
  const video = node.locator('video')
  await expect(node.getByRole('button', { name: '播放视频', exact: true })).toBeVisible()
  const viewport = await page.locator('.vue-flow__transformationpane').getAttribute('style')

  async function dragPicture(dx: number, dy: number) {
    const before = (await node.boundingBox())!
    const picture = (await video.boundingBox())!
    const start = { x: picture.x + picture.width / 2, y: picture.y + picture.height / 3 }
    await page.mouse.move(start.x, start.y)
    await page.mouse.down()
    await page.mouse.move(start.x + dx, start.y + dy, { steps: 8 })
    await page.mouse.up()
    await expect.poll(async () => Math.round((await node.boundingBox())!.x - before.x)).toBe(dx)
    expect((await node.boundingBox())!.y - before.y).toBeCloseTo(dy, 1)
    expect(await page.locator('.vue-flow__transformationpane').getAttribute('style')).toBe(viewport)
  }

  const picture = (await video.boundingBox())!
  await page.mouse.click(picture.x + picture.width / 2, picture.y + picture.height / 3)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  await page.mouse.dblclick(picture.x + picture.width / 2, picture.y + picture.height / 3)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  await dragPicture(80, 50)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)

  const beforePlay = await node.boundingBox()
  await node.getByRole('button', { name: '播放视频', exact: true }).click()
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false)
  expect(await node.boundingBox()).toEqual(beforePlay)
  await dragPicture(40, 25)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false)
  await node.getByRole('button', { name: '暂停视频', exact: true }).click()
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)

  // 调整进度不播放、不拖动；按钮可通过键盘播放和暂停。
  const beforeSeek = await node.boundingBox()
  const progress = node.getByRole('slider', { name: '视频进度' })
  await progress.fill('1')
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeCloseTo(1, 1)
  expect(await video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  expect(await node.boundingBox()).toEqual(beforeSeek)
  await node.getByRole('button', { name: '播放视频', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false)
  await page.keyboard.press('Space')
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('video-drag-and-play.png') })
})

test('混合文件中跳过非媒体和空文件，损坏媒体有错误提示，正常文件继续显示', async ({ page }) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  const chooser = await openUpload(page, 90, 160)
  await chooser.setFiles([
    { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('not a media file') },
    { name: 'empty.mp4', mimeType: 'video/mp4', buffer: Buffer.alloc(0) },
    { name: 'broken.png', mimeType: 'image/png', buffer: Buffer.from('invalid image') },
    { name: 'valid.svg', mimeType: 'image/svg+xml', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="teal"/></svg>') },
  ])
  await expect(page.locator('.vue-flow__node')).toHaveCount(2)
  await expect(page.getByRole('alert').filter({ hasText: '已跳过 2 个文件' })).toBeVisible()
  const broken = page.getByRole('article', { name: 'broken.png', exact: true })
  await expect(broken.getByRole('alert')).toContainText('无法预览此文件')
  await expect(broken.locator('img')).toHaveCount(0)
  await expect(page.getByRole('article', { name: 'valid.svg', exact: true })).toContainText('640 × 360')
  await expect(page.locator('.media-node').getByRole('status')).toHaveCount(0)
})

test('取消选择不创建节点，缩放后的本地上传仍在右键位置出现', async ({ page }) => {
  await page.goto('/?temporary=33333333-3333-4333-8333-333333333333')
  const canceled = await openUpload(page, 300, 180)
  await canceled.setFiles([])
  await expect(page.locator('.vue-flow__node')).toHaveCount(0)
  await expect(page.getByRole('menu')).toHaveCount(0)
  await page.getByRole('button', { name: '放大画布', exact: true }).click()
  await page.mouse.move(200, 140)
  await page.mouse.down()
  await page.mouse.move(250, 190, { steps: 5 })
  await page.mouse.up()
  const chooser = await openUpload(page, 400, 250)
  await chooser.setFiles(imagePath)
  const image = page.getByRole('article', { name: 'sample-image.png', exact: true })
  await expect(image).toContainText('960 × 540')
  const box = await image.boundingBox()
  expect(box!.x).toBeCloseTo(400, 1)
  expect(box!.y).toBeCloseTo(250, 1)
})
