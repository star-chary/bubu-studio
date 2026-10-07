# 全模态参考视频：接口契约 v1

2026-10-07 当前实现已扩展四款 Seedance 模型与文生视频，支持按模型的时长、素材限制和积分报价；首版仅开放 480p／720p。当前接口与验收边界见 [视频模型与文生视频](./video-models.md)。下文为历史 v1 设计契约，其中“仅 2.5／仅 reference／无积分”的描述不代表现状。

2026-10-05 更新：已实现 PostgreSQL 持久化并发队列与 preparing 状态，替代早期全站单任务约束。视频默认并发 3，图片独立并发 1，转存独立并发 2；领取、恢复与配置以 [生成队列](./generation-queue.md) 为准。

2026-10-03 更新：视频接口与任务链路已实现，另支持可选 `promptParts`，任务返回 `compiledPrompt` / `bindings`。实际验证、恢复窗口和范围见 [第二阶段记录](./reference-generation.md)。

以下保留 2026-10-02 原设计依据与目标验收要求；其中历史状态：**视频生成 API 仍为设计契约；第三步素材 / 引用能力已实现并验证，见 [第三步记录](./media-references.md)。尚未调用视频生成模型。**

本文件确定后续开发的请求、响应、素材规则和异常边界，不代表现有服务已经提供这些能力。最小样例及验收方法见 [验收样例](./video-generation-acceptance.md)。现有图片任务的实现状态仍以 [图片生成](./image-generation.md) 和 [持久化](./persistence.md) 为准。

## 1. 本次范围与设计选择

| 项目 | 本契约采用的方案 | 原因 / 边界 |
| --- | --- | --- |
| 模型 | `doubao-seedance-2-5-260628` | 用户已确认开通且有额度；尚未通过真实 API 调用验证，不自动换模型 |
| 业务模式 | `mode: "reference"`，基于参考素材生成新视频 | 对应方舟 `omni_reference_task_type=reference` |
| 参考输入 | 图片、视频、音频，至少一份；允许仅音频 | 三类可以混合，也可单独使用；不要求三类齐全 |
| 目标节点 | 已保存的 video 节点，origin 为空或 generated | 上传素材节点不能被生成结果覆盖 |
| 首版输出 | 480p / 720p，MP4，单条视频，保留水印 | 先控制网页播放兼容性；1080p / MOV 尚不纳入本契约 |
| 输出时长 | 4～30 整数秒，默认 4 秒 | 用户要求后续真实验证采用最低正常时长和画质，即 4 秒 / 480p |
| 服务前提 | PostgreSQL 与 OSS 均启用 | 视频任务不提供刷新即丢失的临时模式 |
| 执行方式 | 后台创建、后台查询；前端只查本项目接口 | 沿用 Go / PostgreSQL 执行器，不增加 Redis、MQ 或公网回调 |
| 并发 | 视频默认 3、图片默认 1、结果转存默认 2 | 超出额度的任务入库等待；同节点存在未完成任务才返回 GENERATION_BUSY |

本期不接受 edit、extend、auto、首尾帧、纯文本无参考、Draft、联网搜索或客户端自定义供应商参数。这些是产品范围，不是断言模型不具备相应能力。若扩展编辑 / 延长，需要先单独补充参数契约；尤其 edit 要求 adaptive 比例和 -1 时长。即使显式 reference，供应商仍可能结合提示词判定为其他类型并异步报错，应展示错误，不能自动切换模式重新付费。

## 2. 两段接口与数据流

```text
浏览器：保存来源节点 → 上传素材 → 获得不可变资源 Key
  → 视频节点固定提示词、参数、参考列表和 taskId
  → POST /api/videos/generations
Go：校验结构、画布节点及已登记资源 → 创建本地 queued 任务 → 返回 202
后台：检查真实媒体 → 签发参考读取地址 → 记录 submitting → 向方舟创建任务
  → 持久保存 providerTaskId → 查询方舟 → 保存模型结果 → 转存 OSS
  → 事务登记 assets 并完成本地任务
浏览器：GET /api/tasks/:taskId → 将结果回填原节点
```

模型只接收提示词、输出参数和可读取的素材地址，不理解 Vue Flow 节点或画布连线。连线来自 `referenceIds`；它表达引用关系，不会自动调度依赖节点。

## 3. 创建接口

