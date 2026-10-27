# APIMart Seedance 2.5 视频生成

本文保留2026-10-25首个型号接入与双任务验收记录。2026-10-26已扩展统一功能菜单、更多视频型号及单图起始画面；最新实现与验收边界见 [视频型号与功能接入](./apimart-video-models.md)。尚未发布线上。

## 开放范围

- 供应商 `apimart`，型号 `seedance-2.5`。
- 文生视频：提示词必填，不提交参考素材。
- 全能参考：提示词必填，1—30张已保存的普通参考图片。图片用于内容参考，不设置首帧或尾帧。
- 比例、480p / 720p / 1080p、4—30秒或 `-1` 自动时长、生成音频开关沿用静态基础参数，固定请求 MP4。
- 其他 APIMart 型号仍可编辑并保存参数，生成入口继续关闭。视频、音频参考及首尾帧、编辑、续接不在本次范围。
- 此链路不请求或冻结平台积分；任务沿用 `credit_status=legacy`，不会产生平台积分消费流水。原有方舟报价、积分和生成流程继续使用原入口。

## 实际流程与状态

```text
前端静态选择与基础参数 → 节点 modelSelection / videoMode
  → 输入校验与画布保存 → POST /api/videos/generations
  → 验证 Session、CSRF、画布归属、保存的供应商与型号
  → generation_tasks：固定 provider、型号、参数、提示词与素材绑定
  → 后台 preparing：核验参考素材并签发现有 OSS 地址
  → APIMart POST /v1/videos/generations：仅提交一次
  → 保存 provider_task_id → GET /v1/tasks/{id}
  → completed 转换为平台成功结果 → saving 转存视频
  → 视频元数据与素材落库 → 平台 succeeded
  → 前端查询平台任务 → 原视频节点回填、播放及刷新恢复
```

继续使用 `generation_tasks`、`assets` 和 `canvases.snapshot`，没有新数据库表或迁移。供应商记录在任务已有的输入 JSON 中；切换画布当前模型不会更改已接收任务的供应商。

后台生成接口继续复用现有 `VideoGenerator` 契约。`cmd/server/video_providers.go` 按任务供应商选择客户端；没有相应客户端时不会回退到方舟。旧任务未保存 `provider` 时按原方舟处理。

APIMart `pending / processing / completed / failed / cancelled` 转换为既有任务进度。前端只读取平台结果，不直接访问 APIMart 或持有密钥。

## 请求示例

`POST /api/videos/generations`，须带现有登录 Cookie、CSRF 和用户绑定头：

```json
{
  "taskId": "11111111-1111-4111-8111-111111111111",
  "canvasId": "22222222-2222-4222-8222-222222222222",
  "nodeId": "33333333-3333-4333-8333-333333333333",
  "provider": "apimart",
  "model": "seedance-2.5",
  "mode": "text",
  "prompt": "An origami paper boat drifts gently on calm water.",
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "generateAudio": true
}
```

参考模式将 `mode` 改为 `reference`，增加现有 `references: [{nodeId, assetKey, kind: "image"}]`。客户端不提交任意素材 URL；服务端从归属正确的已保存素材生成地址。原有图片预检（尺寸、格式、大小）继续执行。

适配层把 `ratio` 转为上游 `size`，图片放入 `image_urls` 并明确 `omni_reference_task_type=reference`。不发送 Ark `content` 或首尾帧字段。提示词素材标签按实际附件顺序编译为 `@图片N`，普通文本不作替换。隐藏的历史高级参数保留在草稿中，不直接转发给供应商。

同一 `taskId` 和相同输入返回原任务；不同输入返回冲突。供应商超时、5xx 或无可靠任务ID的响应保留为 `SUBMISSION_UNKNOWN`，不自动重新生成。查询失败仅继续查询原任务。生成已完成但保存失败时，沿用原任务的重试保存入口。

## 代码职责与配置

- `backend/internal/apimart/videos.go`：上游提交、查询、状态与错误转换；不记录密钥、提示词或签名素材地址。
- `backend/internal/persistence/apimart_video.go`：已开放型号与供应商检查、画布归属、无需平台积分的任务创建。
- `backend/internal/persistence/queue.go` / `video_worker.go`：供应商路由、持久化进度、既有素材转存。
- `frontend/src/models/apimartVideo.ts`：纯函数，从现有草稿和静态基础参数生成提交快照。
- `VideoGenerationControls.vue`：模式与参数展示，通过事件请求修改，不直接修改节点数据。
- `useMediaNodes.ts`：节点提交与任务回填；`useCanvasSession.ts`：既有保存与查询生命周期。
- `NodePromptPanel.vue`：派生生成按钮状态；APIMart 不依赖方舟积分报价状态。

后端 `.env.local` 或环境变量配置 `APIMART_API_KEY`。密钥不进入前端构建。模型清单和参数不发起网络请求。

## 验证记录

- Go 全包测试使用独立 `frame_space_test` 数据库，包含供应商请求格式、状态转换、提交不确定不重发、归属、幂等、供应商快照、积分隔离；`go vet ./...` 与后端构建通过。
- 前端类型检查与生产构建通过。38项浏览器回归全部通过，覆盖文生、图片参考、结果回填与播放、刷新恢复、积分服务不可用时的中转站生成，以及原有方舟参数、积分和素材引用行为。
- 真实验收仅提交两个5秒 / 720p任务，均完成生成、OSS转存、节点播放和刷新恢复。实际输出均为1280×720、5.056秒、H.264，并带音频。
- 文生平台任务：`f5202165-db30-4a09-be43-e1605432256e`。
- 图片参考平台任务：`5bc3c82c-a2cb-4ef9-9ad2-8f92b4e13699`；使用项目合成测试图片，提示词引用编译为 `@图片1`。
- 验收使用独立合成账号与画布，未上传用户私人图片。
- 数据库复核：两个任务均成功、生成两份视频素材，平台积分消费记录为0，冻结积分为0。最终后端重启后仅续查原任务，两个视频的播放与刷新恢复再次通过，没有追加生成任务。

`frontend/scripts/verify-live-apimart-video.mjs` 需显式设置 `RUN_LIVE_APIMART_VIDEO=1`。已有报告必须设置 `RESUME_LIVE_APIMART_VIDEO=1` 才能续查；脚本拒绝为同一模式新建替代任务。Cookie 状态仅保存在本地验收目录，不写入仓库或打印。

## 文档依据

- [APIMart Seedance 2.5](https://docs.apimart.ai/en/api-reference/videos/seedance-2-5/generation)
- [APIMart 任务查询](https://docs.apimart.ai/en/api-reference/tasks/status)
