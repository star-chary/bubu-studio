# Bubu Studio 后端

2026-10-07 视频模型更新：支持 Seedance 2.5、2.0、2.0 fast、2.0 mini 的文生视频和全能参考；按模型校验输出参数及真实参考素材，并按 `test-2026-10-07-v2` 报价。首版只开放 480p／720p。未新增数据库表或迁移；三个新增模型的账户授权和真实结果尚未付费验收。见 [视频模型与文生视频](../docs/video-models.md)。

2026-10-06：已加入测试积分，图片与视频均需先报价并提交 `acceptedPoints`、`priceVersion`；后端重新核价，在 PostgreSQL 事务中冻结余额、写任务和流水。结果可持久交付才结算，明确失败退回，不确定结果保持冻结待核查。无自动赠送或付费充值。独立测试库 Go／HTTP 测试通过；主库备份与 006 迁移已验证，唯一用户已发放 200 分并核对可用 200、冻结 0。未真实调用模型，详见 [测试积分](../docs/test-credits.md)。

2026-10-05：除 `/health` 和统一登录入口外，业务接口必须通过 PostgreSQL Session 认证；写请求验证来源和 CSRF Token。无 DATABASE_URL 时返回 503，已取消匿名业务入口。账号、会话、所有权、迁移和本地配置见 [登录与鉴权](../docs/authentication.md)。

2026-10-04：已增加画布列表接口、创建时的可选名称和旧数据兼容迁移 `003_canvas_library.sql`；详情响应增加 `title`，原快照保存与素材 / 任务归属不变。真实 PostgreSQL 测试、vet、构建及前后端双画布验收通过。契约与验证见 [首页与独立画布](../docs/canvas-library.md)。