### `POST /api/videos/generations`（计划新增）

同源 JSON 请求；客户端不发送方舟密钥。请求体上限为 **64 KiB**（项目限制，不是方舟的 64 MB 限制），拒绝未知字段、错误类型、尾随第二个 JSON 值。所有 ID 使用项目已有的小写 UUID 规则。请求与任务查询均返回 `Cache-Control: no-store`。

| 字段 | 类型 / 必填 | 规则 |
| --- | --- | --- |
| `taskId` | string / 是 | 前端在第一次提交前生成；请求响应丢失时复用 |
| `canvasId` | string / 是 | 已持久保存的画布 ID |
| `nodeId` | string / 是 | 上述画布内已保存的目标视频节点 |
| `model` | string / 否 | 默认上述 2.5 模型；本期仅允许该 ID |
| `mode` | string / 否 | 默认且仅支持 reference |
| `prompt` | string / 否 | 省略等于空字符串；去首尾空白后 0～2000 个 Unicode 码点 |
| `references` | array / 是 | 有序列表，至少 1 项；全局最多 50 项，另按类型限制 |
| `references[].nodeId` | string / 是 | 来源节点 ID；必须与数据库资源所属节点一致且不是目标节点 |
| `references[].assetKey` | string / 是 | 已登记的当前画布资源 Key；禁止 URL、Base64、blob、asset:// 输入 |
| `references[].kind` | string / 是 | image / video / audio；后端必须与真实资源类型核对 |
| `resolution` | string / 否 | 默认 480p；允许 480p、720p；测试默认 480p |
| `ratio` | string / 否 | 默认 16:9；允许 16:9、4:3、1:1、3:4、9:16、21:9、adaptive |
| `duration` | integer / 否 | 默认 4；范围 4～30，单位秒 |
| `generateAudio` | boolean / 否 | 默认 true；false 必须按 false 处理，不得被默认值覆盖 |

2000 字是项目选择的硬上限；官方“中文建议不超过 500 字”是写作建议，不能误当接口硬上限。文本为空且有合格参考时允许提交；后端不构造空 text 内容项。只有文本、没有任何参考，返回 `REFERENCE_REQUIRED`。默认值只用于省略字段；显式 null、数字字符串和错误类型一律拒绝。

以下 ID 和 Key 是占位示例，不是已经存在的资源。后续测试须先创建真实节点并替换为上传接口返回的 Key。

```json
{
  "taskId": "99999999-9999-4999-8999-999999999999",
  "canvasId": "11111111-1111-4111-8111-111111111111",
  "nodeId": "22222222-2222-4222-8222-222222222222",
  "model": "doubao-seedance-2-5-260628",
  "mode": "reference",
  "prompt": "参考图片1的彩色几何图案、视频1的图形运动和音频1的节奏，生成一段全新的4秒抽象动画。镜头固定，色块清晰，运动平滑，不要字幕。",
  "references": [
    {
      "nodeId": "33333333-3333-4333-8333-333333333333",
      "assetKey": "frame-space/dev/canvases/11111111-1111-4111-8111-111111111111/uploads/images/33333333-3333-4333-8333-333333333333/66666666-6666-4666-8666-666666666666.png",
      "kind": "image"
    },
    {
      "nodeId": "44444444-4444-4444-8444-444444444444",
      "assetKey": "frame-space/dev/canvases/11111111-1111-4111-8111-111111111111/uploads/videos/44444444-4444-4444-8444-444444444444/77777777-7777-4777-8777-777777777777.mp4",
      "kind": "video"
    },
    {
      "nodeId": "55555555-5555-4555-8555-555555555555",
      "assetKey": "frame-space/dev/canvases/11111111-1111-4111-8111-111111111111/uploads/audios/55555555-5555-4555-8555-555555555555/88888888-8888-4888-8888-888888888888.wav",
      "kind": "audio"
    }
  ],
  "resolution": "480p",
  "ratio": "16:9",
  "duration": 4,
  "generateAudio": true
}
```

### 编号、输入快照与幂等

