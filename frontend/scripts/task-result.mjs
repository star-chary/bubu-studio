// Used only by explicitly enabled live probes. Polling never submits a task.
export async function waitForTaskResult(request, baseURL, task) {
  const deadline = Date.now() + 330_000
  while (Date.now() < deadline) {
    if (task.status === 'succeeded' && task.result) return task.result
    if (['failed', 'interrupted'].includes(task.status)) throw new Error(`任务 ${task.id}: ${task.error?.code} / ${task.error?.message}`)
    await new Promise((resolve) => setTimeout(resolve, 2000))
    const response = await request.get(`${baseURL}/api/tasks/${task.id}`)
    if (!response.ok()) throw new Error(`任务查询失败，保留任务 ID ${task.id}，不要重新提交生成。`)
    task = await response.json()
  }
  throw new Error(`查询等待超时，任务 ID ${task.id}，未重新提交生成。`)
}
