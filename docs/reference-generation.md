# 提示词引用与视频生成：第二阶段

2026-10-04 UI 更新：视频节点已采用模型 / 生成类型菜单与参数弹层，全能参考按当前节点的素材校验结果启用；文生视频暂未接入，生成按钮及本页接口契约不变。详见 [视频提示词参数 UI](./video-prompt-controls.md)。

2026-10-03。图片和视频节点的结构化 `@` 标签已接入生成；菜单仍只展示图片、视频。已有音频可通过“参考”按钮加入视频输入，不新增音频标签菜单。

## 操作与实际数据流

在图片或视频提示词里输入 `@`，选择当前节点已引用的素材，或展开“素材引用 → 图片 / 视频”选择本画布资源。选择会同步参考缩略图和连线；引用失效时，生成按钮禁用并提示修复。图片仅接收图片参考。视频至少选择一份合格参考，允许只有音频和空提示词。

```text
编辑器 promptParts（文字 + 稳定 nodeId）与 referenceIds
→ 点击生成：复制提示词、参数、有序素材 Key
→ 保存画布 → POST 本项目生成接口
→ 校验数据库资源归属和类型，解析标签
→ PostgreSQL 保存不可变 input、bindings、compiledPrompt
→ 后台读取真实素材、检查媒体、签发临时读取地址
→ 方舟模型 → 后台保存结果 → OSS
→ 数据库事务登记资源并完成任务
→ 浏览器轮询本项目任务接口，结果回填原节点
```

编号以附件数组顺序为准，图片、视频、音频分别计数。例如附件顺序 image、video、image，对应图片1、视频1、图片2。重复提及同一个节点使用相同编号，不重复添加附件。普通文本里的 `@名字` 不会被当作标签解析；不向第三方发送 HTML、节点 UUID、blob URL。草稿后来重命名、修改参考或重新生成来源素材，都不会更改已建立任务的输入 Key。

## 接口与数据库

- 图片仍使用 `POST /api/images/generations`，增加可选 `promptParts`，按既有 `referenceKeys` 顺序绑定资源；请求体上限调整为 64 KiB。普通文本兼容原流程；结构化标签需要数据库模式。
- 新增 `POST /api/videos/generations`：只接 reference 模式，固定 Seedance 2.5；默认 4 秒 / 480p / 16:9 / 有声，可选 4～30 秒、720p 和支持的比例。前端保存每个视频节点的 `videoOptions`。
- 视频使用 `references:[{nodeId,assetKey,kind}]` 和可选 `promptParts`，默认值规范化后再比较任务输入。同 taskId 同输入返回原任务；不同输入返回 409；视频活动任务返回 202，终态返回 200。
- 复用 `GET /api/tasks/:taskId`、画布任务列表与刷新恢复接口。任务增加 `kind`、`compiledPrompt`、`bindings`、供应商任务 ID / 状态和查询错误。前端按任务 ID 和 updatedAt 接收更新，供应商进度变化不要求本地 status 同时变化。
- 新增 `POST /api/tasks/:taskId/storage-retries`，提交 `{}`；对 storage_failed 视频原子切回 saving；saving / succeeded 重复调用直接返回原任务。不可恢复或超过 48 小时窗口返回 410，禁止生成新任务。

迁移 `002_video_tasks.sql` 扩展现有 `generation_tasks`，增加任务类型、编译提示词、绑定快照、供应商 ID / 状态 / 内部结果地址、轮询错误；扩展状态和活动任务唯一索引。**没有新增业务表、Redis、MQ 或中间件。** 旧任务默认 kind=image，旧提示词继续可用。执行本机迁移前已备份数据库到 `.data/frame-space-before-generation-step2.dump`。

## 异步任务与恢复

视频状态：`queued → preparing → submitting → running → saving → succeeded`。2026-10-05 新增 preparing 与并发持久化队列，见 [生成队列](./generation-queue.md)。