- `references` 数组顺序是唯一顺序；按类型分别编号。例如 image、video、image、audio 对应图片1、视频1、图片2、音频1。UI 标签与后端 content 构造使用同一规则。
- 同一来源节点或同一 assetKey 不能重复。用户删除 / 重排参考后重新显示编号，提示核对文本中的指代；不擅自改写用户提示词。
- 提交时复制 references 和参数。后端补齐默认值后保存 `input`，不保存参考签名 URL。来源节点后来生成新资源，不得替换该任务的旧 Key。
- `input` 取上表除 taskId 外的规范化字段；规范化只做默认值补齐和 prompt 首尾空白清理，不重排参考或修改内部文字。
- 同 taskId、同规范化输入：返回原任务，活动任务为 202，终态为 200；不重建、不重跑。即使来源节点或服务配置后来变化，仍先查询并返回已有任务。
- 同 taskId、不同输入或任务种类：409 `TASK_ID_CONFLICT`。不同 taskId 在同节点已有未完成任务时：409 `GENERATION_BUSY`；其他节点可正常提交并排队。
- 并发请求依靠 taskId 主键和数据库活动任务唯一约束兜底；先查后写本身不足以保证幂等。遇到冲突后回读原任务，确认输入一致再返回。
- 本地幂等不能证明供应商创建接口幂等。供应商是否已受理不确定时，禁止自动创建第二个任务。

## 4. 响应与查询接口

创建成功返回完整 `VideoTask`。继续复用 `GET /api/tasks/:taskId`、画布任务历史和 `GET /api/canvases/:canvasId` 恢复接口，它们要同时支持图片、视频任务。查询已有任务返回 200，即使任务业务状态为 failed；未知任务返回 404。

| VideoTask 字段 | 规则 |
| --- | --- |
| `id` | 本项目 taskId，不是方舟任务 ID |
| `kind` | video；图片任务后续补 image；旧记录缺失时按 image 兼容 |
| `input` | 上节定义的规范化输入快照，不随节点草稿改变 |
| `status` | 下节定义的本地状态 |
| `providerTaskId?` | 拿到方舟任务 ID 后立即持久化；创建本地任务时可不存在 |
| `providerStatus?` | queued / running / succeeded / failed / expired / cancelled |
| `result?` | 已得到模型成功结果时提供，字段见下文 |
| `error?` | failed / interrupted / storage_failed 时的 `{code,message}`；错误信息须脱敏 |
| `pollingError?` | `{code,message}`，查询暂时失败的提示；不使 running 变成 failed；查询恢复后清除 |
| `createdAt / updatedAt` | UTC RFC3339 字符串；每次可见信息变化时更新 updatedAt |
| `startedAt? / finishedAt?` | 本地执行开始 / 进入终态时间；重新保存时清除 finishedAt，完成后重写 |

视频 `result` 与图片结果为不同类型：包含 `model`；供应商已返回时记录 `resolution / ratio / duration`（约数）和 `framesPerSecond`。下载后经实际媒体检查补充 `width / height / durationSeconds / hasAudio`。不要用接口的整数 duration 冒充文件精确时长。

上述 model、resolution、ratio 为 string；duration、width、height 为 integer；framesPerSecond、durationSeconds 为有限正数；hasAudio 为 boolean。表中带问号的字段在不可用时省略，不用空字符串 / 0 假装存在。result 中的媒体字段也只返回已经实际获得的值；succeeded 必须已有经检查的文件元数据、asset 和 url。

只有成功保存后才返回 `result.asset` 和 `result.url`；url 沿用 `/api/assets/content?key=<encodeURIComponent(Key)>`，asset 沿用现有结构且 kind=videos、source=generated。供应商下载地址保存在后端恢复信息中，不发给前端作长期播放地址。storage_failed 表示视频已生成但暂时没有本项目可播放的持久结果。

前端以 `id + updatedAt` 合并变化，不能仅比较 status：供应商排队变运行、轮询报错恢复时本地 status 可能相同。应用结果始终核对 canvasId、nodeId 和当前任务 ID，旧任务迟到不得覆盖新任务；失败时保留节点上一次可用视频。

## 5. 本地状态及恢复约定

```text
queued → preparing → submitting → running → saving → succeeded
   └ 校验失败 → failed       └ 转存失败 → storage_failed
submitting 且无法确认供应商是否受理 → interrupted
storage_failed → 用户重试保存 → saving → succeeded / storage_failed
```

