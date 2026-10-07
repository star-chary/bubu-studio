import { authFetch } from '../auth/session'
export interface AssetScope { canvasId: string; nodeId: string }
export interface StoredAsset extends AssetScope {
  key: string
  url: string
  kind: 'images' | 'videos' | 'audios'
  contentType: string
  bytes: number
  source: 'uploads' | 'generated'
  media?: { width?: number; height?: number; durationSeconds?: number; frameRate?: number; videoCodec?: string; audioCodec?: string }
  videoReference?: { eligible: boolean; reason?: string }
}

interface StorageConfig { enabled: boolean; maxImageBytes: number; maxVideoBytes: number; maxAudioBytes: number }
let configRequest: Promise<StorageConfig> | undefined

export function getStorageConfig(): Promise<StorageConfig> {
  if (!configRequest) configRequest = authFetch('/api/storage/config', { cache: 'no-store' }).then(async (response) => {
    const body = await response.json()
    if (!response.ok || typeof body.enabled !== 'boolean') throw new Error('无法读取保存服务配置')
    return body as StorageConfig
  }).catch(() => {
    configRequest = undefined
    throw new Error('无法连接保存服务，文件仍可本地预览，请稍后重新保存。')
  })
  return configRequest
}

export function isStoredAsset(value: unknown): value is StoredAsset {
  const asset = value as Partial<StoredAsset> | null
  return !!asset && typeof asset.key === 'string' && typeof asset.url === 'string' && asset.url === `/api/assets/content?key=${encodeURIComponent(asset.key)}`
    && typeof asset.bytes === 'number' && asset.bytes > 0 && ['images', 'videos', 'audios'].includes(asset.kind ?? '')
    && ['uploads', 'generated'].includes(asset.source ?? '') && typeof asset.contentType === 'string'
    && typeof asset.canvasId === 'string' && typeof asset.nodeId === 'string'
}

export async function uploadAsset(file: File, scope: AssetScope, signal: AbortSignal): Promise<StoredAsset> {
  let response: Response
  try {
    response = await authFetch(`/api/assets?${new URLSearchParams({ ...scope })}`, {
      method: 'POST', headers: { 'Content-Type': 'application/octet-stream' }, body: file, signal,
    })
  } catch (error) {
    if (signal.aborted) throw error
    throw new Error('文件保存中断，仍可本地预览，请重新保存。')
  }
  const body = await response.json().catch(() => null)
  if (!response.ok) throw new Error(body?.error?.message || '文件保存失败，请稍后重试。')
  if (!isStoredAsset(body) || body.canvasId !== scope.canvasId || body.nodeId !== scope.nodeId) throw new Error('保存服务未返回正确的文件信息。')
  return body
}