- queued：检查真实文件并签名；不合格时 failed，不调用模型。
- submitting：发起供应商 POST 前先落库。超时、5xx、响应损坏或服务在提交期间中断，转 interrupted / SUBMISSION_UNKNOWN；可能已受理，不自动再提交。
- running：保存 providerTaskId 后只查询该 ID。查询失败保留 running，并退避重查；默认 10 秒，普通失败上限 60 秒，供应商 Retry-After 可延长至 10 分钟。超过项目 48 小时查询窗口仍不可确认则 interrupted。
- saving：供应商成功结果和内部下载 URL 已落库；重启只恢复下载转存。
- storage_failed：保留生成结果，用户“重试保存视频”复用原任务；不会重复生成或改变计费请求。

图片和视频使用独立的持久化执行队列，默认视频并发 3、图片并发 1、结果转存并发 2；数据库唯一索引仅限制同节点一个未完成任务。图片保留原有临时结果 / storageError 语义；视频只有转存、校验并登记成功才返回本项目播放 URL。查询和下载地址不会作为长期地址写入前端快照。任务完成与 assets 登记使用同一个数据库事务；OSS 不在事务内，极端崩溃窗口仍可能留下孤立对象，目前不自动清理。

## 素材与供应商适配

图片沿用 Seedream 的参考规则和 10 分钟读取签名。视频后台从 OSS 重新读取实际内容，用现有探测模块核对格式、宽高、编码、帧率和精确时长，再检查总数量 / 总时长；规则见 [视频契约](./video-generation-contract.md)。预检文件在本机后端临时目录，完成后删除。

视频参考签名为 2 小时，覆盖设置的供应商 1 小时执行窗口。供应商创建请求只发一次，超时 60 秒；查询超时 15 秒；结果转存最长 300 秒。结果下载继续使用已有 HTTPS 公网地址校验和禁重定向下载器，检查真实 MP4 / H.264 / 可选 AAC 后转存私有 OSS；同源播放支持 Range。

协议核对依据：[方舟创建视频任务](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)、[官方 Go SDK 字段](https://github.com/volcengine/volcengine-go-sdk/blob/master/service/arkruntime/model/content_generation.go)、[Seedance 2.5 提示词指南](https://docs.volcengine.com/docs/ark/seedance-2-5-prompt-guide?lang=zh)，以及用户提供的创建视频任务 PDF。供应商适配集中在 `ark/videos.go`，前端不持有密钥。

## 已验证范围

- 前端类型检查与构建通过；57 项 Chrome 浏览器回归全部通过。新增覆盖带标签图片附件顺序、视频参数、同状态更新、刷新恢复、保存重试；保留旧图片生成、标签编辑、音视频播放和画布回归。
- Go 全量测试与 vet 通过，设置独立真实 PostgreSQL 测试库。覆盖标签编译、错误绑定、默认值 / false / null、供应商请求结构、空提示词音频、未知提交不重发、重启恢复 ID / 保存阶段、重复保存请求、2 小时签名、坏文件和低规格参考在签名前拒绝。
- 真实 Lite 图片引用已生成并存入 OSS：同一标签提及两次，只发送一份附件，任务 `e4f4dee0-203d-4d68-ab42-5c2a9722a894`。真实 Seedance 2.5 视频也已生成、转存、刷新播放并验证 Range 206：任务 `b4087d45-917f-435e-809a-d198850a841a`，输入图片 / 视频 / 音频各一份，输出 854×480、有声，实际文件时长 4.064 秒（请求 4 秒），页面运行错误 0。验收画布 `17d05bb2-2b17-4d9d-b198-152c59959395`。本次每种媒体只创建一次付费生成，浏览器中途点击被面板挡住后用已有任务记录续验，未重复生成图片。
- 验收脚本 `frontend/scripts/verify-live-reference-generation.mjs` 默认禁止真实调用；显式开启后每种媒体最多提交一次。中途浏览器操作失败可用 `RESUME_REFERENCE_GENERATION=1` 读取已有记录继续查询，不重新生成已有任务。证据写入 `frontend/artifacts/reference-generation-live.json` / `.png`。

项目仍为本机单人工作台，无用户登录、跨用户资源鉴权或自动依赖调度。UUID 归属检查不能替代用户权限；Pro、720p、长视频、真人素材和不同参考组合的生成效果不因本次最小验收而视为已验证。