2026-10-03：OSS 上传诊断修复已通过 Go / PostgreSQL 测试、vet、构建和真实图片 / 视频上传验收。连接超时明确为 15 秒，仅连接建立前的可恢复失败最多尝试 3 次；错误按网络、超时、权限、凭据、配置等分类并记录脱敏日志。保存与生成模型调用保持独立，详见 [上传失败排查与恢复](../docs/object-storage.md#上传失败排查与恢复)。

2026-10-03：第二阶段已接入提示词标签到模型输入的转换、不可变素材快照、Seedance 2.5 视频任务和只重试保存。前端构建、57 项浏览器回归、Go / PostgreSQL 集成测试及 vet 通过；真实 Lite 引用图片、4 秒 / 480p 有声视频生成和 OSS 转存、刷新播放均已验收。当前实现与真实验收状态见 [引用生成第二阶段](../docs/reference-generation.md)。

2026-10-02 历史阶段记录：画布快照新增可选 `promptParts`，保存文字和图片／视频引用片段并校验 `prompt` 投影一致。含引用片段的草稿暂不创建新图片任务，返回 409 `PROMPT_REFERENCES_NOT_READY`；原任务幂等重查与普通图片任务保持原行为。无需新增表或迁移，详见 [提示词引用第一阶段](../docs/prompt-mentions.md)。

2026-10-02 历史阶段记录：第三步新增 WAV/MP3 上传、ffprobe 媒体参数读取和视频参考单文件校验；元数据写入现有 assets JSONB，不新增表。真实音视频上传、Range、数据库恢复及测试已通过，视频生成 API 尚未实现。详见 [素材与引用](../docs/media-references.md)。音视频上传要求 PATH 中有 ffprobe，或设置 `FFPROBE_PATH` 为可执行文件绝对路径；缺失时返回可重试的保存错误，不写 OSS。

Go + Gin 后端，使用方舟 Seedream 生成单图、OSS 保存媒体、PostgreSQL 保存画布与生成任务。2026-09-24 已接入 pgx 连接池、版本化迁移、后台任务与恢复接口；数据库真实集成测试通过。本次未新增真实模型调用，之前 Lite 图生图 / OSS 真实验收与 Pro 图生图模拟验收仍保留。完整数据模型和接口见 [持久化说明](../docs/persistence.md)。

## 启动

使用 Go 1.26 或更高版本，本机开发默认监听 `127.0.0.1:8080`。

本机先在项目根运行 `./scripts/start-postgres-wsl.ps1`。`DATABASE_URL` 从 `.env.local` 或环境变量读取；配置后启动自动迁移、获取单执行器锁并运行后台任务。配置了无效连接时拒绝启动；留空时登录与业务接口返回 503。测试库使用独立的 `TEST_DATABASE_URL`，不能指向用户业务数据库。

```powershell
cd backend
go mod download
# 第一次配置时复制模板，然后在 .env.local 中填写自己的 API Key。
# 已有 .env.local 时不要覆盖。
# Copy-Item .env.example .env.local
go run ./cmd/server
```

打开 <http://127.0.0.1:8080/health>，正常返回 HTTP 200 和 `{"status":"ok"}`。按 Ctrl+C 停止服务。

可用进程环境变量调整端口：

```powershell
$env:PORT = '8081'
go run ./cmd/server
```

启动时读取当前后端目录的 `.env.local`，已有进程环境变量优先。密钥使用 `ARK_API_KEY`，修改密钥后需重启后端。本地配置被 Git 忽略，模板 `.env.example` 不包含凭据。模型现在由每次请求选择，不再读取旧的 `ARK_IMAGE_MODEL` 环境变量，切换模型无需重启。

OSS 默认关闭。填写 `.env.local` 的 `OSS_REGION`、`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET` 后，设 `OSS_ENABLED=true` 并重启。Endpoint 可由 Region 推导。配置已启用但不完整会拒绝启动。具体目录分层、权限模板、大小限制、接口及验证见 [对象存储说明](../docs/object-storage.md)。模型密钥与 OSS 凭据相互独立。

未设置 `PORT` 时使用 8080；端口冲突会报错退出。前端 Vite 的 `/api` 代理默认指向 `127.0.0.1:8080`；调整后端端口时，在启动前端的终端设置 `API_PROXY_TARGET=http://127.0.0.1:新端口`。

如果官方 Go 下载代理连接超时，可以在当前终端临时指定代理后重试，依赖校验仍启用：

```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'
go mod download
```

## 目录与运行流程

```text
backend/
├─ cmd/server/main.go        本地配置、启动入口、监听地址、HTTP 服务
├─ internal/server/router.go 路由注册、请求日志和异常恢复
├─ internal/server/images.go JSON / 提示词 / 模型校验、并发限制、结果和错误响应
├─ internal/server/assets.go 存储能力、二进制上传、私有预览及 Range
├─ internal/ark/models.go    两个允许调用的模型 ID、默认值与白名单校验
├─ internal/ark/images.go    方舟鉴权、请求参数、超时、响应解析与错误映射
├─ internal/storage/         OSS SDK、目录分配、文件校验、生成结果转存
├─ .env.example             无密钥的配置模板
├─ go.mod                   模块名称、Go 版本和依赖版本
├─ go.sum                   下载依赖的校验记录
└─ README.md                启动与验证说明
```

```text
运行入口 → 创建 Gin 路由 → 启动 HTTP 监听
浏览器 GET /health → 通用中间件 → 匹配路由 → 返回 JSON
```

路由负责按请求方法和路径找到处理函数。日志和异常恢复是通用中间件：前者记录请求路径、状态和耗时，后者在处理请求发生 panic 时恢复并返回错误。图片 Handler 校验 HTTP 输入，Ark Client 负责模型调用，Storage 负责文件校验、目录分配和 OSS 读写。

测试积分接口：`GET /api/credits` 返回当前用户的 `{available,reserved}`，`GET /api/credits/ledger` 返回分页流水，`POST /api/credits/quote` 为已保存的图片或视频节点报价。积分发放没有公开 HTTP 接口，新注册账号为 0 分。生成提交时后端重新报价；缺积分返回 402 / `CREDITS_INSUFFICIENT`，报价版本或分数变化返回 409 / `CREDIT_QUOTE_CHANGED`，资源不属于当前用户或不存在返回 404 / `NOT_FOUND`。这些拒绝不创建任务、不调用方舟。接口及状态机见 [测试积分](../docs/test-credits.md)。

## 图片生成接口

**当前模式：** 需有效 Session，写请求需 CSRF Token；画布与节点必须归当前用户所有并先通过保存 API 入库。先调用 `POST /api/credits/quote` 获得 `{points,priceVersion}`，再将它们作为 `acceptedPoints`、`priceVersion` 随必填 UUID `taskId` 提交。返回 HTTP 202 与 `id/input/status/createdAt/creditPoints/creditStatus/...` 任务对象，使用 `GET /api/tasks/:id` 或 `GET /api/canvases/:id/tasks` 查询。最终图片位于 `task.result`，失败位于 `task.error`。同 ID 相同输入重交返回原任务，不会重复调用模型或冻结积分。下面的同步 200 响应及 HTTP 模型错误示例是历史记录；当前模型错误写入任务后由查询接口返回，无数据库时业务接口不可用。余额、流水、报价与结算规则见 [测试积分](../docs/test-credits.md)。

`POST /api/images/generations`，`Content-Type: application/json`：

```json
{"taskId":"39c6f2cd-9379-4486-ad15-ea1f7fabed85","prompt":"黑洞中冲出一辆复古列车，电影质感","model":"doubao-seedream-5-0-pro-260628","canvasId":"f2476fbb-2af0-494f-9a38-f8f9bdf1e5ac","nodeId":"56b46c49-fc0b-41cf-9db0-01493aa14d3f","acceptedPoints":16,"priceVersion":"test-2026-10-06-v1"}
```

接收 `taskId`、`prompt`、可选的 `model`、必填的 `canvasId` 和 `nodeId`，以及可选的 `referenceKeys: string[]`、`promptParts`。参考列表为空时为文生图，有值时为图生图，详见 [图片生成说明](../docs/image-generation.md) 和 [引用生成第二阶段](../docs/reference-generation.md)。归属 ID 必须为 UUID，否则在付费模型调用前拒绝请求。提示词去除两端空白后要求 1～2000 个 Unicode 字符，请求体上限 64 KiB。`model` 省略或留空时默认 Lite；未知模型返回 400 / `INVALID_MODEL`。拒绝其他字段，不能从浏览器传入密钥、尺寸、任意图片 URL 或 Base64；参考只能提交已保存且归属正确的资源。

| 展示名称 | model 值 |
| --- | --- |
| Doubao-Seedream-5.0-lite（默认） | `doubao-seedream-5-0-260128` |
| Doubao-Seedream-5.0-pro | `doubao-seedream-5-0-pro-260628` |

Lite 沿用前一阶段真实验证过的 ID；Pro ID 核对自[火山方舟模型发布公告](https://docs.volcengine.com/docs/ark/model-release-announcement?lang=zh)。模型展示目录在前端 `src/models/imageModels.ts`，调用白名单在后端 `internal/ark/models.go`，新增选项时需同步两处。

历史同步模式的 HTTP 200 结果示例（当前成功任务的 `result` 使用此类结构）：

```json
{"url":"https://供应商返回的图片地址","model":"doubao-seedream-5-0-260128","size":"3136x1312"}
```

启用 OSS 且转存成功时，`url` 改为 `/api/assets/content?key=...`，额外返回包含 Object Key、来源、类型、大小和画布 / 节点 ID 的 `asset`，此时才结算积分。图片转存失败时任务仍提供临时图片并包含 `storageError`，积分保持冻结待核查；不会把保存失败变成再次调用模型。完整资源响应见对象存储说明。

`model` 为本次实际调用的 ID，`size` 来自模型实际返回，可能省略。两个模型都请求单张图片、`response_format=url`、`size=2K`、`watermark=true`；2K 是请求参数，实际宽高由模型输出决定。Lite 额外发送 `sequential_image_generation=disabled`、`stream=false`；根据用户提供的方舟接口文档，Pro 不支持配置这两项，后端会省略，而非复用 Lite 的完整请求体。

错误统一返回 `{"error":{"code":"错误码","message":"可展示的中文信息"}}`：

| HTTP 状态 | 代表情况 |
| --- | --- |
| 400 / 415 | JSON、字段、模型、提示词或 Content-Type 不符 |
| 409 | 本地后端已有生成正在进行 |
| 422 | 供应商拒绝生成参数或提示词 |
| 429 | 供应商频率 / 额度限制 |
| 503 | 未配置后端密钥 |
| 504 | 供应商调用超过 180 秒 |
| 502 | 供应商鉴权失败、模型未开通 / 不存在、网络错误、服务错误或无有效图片 |

请求链路：`前端 prompt + model + 资源归属 → Vite 开发代理 → Gin Handler 校验 → Ark Client → 方舟 → 提取图片 URL → 已启用则转存 OSS → 前端展示`。不透传供应商原始错误响应，不记录 API Key、请求正文或带签名的结果地址；不跟随携带凭据请求的重定向。

数据库的部分唯一索引限制同一画布节点同时只有一个未完成任务；PostgreSQL 持久化队列另设图片、视频和转存并行槽。任务主键实现幂等；后台任务独立于浏览器请求上下文。重启时未知的模型提交保持待核查，saving 只继续保存已记录结果；失败、超时和中断均不自动重新调用模型。详见 [生成队列](../docs/generation-queue.md)。

开启 OSS 后文件保存在云端，预览在校验登录与资源所有权后由后端读取私有对象并转发，视频支持 Range。导入文件通过单独上传接口保存；临时文件在后端 `.storage-tmp/`，处理后清理，未实现直传或分片。参考准备最长 30 秒，模型最长 180 秒，转存最多 90 秒；前端每 2 秒查询任务。账号、Session、画布快照、资源登记和生成历史在 PostgreSQL 中保存。没有 Redis 或团队协作；服务仅监听本机。健康检查仅检查 HTTP 服务，不代表模型或 OSS 连通。

## 检查与构建

2026-09-24：设置 `TEST_DATABASE_URL` 后的完整 Go 测试、vet、构建通过，集成用例会创建并清理独立测试 schema；不设置时数据库集成测试明确跳过。前端 40 项回归与真实数据库浏览器保存 / 刷新 / 失败任务追踪通过，模型与对象存储成功分支用模拟实现配合真实 PostgreSQL 验证。详细证据见 [持久化说明](../docs/persistence.md)。

```powershell
go test -timeout 30s ./...
go vet ./...
go build -o bin/server.exe ./cmd/server
```

`bin/` 为本地构建产物，已加入根目录 Git 忽略规则。初始化方法参考 [Gin 官方 Quickstart](https://gin-gonic.com/en/docs/quickstart/)。

2026-09-21 使用 Go 1.26.6 / Windows 验证：测试、`go vet`、编译通过。测试用本地 HTTP 模拟服务覆盖 Lite / Pro 请求差异、所选模型转发、省略模型时默认 Lite、未知模型拒绝、鉴权头、供应商错误映射、超时不重试、无效图片结果、重定向拒绝、并发拦截和结束后再次调用，不使用真实密钥或模型。

另完成一次真实前端 → 后端 → 方舟 → 画布验证：模型 `doubao-seedream-5-0-260128`，一次请求，返回并显示 3136 × 1312 图片，总计约 26.4 秒。证据在被 Git 忽略的 `frontend/artifacts/text-to-image-live.json` 和截图中，未写入密钥或结果签名地址。

模型选择接通后，另从前端下拉列表选择 Pro 完成一次真实调用：`doubao-seedream-5-0-pro-260628`，只提交 1 次请求，约 63.7 秒返回并显示 2816 × 1584 图片。记录和截图为 `frontend/artifacts/text-to-image-pro-live.json` / `.png`。两次耗时仅为本次体验记录，不代表模型速度对比或服务承诺。
