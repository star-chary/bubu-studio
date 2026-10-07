# 项目地图

2026-10-07 部署验证记录：正式 HTTPS 登录／会话、画布保存、CSRF、PNG 上传／读取／Range、匿名拒绝及退出失效已通过接口验收；密码 6～20 字符边界经过真实接口与浏览器验证。本轮未调用付费模型。公开仓库提供 [部署指南](./docs/deployment.md) 和示例配置，具体服务器资料及发布记录保留在本地。

2026-10-07 视频模型更新：视频节点支持 Seedance 2.5、2.0、2.0 fast、2.0 mini，提供文生视频／全能参考两种输入。模型、时长、参考素材规则、积分报价与后端方舟请求随选择变化；首版只开放 480p／720p。画布快照新增可选 `videoModel`／`videoMode` 字段，旧画布按 2.5／全能参考读取；无新表或迁移。隔离 PostgreSQL 的 Go 全包测试、vet、前端构建及 89/89 浏览器模拟接口回归通过，未调用真实方舟。实现与验收边界见 [视频模型与文生视频](./docs/video-models.md)。

2026-10-06 公开首页更新：访客根路径只显示品牌、登录和创建入口，不读取个人画布与积分；创建意图在登录后进入命名弹窗，确认后才写数据库。画布深链仍要求登录，后端私有 API 鉴权不变。前端构建和 84 项模拟接口浏览器回归通过；详见 [首页与独立画布](./docs/canvas-library.md)。

2026-10-06 测试积分更新：006 迁移新增账户和流水，生成任务保存报价快照；后端按登录用户报价，事务内冻结余额并创建任务，成功保存后结算、明确失败退回、不确定结果保留冻结待核查。前端显示报价与余额并拦截余额不足。独立测试库 Go／HTTP、前端构建及完整浏览器回归 81/81 通过；浏览器测试使用模拟接口。主库备份已验证，唯一用户已发放 200 分并核对可用 200、冻结 0；未真实调用供应商。详见 [测试积分](./docs/test-credits.md)。

2026-10-05 队列更新：已接入 PostgreSQL 持久化等待与并行领取，默认视频并发 3、图片并发 1、转存并发 2。同节点防重复和已提交任务恢复保留。入口：`backend/internal/persistence/queue.go`、005 迁移、前端 `generationStatus.ts`；机制与验证见 [生成队列](./docs/generation-queue.md)。

2026-10-05：邮箱密码登录／自动注册、服务端 Session、CSRF 与资源所有权已实现并验证。核心入口与契约见 [登录与鉴权](./docs/authentication.md)。旧主库 9 张画布及关联记录已显式清空；OSS 原文件保留且不再提供应用访问。

2026-10-04：视频提示词参数 UI 已优化，模型 / 模式菜单、比例 / 画质 / 时长弹层、空参考置灰及音频兼容通过 27 项相关浏览器用例与前端构建。没有新增生成模式或后端契约，见 [视频提示词参数 UI](./docs/video-prompt-controls.md)。

2026-10-04：已完成首页、新建 / 历史画布入口、独立编辑会话和保存后返回。前后端构建、58 项原回归 + 7 项专项、Go / PostgreSQL 测试及真实双画布验收通过。旧画布保留，无模型调用。见 [首页与独立画布](./docs/canvas-library.md)。

