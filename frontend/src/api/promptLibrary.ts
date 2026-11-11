export interface PromptImage { id: string; url: string }
export interface LibraryPrompt { id: string; title: string; content: string; images: PromptImage[]; createdAt: string; updatedAt: string }

export async function getPrompts(query: string, offset: number, signal: AbortSignal) {
  const params = new URLSearchParams({ q: query, offset: String(offset), limit: '12' })
  const response = await fetch(`/api/prompts?${params}`, { credentials: 'omit', cache: 'no-store', signal })
  const body = await response.json().catch(() => null)
  if (!response.ok || !body || !Array.isArray(body.prompts)) throw new Error(body?.error?.message || '提示词加载失败，请重试。')
  return body as { prompts: LibraryPrompt[]; hasMore: boolean }
}