| 状态 | 含义 | 重启 / 查询失败时的动作 |
| --- | --- | --- |
| queued | 本地已落库，等待执行名额 | 可继续领取执行 |
| preparing | 已领取，检查真实参考素材 | 重启回到 queued；只有落库 submitting 成功后才允许发创建请求 |
| submitting | 开始供应商创建请求，正在取得并持久化 ID | 无已落库 ID 时标 interrupted / SUBMISSION_UNKNOWN；不自动再次提交 |
| running | 已保存 providerTaskId，供应商排队或生成中 | 继续查相同 ID；网络失败或 429 可退避重查，不生成新任务 |
| saving | 已保存供应商成功结果，正在下载和转存 | 只恢复保存；不创建新视频 |
| succeeded | 视频已成功保存并登记资源 | 返回可播放的同源地址 |
| failed | 明确失败，包括媒体不合格、供应商拒绝、异步失败 / expired / cancelled | 保留错误和快照；用户修正后用新 taskId 发起新业务 |
| interrupted | 无法确认外部执行结果或无法安全继续 | 告知可能已计费，保留可用 ID；先核查，不能当普通失败自动重跑 |
| storage_failed | 供应商成功，本项目转存失败 | 保留供应商 ID 和内部结果来源；允许只重试保存 |

providerStatus=expired / cancelled 映射本地 failed，并分别使用 `PROVIDER_EXPIRED` / `PROVIDER_CANCELLED`。查询返回一次 404 不能证明原任务未创建；保留 ID 并继续核查，超过供应商记录保留期仍无结果时转 interrupted / PROVIDER_RESULT_UNAVAILABLE。未知供应商状态也不得当成 succeeded。

submitting、storage_failed 已由 002 迁移接入，preparing 及队列由 005 迁移接入。同节点活动唯一索引覆盖 queued、preparing、submitting、running、saving。图片任务维持原有临时结果及 storageError 语义；不能把图片历史记录按视频规则误判。

### `POST /api/tasks/:taskId/storage-retries`（计划新增）

JSON 空对象 `{}`；只适用于视频任务。storage_failed 可原子改为 saving，返回 202 和同一 VideoTask，输入及供应商 ID 不变。已经 saving 时返回 202，已经 succeeded 时返回 200，重复点击不产生并发转存。其他任务状态返回 409 `STORAGE_RETRY_NOT_ALLOWED`；同节点已有另一未完成任务时返回 409 `GENERATION_BUSY`。转存槽繁忙时持久化等待，不占视频生成槽。

后端没有可用结果来源且不能从既有供应商任务恢复时，返回 410 `RESULT_UNAVAILABLE`，保留 storage_failed 和失败原因；不自动重新生成。保存重试可 GET 查询原供应商任务，但不得 POST 新建供应商任务。资源登记与任务完成采用数据库事务；OSS 不在事务内，后续实现仍要防止并发重复写并记录可能的孤立对象。

## 6. 素材校验与上传契约扩展

图片 / 视频 / 音频规则独立于既有图生图规则；不能把 Seedream 的宽高、比例条件直接用于 Seedance。以下官方能力来自用户 PDF（PDF 第 7、9、10 页）；项目限制有意单列。

| 项目 | 本期校验 |
| --- | --- |
| 数量 | 图片 0～30、视频 0～10、音频 0～10，总数至少 1 |
| 图片格式 / 大小 | 项目首版 PNG、JPEG、WebP、GIF、BMP；沿用不超过 20 MiB 的上传限制，比官方“小于 30 MB”更严 |
| 图片尺寸 | 宽和高均 300～6000；宽/高在 0.4～2.5 |
| 视频格式 / 编码 | MP4 或 MOV；视频轨 H.264 / H.265；有音轨时 MP4 为 AAC / MP3，MOV 为 AAC / MP3 / PCM |
| 视频尺寸 / 帧率 | 宽高均 300～6000，比例 0.4～2.5，总像素 407696～8295044；帧率 24～60 |
| 视频时长 / 大小 | 单段 2～30 秒，所有视频总时长不超过 30 秒；本期保守按不超过 200000000 字节校验 |
| 音频格式 / 时长 / 大小 | WAV / MP3；单段 2～30 秒，所有音频总时长不超过 30 秒；保守按不超过 15000000 字节校验 |