2026-10-03：OSS 上传诊断修复完成。明确连接超时、有限连接重试和脱敏错误分类；Go / PostgreSQL 测试、vet、5 项存储浏览器回归及真实 PNG / MP4 上传与读取校验通过。未新增表或调用模型，详见 [上传失败排查与恢复](./docs/object-storage.md#上传失败排查与恢复)。

2026-10-03：第二阶段已接入提示词标签到模型输入的转换、不可变素材快照、Seedance 2.5 视频任务和只重试保存。前端构建、57 项浏览器回归、Go / PostgreSQL 集成测试及 vet 通过；真实 Lite 引用图片、4 秒 / 480p 有声视频生成和 OSS 转存、刷新播放均已验收。当前实现与真实验收状态见 [引用生成第二阶段](./docs/reference-generation.md)。

2026-10-02 历史阶段记录：完成提示词 `@` 素材引用第一阶段：图片／视频菜单、已引用列表、稳定 ID 标签、参考同步、保存与刷新恢复。含新标签的生成输入暂不提交；普通图片生成保留，视频生成仍未实现。新增片段沿用画布 JSONB，不新增表。55 项浏览器回归、Go 测试（含真实 PostgreSQL）和真实画布恢复验收通过，无模型调用。详见 [提示词引用](./docs/prompt-mentions.md)。

2026-09-24 基线：已接 PostgreSQL，支持画布自动 / 手动保存、刷新恢复、后台图片任务和任务历史。真实数据库集成测试、浏览器实机保存 / 失败追踪及 40 项浏览器回归通过；该阶段未新增付费模型调用。此前 Lite 图生图与 OSS 转存已真实验证，Pro 图生图仍为模拟验证。

2026-10-02 历史阶段记录：第三步素材与引用能力已完成。新增 WAV/MP3、服务端媒体参数、视频节点三类引用与校验提示；40 项原回归及 8 项专项、Go 测试（含真实 PostgreSQL）、vet、前端构建通过，真实 OSS 上传 / Range / 刷新播放与引用恢复通过。未新增数据库表，未调用付费模型；视频生成接口仍为待实现设计。详见 [第三步记录](./docs/media-references.md)。

| 位置 | 职责 / 状态 |
| --- | --- |
| `docs/deployment.md` | 通用部署目录、配置模板、服务与数据流、发布检查和证书维护 |
| `docs/development-notes.md` | 原 README 中的开发过程、交互细节和分阶段验证记录 |
| `deploy/nginx/studio.example.conf`、`frame-space-proxy.conf`、`frame-space-upload-size.conf` | 脱敏后的独立站点示例、同机反向代理和上传 200 MiB 配置；请求头边界已验证，完整 200 MiB 上传尚未验收 |
| `deploy/systemd/frame-space.service` | 已启用的专用用户 Go 常驻服务，使用独立环境配置和工作目录 |
| `backend/internal/server/proxy.go`、`proxy_test.go` | 可选可信代理 IP/CIDR 配置与客户端 IP 解析边界测试 |
| `docs/test-credits.md` | 测试积分定价、发放边界、账户／流水模型、生成结算状态、接口与验证 |
| `backend/internal/persistence/credits.go`、`migrations/006_test_credits.sql` | 按用户核价、积分账户与流水、原子冻结／结算、任务价格快照 |
| `backend/internal/server/credits.go`、`credits_test.go` | 余额／流水／报价接口，独立测试库 HTTP 零余额与用户隔离验证 |
| `frontend/src/api/credits.ts`、`components/NodePromptPanel.vue` | 读取余额、请求报价、生成前展示本次积分与不足提示 |
| `docs/authentication.md` | 邮箱自动注册／登录、Session、CSRF、所有权、旧数据重置与验收 |
| `backend/internal/identity/`、`persistence/auth.go`、`migrations/004_auth.sql` | 密码与随机凭证、用户／会话事务、唯一性、归属查询 |
| `backend/internal/server/auth.go`、`auth_test.go` | 会话中间件、来源／CSRF／限流、完整路由与数据库权限测试 |
| `frontend/src/auth/session.ts`、`views/LoginPage.vue`、`components/AccountControl.vue` | 登录状态、统一请求、自动注册入口、退出与跨标签页失效 |
| `frontend/tests/auth.spec.ts`、`scripts/verify-live-auth.mjs` | 模拟会话交互回归与真实隔离数据库端到端验收 |
| `backend/cmd/reset-canvases/` | 显式旧画布清理工具，默认只读且拒绝重置已有用户数据 |
| `AGENTS.md` | AI 协作与工程解释约定 |
| `frontend/src/components/VideoGenerationControls.vue`、`frontend/tests/video-controls.spec.ts` | 四款视频模型、文生视频／全能参考、参数弹层与交互回归 |
| `docs/video-models.md`、`docs/video-prompt-controls.md` | 当前视频模型能力、报价与数据流；控件历史阶段记录 |
| `docs/canvas-library.md`、`frontend/src/views/GuestHomePage.vue`、`HomePage.vue` | 公开首页、登录后个人画布库、命名新建、历史分页与布局预览 |
| `backend/internal/persistence/migrations/003_canvas_library.sql`、`canvas_library_test.go` | 名称、排序索引、旧数据迁移与跨画布隔离测试 |
| `frontend/tests/canvas-library.spec.ts`、`frontend/scripts/verify-live-canvas-library.mjs` | 7 项专项与真实双画布保存 / 切换 / 恢复验收 |
| `docs/reference-generation.md`、`backend/internal/ark/videos.go`、`persistence/video_worker.go` | 标签编译、视频接口、后台状态机与结果保存恢复 |
| `docs/prompt-mentions.md` | 提示词引用第一阶段的交互、数据结构、生成边界与验收 |
| `frontend/src/components/PromptEditor.vue`、`canvas/prompt.ts`、`canvas/promptDom.ts` | 图片／视频 `@` 菜单、结构化标签、纯文本粘贴、光标及撤销 |
| `backend/internal/persistence/prompt.go`、`prompt_test.go` | 引用草稿校验、数据库保存与新任务提交边界 |
| `README.md` | 启动命令、交互、状态流、验收结果与边界 |
| `backend/README.md` | 后端范围、启动、目录职责与构建说明 |
| `docs/image-generation.md` | 文生图 / 图生图操作、参考输入契约、签名机制、单张输出和验证证据 |
| `docs/media-references.md` | 第三步已完成范围、素材数据流、校验职责、真实验收与学习重点 |
| `backend/internal/storage/probe.go`、`video_reference.go`、`probe_test.go` | 真实媒体参数探测、单文件参考条件及边界验证 |
| `frontend/src/canvas/videoReferences.ts`、`tests/audio-references.spec.ts`、`tests/video-reference-rules.spec.ts` | 全模态参考前置提示、音频交互和数量 / 时长边界 |
| `scripts/create-reference-fixtures.ps1`、`frontend/scripts/verify-live-media-references.mjs` | 自制测试素材及显式运行的真实数据库 / OSS / 刷新验收 |
| `docs/video-generation-contract.md` | 全模态参考视频请求、响应、输入快照、状态恢复和媒体规则；实现记录见 reference-generation.md |
| `docs/video-generation-acceptance.md` | 三类参考生成单条视频的最小验收规格、已有不合格样本及待执行断言；未做真实生成 |
| `docs/object-storage.md` | OSS 配置、目录 / 权限、上传 / 转存 / 预览契约、验证与持久化边界 |
| `docs/persistence.md` | PostgreSQL 数据模型、保存 / 恢复、任务状态机、幂等、并发、启动与验收 |
| `scripts/start-postgres-wsl.ps1`、`compose.yaml` | 当前本地 WSL 数据库启动，以及未实机验证的 Docker 备选 |
| `backend/internal/persistence/` | pgx 连接池、迁移、画布 / 资源 / 任务仓储、后台执行器与真实数据库测试 |
| `backend/internal/server/persistence.go`、`persistence_test.go` | 画布保存 / 恢复、任务查询 / 分页接口及真实 HTTP 集成测试 |
| `frontend/src/canvas/useCanvasSession.ts` | 画布 ID、恢复、串行自动保存、版本冲突、任务轮询和历史分页 |
| `frontend/src/api/persistence.ts`、`components/TaskHistory.vue` | 持久化 API、同任务 ID 的提交恢复、任务记录面板 |
| `frontend/tests/persistence.spec.ts`、`scripts/verify-live-persistence.mjs` | 6 项模拟接口浏览器验收及真实 Go / PostgreSQL 保存与失败任务验收 |
| `backend/oss-policy.example.json` | 仅授权指定 Bucket 的项目开发目录读写，已限定 bubu2/frame-space/dev/* |
| `backend/internal/storage/keys.go`、`keys_test.go` | 对象路径分配与校验：环境、画布、来源、类型、节点、新资源 UUID |
| `backend/internal/storage/oss.go`、`oss_errors.go`、`oss_errors_test.go` | OSS SDK 适配、连接超时 / 有限重试、私有写入、Range 读取、错误分类及脱敏诊断 |
| `backend/internal/storage/store.go`、`media.go`、`download.go` | 文件内容 / 大小校验、D 盘临时文件、并发限制、结果下载转存及公网地址限制 |
| `backend/internal/storage/references.go`、`references_test.go` | 参考 Key 归属、格式 / 尺寸检查、有序临时读取地址与错误边界 |
| `backend/internal/server/references_test.go` | 图生图参数、模型数量上限、前置失败不调用模型及签名不透传 |
| `backend/internal/storage/store_test.go` | 模拟对象存储 / HTTP 验证写入、读取、参数、失败和安全边界 |
| `backend/internal/server/assets.go`、`assets_test.go` | 保存能力查询、二进制文件上传、私有预览与视频 Range 接口 |
| `backend/go.mod`、`go.sum` | Go 模块、Gin 依赖版本及校验记录 |
| `backend/cmd/server/main.go`、`.env.example` | 本地／环境配置读取、无密钥模板、可信代理校验、本机 HTTP 监听与 SIGTERM 关闭 |
| `backend/internal/server/router.go` | Gin 路由、通用日志 / 异常恢复、健康检查和文生图入口 |
| `backend/internal/server/images.go` | 文生图输入校验、并发限制、返回契约与错误处理 |
| `backend/internal/ark/models.go` | Lite / Pro 模型白名单、ID 与默认模型 |
| `backend/internal/ark/images.go` | 方舟鉴权、Lite / Pro 参数差异、HTTP 调用、超时和结果解析 |
| `backend/internal/{ark,server}/images_test.go` | 模拟供应商验证请求、错误、超时、校验与并发，不调用真实模型 |
| `frontend/package.json`、`package-lock.json` | 前端依赖、锁定版本及运行命令 |
| `frontend/src/main.ts` | 挂载 Vue 应用、加载全局样式与 Vue Flow 必需样式 |
| `frontend/src/App.vue` | 按 URL 显示首页或指定画布，完整页面导航隔离会话，不引入路由库 |
| `frontend/src/views/CanvasPage.vue` | 画布、点阵、固定界面、视口、菜单、文件选择器、当前编辑节点、参考选择事件分流及屏幕 / 画布坐标转换 |
| `frontend/src/components/CanvasToolbar.vue` | 缩放比例、缩放按钮、重置操作 |
| `frontend/src/components/CanvasContextMenu.vue` | 右键菜单两层内容、屏幕边缘定位和关闭；发出上传 / 创建节点事件 |
| `frontend/src/components/MediaNode.vue` | 媒体预览、独立播放 / 进度控制、加载 / 错误、参考候选状态与连线端点 |
| `frontend/src/components/NodePromptPanel.vue` | 提示词面板、参考 / 缩略图、文生图按钮 / 状态 / 错误、放大 / 恢复和焦点 |
| `frontend/src/components/ImageModelSelect.vue` | 两个模型的下拉列表、选中状态、键盘 / 焦点、边缘定位与关闭 |
| `frontend/src/models/imageModels.ts` | Lite / Pro 展示名称、模型 ID 类型与默认值 |
| `frontend/src/components/ReferenceSelectionBar.vue` | 蓝色参考选择提示、允许类型 / 操作反馈、返回节点和结束选择 |
| `frontend/src/canvas/media.ts` | 媒体节点和引用预览数据模型、文件类型识别和预览尺寸计算 |
| `frontend/src/canvas/useMediaNodes.ts` | 节点 / 导入 / 提示词和模型草稿、生成快照与节点绑定、图片回填、状态 / 超时和资源清理 |
| `frontend/src/api/images.ts` | 浏览器文生图 HTTP 请求和响应校验，不包含模型凭据 |
| `frontend/src/api/assets.ts` | 保存能力配置、二进制上传、资源响应校验 |
| `frontend/src/canvas/useNodeReferences.ts` | 选择模式、引用规则、ID 关系维护、缩略图 / 连线派生、失效引用清理 |
| `frontend/src/canvas/viewport.ts` | 初始偏移和比例、最小 / 最大缩放 |
| `frontend/src/style.css` | 深色画布、固定工具栏、响应式布局和焦点样式 |
| `frontend/vite.config.ts`、`tsconfig.json` | 本地开发、`/api` 后端代理与类型检查配置 |
| `frontend/playwright.config.ts`、`tests/canvas.spec.ts` | 真实浏览器交互验收，独立测试端口 5174 |
| `frontend/tests/media-nodes.spec.ts`、`tests/fixtures/` | 节点 / 文件选择 / 解码 / 播放 / 拖动验收及非用户媒体测试样本 |
| `frontend/tests/prompt-panel.spec.ts` | 提示词弹出、放大 / 恢复、独立草稿、交互互不干扰与窄屏验收 |
| `frontend/tests/references.spec.ts` | 引用类型约束、多选 / 去重 / 移除、节点间独立性、端点随视口移动、退出 / 焦点 / 窄屏及素材失效验收 |
| `frontend/tests/generation.spec.ts` | 模拟生成、重复点击 / 草稿 / 结果归属、失败、约束、网络异常和引用更新联动 |
| `frontend/tests/image-to-image.spec.ts` | 已存参考可用性、参考顺序 / 请求快照、单图回填及模型数量边界 |
| `frontend/tests/image-models.spec.ts` | 模型下拉选择、节点独立性、Esc 分层关闭、键盘和窄屏定位 |
| `frontend/tests/setup.ts`、`storage.spec.ts` | 普通测试云请求隔离、上传队列 / 重试、转存成功 / 失败展示 |
| `frontend/scripts/verify-live-storage.mjs` | 显式启用的真实图片 / 视频上传、内容校验和 Range 验收；2026-09-22 已通过 |
| `frontend/scripts/verify-live-image-to-image.mjs` | 显式启用的单次真实图生图、OSS 转存和节点展示验收 |
| `frontend/scripts/verify-live-generation.mjs` | 显式开启才执行的单次真实生成验证，证据写入被忽略的 `artifacts/` |
| `design/` | 原有 Figma 设计与导入包；其状态以 `design/figma-import/README.md` 为准 |

## 按任务读取

- 发布 / 服务器维护：`docs/deployment.md` → `deploy/nginx/` / `deploy/systemd/` → `backend/.env.example`；配置中的实际密钥不进入文档或发布前端。

- 改首页 / 新建 / 切换画布：`docs/canvas-library.md` → `HomePage.vue` / `App.vue` → `useCanvasSession.ts` / `CanvasPage.vue` → `backend/internal/server/persistence.go` / `persistence/store.go`。

- 视频生成设计与实现：先看 `docs/video-models.md`，再看 `frontend/src/models/videoModels.ts`、`frontend/src/canvas/useMediaNodes.ts`、`backend/internal/persistence/generation_input.go`、`credits.go`、`backend/internal/ark/videos.go`；历史 v1 契约与先前验收见 `docs/video-generation-contract.md`、`docs/reference-generation.md`。

- 改数据库 / 保存 / 任务：`docs/persistence.md` → `backend/internal/persistence/` → `backend/internal/server/persistence.go` / `images.go` → `frontend/src/canvas/useCanvasSession.ts` / `useMediaNodes.ts`。

- 改后端启动 / 路由：`backend/README.md` → `cmd/server/main.go` → `internal/server/router.go`（后两个路径相对于 `backend/`）。
- 改文生图 / 图生图链路：`docs/image-generation.md` → `NodePromptPanel.vue` → `useMediaNodes.ts` → `api/images.ts` → `backend/internal/server/images.go` → `backend/internal/ark/images.go`。
- 改可选模型：`frontend/src/models/imageModels.ts` 与 `backend/internal/ark/models.go` 同步维护；交互见 `ImageModelSelect.vue`，供应商参数适配见 `backend/internal/ark/images.go`。
- 改文件存储：`docs/object-storage.md` → `frontend/src/api/assets.ts` / `useMediaNodes.ts` → `backend/internal/server/assets.go` / `images.go` → `backend/internal/storage/`。
- 查模型配置 / 接口错误：`backend/README.md` → `.env.example` → `internal/ark/images.go`；不要输出实际 `.env.local` 凭据。
- 改平移 / 缩放行为：`CanvasPage.vue` → `viewport.ts` → `tests/canvas.spec.ts`。
- 改工具栏：`CanvasToolbar.vue` → `style.css`。
- 改右键菜单：`CanvasContextMenu.vue` → `CanvasPage.vue` → `tests/canvas.spec.ts`。
- 改节点显示：`MediaNode.vue` → `media.ts` → `tests/media-nodes.spec.ts`。
- 改提示词输入面板：`NodePromptPanel.vue` → `CanvasPage.vue` → `useMediaNodes.ts` → `tests/prompt-panel.spec.ts`。
- 改参考规则 / 引用数据：`useNodeReferences.ts` → `media.ts` → `CanvasPage.vue` → `tests/references.spec.ts`。
- 改参考展示 / 连线：`ReferenceSelectionBar.vue`、`NodePromptPanel.vue`、`MediaNode.vue` → `useNodeReferences.ts` → `tests/references.spec.ts`。
- 改文件导入 / 节点数据：`CanvasPage.vue` → `useMediaNodes.ts` → `media.ts` → `tests/media-nodes.spec.ts`。
- 改启动 / 构建：`frontend/package.json` → `vite.config.ts` → `README.md`。
- 了解原型：仅在需要时读取 `design/figma-import/README.md` 和相关设计文件。

OSS 保存文件，PostgreSQL 保存画布快照、资源元数据和任务历史。首页可新建或打开独立画布，恢复草稿、模型、视口、关系及已存素材，并继续查询任务。临时本地文件和未转存的供应商链接不能承诺长期恢复。现已通过邮箱密码登录；未配置 DATABASE_URL 时登录和业务接口不可用。画布、素材和任务均检查当前用户所有权。实际状态见 `docs/canvas-library.md`、`docs/persistence.md`、`docs/object-storage.md` 和 `backend/README.md`。
