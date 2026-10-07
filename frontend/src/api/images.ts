import type { StoredAsset } from './assets'

export interface GeneratedImage {
  url: string
  model: string
  size?: string
  asset?: StoredAsset
  storageError?: string
}