官方标注的 MB 未在这份 PDF 中明确十进制 / 二进制；视频和音频采用十进制阈值作为项目保守选择。不得把当前上传服务的 200 MiB 直接当成视频参考上限。总时长按媒体实际精度计算后比较，不能先向下取整掩盖超限；元数据不完整、帧率无法可靠判定或文件损坏时拒绝进入付费调用。

前端用于及时反馈；后端从已登记的同画布资源检查类型、节点、去重、文件内容及元数据。普通上传成功和模型参考合格是不同状态；例如 WebM 可以继续作为普通画布素材，选作视频参考时拒绝。后端预检失败可发生在接收 202 之后，此时任务应变 failed 且供应商创建次数为零。

上传仍用 `POST /api/assets?canvasId=...&nodeId=...` 的二进制方式。计划扩展：

- 节点 kind 增加 audio，音频节点 origin=upload；可播放和被视频引用，不能作为图片参考，也不提供音频生成。
- StoredAsset.kind 增加 audios，Key 使用 `uploads/audios/<节点UUID>/<资源UUID>.wav或.mp3`；保留不可变 Key。
- `/api/storage/config` 增加 `maxAudioBytes:15000000`；已有 maxVideoBytes 含义仍是普通上传上限，界面另应用上述参考限制。
- assets 元数据记录实际 width、height、durationSeconds、frameRate、videoCodec、audioCodec 等适用字段；以服务端探测为准。第三步已实现 `asset.media` 和 `asset.videoReference`；音轨存在可由 audioCodec 判断。老素材当前提示重新上传；“后台预检补齐”仍属于下一阶段视频任务方案，不信任浏览器声明。
- 用 ffprobe 探测后端 D 盘临时文件，设置执行超时和并发上限；首版遇到不合格素材提示更换，不自动裁剪、拉伸或转码改变参考内容。

含真人的素材依照方舟规定的授权 / 信任素材路径处理，不将上传成功视为供应商一定接受。最小样例使用不含真人的素材，避免把模型准入问题与接口实现混在一起。

## 7. 后端转换为方舟请求

后端调用 `POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks`，Header 由后端设置 Bearer ARK_API_KEY。随后用 `GET /api/v3/contents/generations/tasks/{providerTaskId}` 查询。模型、输入角色、模式不得由浏览器任意透传。

| 项目输入 | 方舟字段 |
| --- | --- |
| 非空 prompt | content 中的 text 项，排在素材项前 |
| image 参考 | type=image_url，role=reference_image，image_url.url=签名地址 |
| video 参考 | type=video_url，role=reference_video，video_url.url=签名地址 |
| audio 参考 | type=audio_url，role=reference_audio，audio_url.url=签名地址 |
| model / resolution / ratio / duration | 对应同名顶层字段 |
| generateAudio | generate_audio |
| mode=reference | omni_reference_task_type=reference |
| 后端固定配置 | output_format=mp4、watermark=true、execution_expires_after=3600 |

这些时限是**首版项目策略**：创建请求 HTTP 超时 60 秒，单次查询 15 秒；后台按约 10 秒间隔查询，网络错误 / 429 退避到最多 60 秒并遵守 Retry-After；前端沿用 2 秒查询本项目后端。单次查询超时与供应商任务过期分开处理。3600 秒是方舟允许的任务过期下限，不能因本地等候几分钟就假定供应商任务失败。

参考预检完成后、即将提交时签发有效 2 小时的 OSS GET 地址，覆盖上述 1 小时任务窗口并留余量；不沿用图片的 10 分钟 TTL。这是可验证的设计策略，不是文档承诺“素材一定在何时被读取”。如修改供应商过期窗口，必须同时调整签名期限；签名凭据自身剩余有效期也必须足够，不能通过公开存储桶绕过。

供应商 ID 与 running 状态必须一起落库；保存失败可重试数据库写入，不能再创建供应商任务。成功响应中的下载地址应先保存到内部恢复信息，再转存，保持既有只允许公网 HTTPS、限制下载大小等边界；为大视频设置独立的传输超时，不复用图片下载时限。

## 8. 错误契约

HTTP 拒绝仍用 `{ "error": { "code": "...", "message": "中文说明" } }`。已接收任务的失败由任务 error 返回，GET 本身仍为 200。不得将方舟鉴权失败返回为“本项目用户未登录”。

