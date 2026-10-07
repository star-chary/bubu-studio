import { auth } from '../auth/session'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useVueFlow } from '@vue-flow/core'
import { getTask, PersistenceError, persistenceRequest, writeJSON, type CanvasSnapshot, type GenerationTask, type SavedCanvas } from '../api/persistence'
import type { MediaNodeData } from './media'
import { refreshCredits } from '../api/credits'

const active = (task: GenerationTask) => ['queued', 'preparing', 'submitting', 'running', 'saving'].includes(task.status)
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

export function useCanvasSession(flowId: string, requestedId: string, temporary: boolean, applyTask: (task: GenerationTask, force?: boolean) => void) {
  const { nodes, viewport, setNodes, setViewport } = useVueFlow(flowId)
  const canvasId = ref(requestedId)
  const title = ref(temporary ? '临时画布' : '画布')
  const enabled = ref(false)
  const ready = ref(false)
  const loadError = ref('')
  const saveError = ref('')
  const trackingError = ref('')
  const saving = ref(false)
  const dirty = ref(false)
  const conflict = ref(false)
  const historyOpen = ref(false)
  const history = ref<GenerationTask[]>([])
  const hasMore = ref(false)
  const historyLoading = ref(false)
  let version = 0
  let saved = ''
  let disposed = false
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let inflight: Promise<void> | undefined
  const pendingTasks = new Set<string>()
  const taskCreditStates = new Map<string, GenerationTask['creditStatus']>()

  function snapshot(): CanvasSnapshot {
    return {
      nodes: nodes.value.map(({ id, position, data }) => ({ id, position: { x: position.x, y: position.y }, data: {
        kind: data.kind, name: data.name, prompt: data.prompt ?? '', imageModel: data.imageModel,
        promptParts: data.promptParts, videoModel: data.videoModel, videoMode: data.videoMode, videoOptions: data.videoOptions,
        origin: data.origin, referenceIds: [...(data.referenceIds ?? [])],
      } })),
      viewport: { ...viewport.value },
    }
  }
  const serial = computed(() => JSON.stringify(snapshot()))
  const saveLabel = computed(() => !ready.value ? '正在恢复画布…' : !enabled.value ? '临时画布' : conflict.value ? '保存冲突' : saveError.value ? '保存失败' : saving.value ? '正在保存…' : dirty.value ? '尚未保存' : '画布已保存')

  async function flush() {
    if (auth.status !== 'authenticated') throw new Error('请重新登录后保存画布。')
    if (!ready.value) throw new Error('画布尚未恢复，请稍后再试。')
    if (!enabled.value) return
    if (conflict.value) throw new Error(saveError.value)
    if (saveTimer) clearTimeout(saveTimer)
    if (inflight) { await inflight; if (serial.value !== saved) return flush(); return }
    if (serial.value === saved) { dirty.value = false; saveError.value = ''; return }
    inflight = (async () => {
      saving.value = true
      try {
        // Serialize saves and drain edits made during an in-flight save.
        while (!disposed && auth.status === 'authenticated' && serial.value !== saved) {
          const value = serial.value
          const response = await writeJSON<{ version: number }>(`/api/canvases/${canvasId.value}`, { version, snapshot: JSON.parse(value) }, 'PUT')
          version = response.version
          saved = value
        }
        dirty.value = serial.value !== saved
        saveError.value = ''
      } catch (error) {
        saveError.value = error instanceof Error ? error.message : '画布保存失败。'
        if (error instanceof PersistenceError && error.code === 'VERSION_CONFLICT') conflict.value = true
        throw error
      } finally { saving.value = false; inflight = undefined }
    })()
    return inflight
  }

  watch(serial, () => {
    if (!ready.value || !enabled.value || disposed || auth.status !== 'authenticated') return
    dirty.value = serial.value !== saved
    if (!dirty.value || conflict.value) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => { void flush().catch(() => {}) }, 600)
  })

  function track(task: GenerationTask, force = false) {
    if (active(task)) pendingTasks.add(task.id); else pendingTasks.delete(task.id)
    const previousCreditState = taskCreditStates.get(task.id)
    if (task.creditStatus && previousCreditState && previousCreditState !== task.creditStatus) void refreshCredits()
    if (task.creditStatus) taskCreditStates.set(task.id, task.creditStatus)
    applyTask(task, force)
  }
  async function loadHistory(append = false) {
    if (!enabled.value || historyLoading.value || auth.status !== 'authenticated') return
    historyLoading.value = true
    try {
      const offset = append ? history.value.length : 0
      const response = await persistenceRequest<{ tasks: GenerationTask[]; hasMore: boolean }>(`/api/canvases/${canvasId.value}/tasks?limit=20&offset=${offset}`)
      history.value = append ? [...history.value, ...response.tasks.filter((t) => !history.value.some((old) => old.id === t.id))] : response.tasks
      hasMore.value = response.hasMore
      trackingError.value = ''
    } catch { trackingError.value = '暂时无法查询任务，恢复连接后会继续查询。' }
    finally { historyLoading.value = false }
  }
  watch(historyOpen, (open) => { if (open) void loadHistory() })

  async function poll() {
    if (disposed || !enabled.value || !ready.value || auth.status !== 'authenticated') return
    try {
      const latest = await persistenceRequest<{ tasks: GenerationTask[] }>(`/api/canvases/${canvasId.value}/tasks?latest=true`)
      if (disposed) return
      latest.tasks.forEach((task) => track(task))
      // Also reconcile a submission whose response was lost.
      for (const id of pendingTasks) {
        if (latest.tasks.some((t) => t.id === id)) continue
        try { track(await getTask(id)) } catch (error) { if (!(error instanceof PersistenceError && error.status === 404)) throw error }
      }
      history.value = history.value.map((task) => latest.tasks.find((t) => t.id === task.id) ?? task)
      if (historyOpen.value && latest.tasks.some((t) => !history.value.some((old) => old.id === t.id))) await loadHistory()
      trackingError.value = ''
    } catch { trackingError.value = '暂时无法查询任务，恢复连接后会继续查询；请勿重复生成。' }
    finally { if (!disposed && auth.status === 'authenticated') pollTimer = setTimeout(() => { void poll() }, 2000) }
  }

  async function initialize() {
    loadError.value = ''
    try {
      if (!uuid.test(requestedId)) throw new Error('画布地址无效，请返回首页选择画布。')
      if (temporary) { ready.value = true; return }
      const config = await persistenceRequest<{ enabled: boolean }>('/api/persistence/config')
      if (typeof config.enabled !== 'boolean') throw new Error('保存服务配置无效。')
      enabled.value = config.enabled
      if (!enabled.value) throw new Error('画布保存服务未启用，请启动数据库后重新连接。')
      const canvas = await persistenceRequest<SavedCanvas>(`/api/canvases/${canvasId.value}`)
      if (disposed) return
      title.value = canvas.title || '未命名画布'
      version = canvas.version
      setNodes(canvas.snapshot.nodes.map((node) => {
        const asset = canvas.assets.find((a) => a.nodeId === node.id)
        const result = canvas.results.find((t) => t.input.nodeId === node.id)?.result
        const mediaURL = result?.url ?? asset?.url
        const data: MediaNodeData = { ...node.data, status: mediaURL ? 'loading' : node.data.origin === 'upload' ? 'error' : 'empty' }
        if (mediaURL) {
          data.url = mediaURL
          // A newer temporary result must not inherit an older image's asset.
          // Otherwise the preview and the reference key would identify different images.
          data.asset = result ? result.asset : asset
          data.origin = result ? 'generated' : asset?.source === 'uploads' ? 'upload' : 'generated'
          data.storage = data.asset ? { status: 'saved' } : result?.storageError ? { status: 'error', error: result.storageError } : undefined
        } else if (data.origin === 'upload') data.error = '此本地文件尚未上传成功，请重新上传。'
        return { ...node, type: 'media', data, connectable: false, ariaLabel: data.name }
      }))
      canvas.tasks.forEach((task) => track(task, true))
      await nextTick()
      await setViewport(canvas.snapshot.viewport)
      saved = serial.value
      ready.value = true
      void poll()
    } catch (error) { loadError.value = error instanceof Error ? error.message : '无法恢复画布，请确认后端与数据库已启动。' }
  }
  watch(() => auth.status, (status) => {
    if (saveTimer) clearTimeout(saveTimer)
    if (pollTimer) clearTimeout(pollTimer)
    if (status === 'authenticated' && ready.value && !disposed) { void flush().catch(() => {}); void poll() }
  })
  function beforeUnload(event: BeforeUnloadEvent) {
    if (enabled.value && (dirty.value || saving.value)) { event.preventDefault(); event.returnValue = '' }
  }
  function flushOnHide() { if (document.hidden && ready.value && dirty.value) void flush().catch(() => {}) }
  onMounted(() => { void initialize(); window.addEventListener('beforeunload', beforeUnload); document.addEventListener('visibilitychange', flushOnHide) })
  onBeforeUnmount(() => {
    disposed = true
    if (saveTimer) clearTimeout(saveTimer)
    if (pollTimer) clearTimeout(pollTimer)
    window.removeEventListener('beforeunload', beforeUnload)
    document.removeEventListener('visibilitychange', flushOnHide)
  })
  return { canvasId, title, enabled, ready, loadError, saveError, trackingError, saveLabel, saving, dirty, conflict, historyOpen, history, hasMore, historyLoading, initialize, flush, track, loadHistory }
}
