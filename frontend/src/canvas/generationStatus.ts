export function generationStatusLabel(task: { taskStatus?: string; providerStatus?: string } | undefined, mediaLabel = '') {
  switch (task?.taskStatus) {
    case 'queued': return '等待执行…'
    case 'preparing': return '准备素材…'
    case 'submitting': return '正在提交…'
    case 'saving': return '保存中…'
  }
  return task?.providerStatus === 'queued' ? '模型排队中…' : `${mediaLabel}生成中…`
}