| HTTP / 场景 | code | 行为 |
| --- | --- | --- |
| 400 请求字段、模型、模式、输出参数无效 | INVALID_REQUEST / INVALID_MODEL / UNSUPPORTED_MODE / INVALID_VIDEO_OPTIONS | 不建任务、不调用模型 |
| 400 无参考或数量超限 | REFERENCE_REQUIRED / TOO_MANY_REFERENCES | 不调用模型 |
| 400 引用归属、重复、类型不符 | INVALID_REFERENCE_SCOPE / DUPLICATE_REFERENCE / INVALID_REFERENCE_TYPE | 不调用模型 |
| 404 画布、节点、资源或任务不存在 | NOT_FOUND | 不调用模型 |
| 409 ID 冲突 / 活动名额被占 | TASK_ID_CONFLICT / GENERATION_BUSY | 不增加供应商任务 |
| 413 / 415 | REQUEST_TOO_LARGE / INVALID_CONTENT_TYPE | 拒绝超大 JSON 或错误 Content-Type |
| 503 配置或服务不可用 | VIDEO_GENERATION_NOT_CONFIGURED / DATABASE_UNAVAILABLE | 说明缺失的服务类别，不输出凭据 |
| 异步媒体校验失败 | INVALID_REFERENCE_MEDIA / REFERENCE_UNAVAILABLE | failed；供应商创建次数为零 |
| 明确的供应商错误 | PROVIDER_AUTH_FAILED / PROVIDER_ACCESS_DENIED / PROVIDER_RATE_LIMITED / PROVIDER_FAILED | failed；提示核查或修正，无自动付费重试 |
| 创建响应丢失 / 请求结果不确定 | SUBMISSION_UNKNOWN | interrupted；可能已计费，需要核查 |
| 已知任务查询暂时失败 | PROVIDER_QUERY_UNAVAILABLE | 放入 pollingError；保留 running，继续查询 |
| 结果转存失败 | RESULT_STORAGE_FAILED | storage_failed，保留原供应商结果 |

创建时发生网络超时、无有效任务 ID 的异常响应或不能证明未受理的服务端错误，都按不确定结果处理，不笼统归为可重新生成的失败。日志可记录本地 / 供应商任务 ID、阶段、耗时、脱敏错误和用量；不记录 API Key、签名 URL、文件内容或完整敏感提示词。

## 9. 兼容与资料依据

第一步只新增文档；第三步已扩展音频节点和混合引用。后续视频任务实现应扩展现有节点草稿白名单，保存 videoModel、videoMode、videoResolution、videoRatio、videoDuration、videoGenerateAudio；旧视频节点使用本契约默认值。任务结果、供应商 ID 和运行状态仍从任务表恢复，不写入可编辑草稿；否则旧页面保存会覆盖后台状态。

任务表增加 kind、供应商关联 / 恢复字段并扩展状态约束；assets 增加媒体元数据；画布和引用继续存 JSONB，不增加独立节点表或引用中间表。当前没有登录和跨用户鉴权，UUID / 画布归属检查不是权限系统，服务继续限于本机单人场景。

依据与核对日期：2026-10-02。

- 用户提供的 `D:\新建文件夹\火山方舟_创建视频生成任务_1790073371.pdf`：PDF 第 4、7～12、15～20 页，提供模型能力、参考约束、参数和异步创建说明。
- [官方创建任务](https://docs.volcengine.com/docs/ark/create-video-generation-task-api?lang=zh)：补充核对 reference 模式及模型调用示例。
- [官方模型列表](https://docs.volcengine.com/docs/ark/model-list?lang=zh)：模型 ID；仅核对文档，不代表账号开通。
- [官方 SDK 任务接口源码](https://github.com/volcengine/ark-runtime-go/blob/main/arkruntime/content_generation.go)：创建与按 ID 查询的路径和职责。
- [官方任务列表说明](https://docs.volcengine.com/docs/ark/list-video-generation-tasks-api?lang=zh)：任务记录保留 7 天、产物地址有效 24 小时，2.5 产物地址下载次数上限 100；不能把供应商临时地址当永久资产。
- 当前源码：`backend/internal/server/{router,images,assets,persistence}.go`、`backend/internal/persistence/{models,worker,store}.go`、迁移 `001_initial.sql`、`frontend/src/{api/persistence,canvas/media,canvas/useMediaNodes,canvas/useCanvasSession}.ts`。当前仍是图片生成实现。
