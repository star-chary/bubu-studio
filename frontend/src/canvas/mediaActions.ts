import { authFetch } from '../auth/session'
import { previewRetryURL, type MediaNodeData } from './media'

export function mediaSource(data: MediaNodeData): string | undefined {
  return data.url || previewRetryURL(data)
}

const extensions: Record<string, string> = {
  'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/gif': 'gif',
  'image/avif': 'avif', 'image/bmp': 'bmp', 'image/svg+xml': 'svg',
  'video/mp4': 'mp4', 'video/webm': 'webm', 'video/quicktime': 'mov', 'video/ogg': 'ogv',
  'audio/mpeg': 'mp3', 'audio/mp3': 'mp3', 'audio/wav': 'wav', 'audio/x-wav': 'wav', 'audio/wave': 'wav',
}

function downloadName(name: string, contentType: string): string {
  const cleaned = name.replace(/[\u0000-\u001f<>:"/\\|?*]/g, '_').replace(/[. ]+$/g, '').slice(0, 160) || '素材'
  const extension = extensions[contentType]
  if (!extension) return cleaned
  const current = cleaned.match(/\.(png|jpe?g|webp|gif|avif|bmp|svg|mp4|webm|mov|m4v|ogv|ogg|mp3|wav)$/i)
  if (current && (current[1]!.toLowerCase() === extension || (extension === 'jpg' && current[1]!.toLowerCase() === 'jpeg'))) return cleaned
  return `${current ? cleaned.slice(0, -current[0].length) : cleaned}.${extension}`
}

// 下载和预览共用已有素材地址；这里不调用生成、保存或积分接口。
export async function downloadMedia(data: MediaNodeData, signal: AbortSignal): Promise<void> {
  const source = mediaSource(data)
  if (!source) throw new Error('当前节点还没有可下载的素材。')
  const url = new URL(source, location.href)
  if (!['http:', 'https:', 'blob:', 'data:'].includes(url.protocol)) throw new Error('素材地址不可用。')
  // 私有素材沿用登录鉴权；外部供应商地址不携带应用的身份头或 Cookie。
  const response = url.origin === location.origin && ['http:', 'https:'].includes(url.protocol)
    ? await authFetch(url.href, { signal })
    : await fetch(url.href, { signal, credentials: 'omit' })
  if (!response.ok) throw new Error(`素材下载失败（${response.status}），请重试。`)
  const blob = await response.blob()
  signal.throwIfAborted()
  const contentType = blob.type.split(';')[0]!.toLowerCase()
  if (!blob.size || (contentType && contentType !== 'application/octet-stream' && !contentType.startsWith(`${data.kind}/`))) {
    throw new Error('素材响应不是有效的媒体文件，请重试。')
  }
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = downloadName(data.name, contentType && contentType !== 'application/octet-stream' ? contentType : data.asset?.contentType || '')
  document.body.append(link)
  link.click()
  link.remove()
  // 给浏览器留出接管下载的时间，再释放本次读取的文件内存。
  setTimeout(() => URL.revokeObjectURL(objectURL), 30_000)
}
