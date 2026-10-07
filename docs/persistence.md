# PostgreSQL：画布保存、恢复与任务追踪

2026-10-05 队列更新：全站单活动任务限制已移除。默认视频并发 3、图片并发 1、结果转存并发 2，超出名额持久化等待；同节点仍限制一个未完成任务。具体领取、恢复与配置见 [生成队列](./generation-queue.md)。

2026-10-05：已新增 users / user_sessions，canvases 关联 owner_user_id。所有业务入口要求登录；无数据库不会降级为匿名操作。下文早期验证是历史记录，当前账号与权限契约见 [登录与鉴权](./authentication.md)。

2026-10-04：首页与独立画布已接入。`canvases` 新增名称和排序索引，支持分页列表、显式新建和按 ID 打开；不再从 localStorage 自动选用上次画布。保存后返回、跨画布隔离、升级保留旧数据及真实双画布验收通过，见 [首页与独立画布](./canvas-library.md)。

2026-10-03：第二阶段已接入提示词标签到模型输入的转换、不可变素材快照、Seedance 2.5 视频任务和只重试保存。前端构建、57 项浏览器回归、Go / PostgreSQL 集成测试及 vet 通过；真实 Lite 引用图片、4 秒 / 480p 有声视频生成和 OSS 转存、刷新播放均已验收。当前实现与真实验收状态见 [引用生成第二阶段](./reference-generation.md)。

2026-10-02 扩展：画布允许 `kind=audio, origin=upload`；音频不能携带生成草稿或引用其他节点，图片不能引用音频，视频可引用三类素材。媒体参数及视频参考条件随 Asset 写入现有 `assets.metadata` JSONB，无新增迁移。真实数据库测试与浏览器刷新恢复已通过，见 [第三步记录](./media-references.md)。

更新：2026-09-24。已接入真实 PostgreSQL，完成画布自动 / 手动保存、刷新恢复、后台图片任务、任务记录、失败追踪与后端中断处理。本机运行在 D 盘的 Ubuntu-24.04 WSL 内，PostgreSQL 16；Docker Desktop 本次启动失败，未作为实际数据库运行环境。

## 本机启动

在项目根目录运行：

```powershell
./scripts/start-postgres-wsl.ps1
```

脚本启动已安装的 PostgreSQL，创建项目用户与 `frame_space`、`frame_space_test` 两个数据库；生成的本地数据库密码放在被忽略的根 `.env.local`，连接写入 `backend/.env.local`，不覆盖已有模型 / OSS 凭据或已有非空数据库连接。WSL 分发存储位置为 `D:\WSL\Ubuntu`，数据库位于其中的 Linux `/var/lib/postgresql/16/main`。脚本通过隐藏的 WSL 保持进程避免分发自动退出，PID 记录在 `.data/postgres-wsl-keeper.pid`；Windows 重启后重新运行启动脚本。

另开终端运行 `cd backend; go run ./cmd/server` 和 `cd frontend; npm run dev`，页面为 `http://127.0.0.1:5173`。后端启动时自动执行嵌入的版本化迁移，迁移与版本登记在同一个事务中完成。数据库不可达或迁移失败时拒绝启动，不会静默改用内存。

如果需要停止本次本地数据库会话，先停止后端，再运行 `wsl -d Ubuntu-24.04 -u root --exec /usr/sbin/service postgresql stop`；之后可停止启动脚本创建并记录的 WSL 保持进程。不要执行 `wsl --shutdown` 来代替项目级停止，它会影响其他 WSL 工作。

Docker 备选配置为根 `compose.yaml`：`docker compose --env-file .env.local up -d postgres`，仅绑定 `127.0.0.1:5432`，数据目录为项目 `.data/postgres`。Docker 与 WSL 两种方案不能同时占用该端口；Docker 配置本次未实机验证。

其他机器使用 `DATABASE_URL=postgres://用户名:密码@地址:端口/数据库?...` 配置已有 PostgreSQL 即可。`sslmode=disable` 仅用于当前本机连接。登录功能启用后，清空 `DATABASE_URL` 会使登录与业务接口返回 503，不再进入匿名临时画布。

## 数据如何分工

| 表 | 内容 | 关系与约束 |
| --- | --- | --- |
| `canvases` | UUID、名称、版本号、JSONB 画布快照、更新时间 | 一个画布有多个资源和任务；快照包含节点、草稿、引用 ID、位置和视口 |
| `assets` | Object Key、画布 / 节点 ID、文件类型、大小、来源、读取地址、创建时间 | 画布外键；Object Key 主键避免同一资源重复登记，文件字节仍在 OSS |
| `generation_tasks` | taskId、画布 / 节点 ID、输入快照、状态、结果、错误、领取 / 创建 / 开始 / 完成 / 更新时间 | 画布外键；taskId 主键保证同一请求幂等；部分唯一索引限制同节点一个未完成任务 |
| `schema_migrations` | 已执行的迁移文件名与时间 | 迁移版本主键避免重复应用 |

节点和引用本阶段保存在 `canvases.snapshot` 的 JSONB 中，没有单独节点表或引用中间表。节点关系由后端校验：UUID 唯一、引用必须存在、不能引用自身、图片只能引用图片；最多 300 个节点、2 MiB 快照。`node_id` 对应快照中的逻辑节点 ID，不是数据库外键。任务和资源对画布的外键默认限制删除，当前不提供画布 / 节点删除 API，也不自动清理 OSS 历史文件。

JSONB 适合当前整张画布保存与恢复；若未来需要多人协同、逐节点并发编辑或跨画布查询节点，应再拆表。前端只提交可编辑草稿字段；生成状态、结果地址和资源信息来自后端独立记录，旧页面保存草稿不会覆盖后台任务结果。浏览器 `blob:` 地址与 Vue Flow 内部运行字段不入库。

## 保存与恢复流程

```text
编辑草稿 / 模型 / 引用、拖动节点、调整视口
 → 前端等待 600ms 无变化，或点击“保存”
 → PUT /api/canvases/:id { version, snapshot }
 → PostgreSQL 比较 version，一次更新快照并递增版本
 → 页面显示“画布已保存”

刷新页面
 → 只从地址 ?canvas=<UUID> 读取画布 ID，无 ID 时进入首页
 → GET /api/canvases/:id
 → 恢复节点 / 草稿 / 视口，查询节点最新资源和生成任务
 → 用 OSS Object Key 预览素材，继续轮询任务
```

保存请求串行执行，期间的新编辑会继续保存，不会倒序覆盖。两个页面持有相同旧版本时，仅一个保存成功，另一个收到 409 并停止覆盖，界面保留本地草稿，需复制需要的改动后刷新。数据库断开时显示保存失败，可点击“保存”重试；初次恢复失败会阻挡画布编辑，不会创建空画布覆盖旧内容。未保存编辑离开页面时使用浏览器离开提示，仍应以“画布已保存”为完成信号。

本地上传先保留即时预览，上传前确保节点已入库，上传成功后后端登记 `assets`。未上传完成就刷新，只能恢复节点占位并提示重新上传；仅保存画布不能保存浏览器本地文件。已存素材短暂预览失败不会删除持久化引用。

## 图片任务流程与一致性

```text
点击生成
 → 先保存画布，再提交唯一 taskId + 提示词 / 模型 / 有序参考 Key 快照
 → 后端校验、创建 queued 记录，返回 HTTP 202
 → 图片执行槽原子领取任务，写 preparing
 → 准备参考图地址 → 写 running → 调用方舟一次
 → 把模型结果持久化为 saving，释放生成槽，由独立转存槽保存 OSS
 → 在同一事务中登记资源并写 succeeded / 结果
 → 前端每 2 秒查询，回填原节点；任务记录展示输入与结果
```

同一个 taskId、相同输入重复提交返回原任务，改了输入则返回 409。任务 ID 在前端提交前生成，因此响应丢失时可用原 ID 查询或重交；重交复用同一 ID。每次新生成使用新 ID，历史记录不会被覆盖。画布草稿和任务输入分开，生成期间编辑草稿不会改变已经提交的模型输入。

任务执行器与 HTTP 请求上下文分离，刷新 / 关闭页面不取消后台任务。当前是单进程调度器内多个并行执行槽消费 PostgreSQL 任务表，没有 Redis 或独立消息队列。数据库会话 advisory lock 保证同一 schema 只有一个调度器，避免第二个后端误判现有任务中断；数据库锁连接丢失则停止执行器与 HTTP 服务。领取使用 FOR UPDATE SKIP LOCKED，在同一条 SQL 中更新 claimed_at 和准备状态。部分唯一索引只约束同节点的未完成任务，不能靠两个请求“先检查再写入”绕过。

| 状态 | 含义与恢复 |
| --- | --- |
| `queued` | 已接收，尚未调用模型；后端重启可继续执行 |
| `preparing` | 已领取、准备参考素材，尚未调用付费模型；重启回到 queued |
| `running` | 图片模型处理中；图片重启后标记 `interrupted`，不重新调用付费模型；视频恢复见生成队列文档 |
| `saving` | 模型结果已记录，正在转存；重启只继续保存，不重新生成 |
| `succeeded` | 模型成功；转存失败也保留临时结果及 `storageError`，不能声称文件已永久保存 |
| `failed` | 参考准备、模型配置或模型调用失败，记录中文错误 |
| `interrupted` | 后端在未拿到可持久化模型结果时中断，供应商可能已计费，用户核查后决定是否重试 |

模型调用不会自动重试；数据库结果写入可以重试。任务终态与资源登记使用事务，避免数据库内只成功一半。OSS 和 PostgreSQL 没有跨服务事务：进程在 OSS 已上传、数据库尚未登记时崩溃，恢复可能留下重复 / 孤立对象；当前不做自动清理。上传响应丢失后的手动重试也可能产生新资源。

## API

| 接口 | 契约 |
| --- | --- |
| `GET /api/persistence/config` | `{ enabled }`，不返回数据库连接或凭据 |
| `GET /api/canvases?limit=24&offset=0` | `{ canvases, hasMore }`；最近修改倒序，摘要含名称、节点数与布局，详见 canvas-library.md |
| `POST /api/canvases` | `{ id: UUID, title?: string }`；幂等创建空画布，名称默认“未命名画布”，最多 80 字；返回 201 与画布 |
| `GET /api/canvases/:id` | `id/title/version/snapshot/updatedAt/assets/tasks/results`，包含每节点最新资源、最新任务和最近成功结果 |
| `PUT /api/canvases/:id` | `{ version, snapshot }`，返回新 `version`；过期版本返回 409 |
| `POST /api/images/generations` | 原参数加必填 `taskId`，认证与归属校验后返回 202 与任务；数据库不可用时返回 503 |
| `GET /api/tasks/:id` | 查询单个任务，未知 ID 返回 404 |
| `GET /api/canvases/:id/tasks?limit=20&offset=0` | 返回 `{ tasks, hasMore }`，按创建时间倒序，limit 为 1～100 |
| `GET /api/canvases/:id/tasks?latest=true` | 返回每个节点的最新任务，供刷新与轮询恢复 |

快照节点格式：`{ id, position: {x,y}, data: {kind,name,prompt?,promptParts?,imageModel?,origin?,referenceIds?} }`；快照另含 `viewport: {x,y,zoom}`。2026-10-02 新增可选的结构化提示词片段 `promptParts`，沿用 JSONB；旧纯文本画布兼容，含引用片段的新生成任务暂返回 409 `PROMPT_REFERENCES_NOT_READY`，已有任务的幂等重查保持不变，详见 [提示词引用](./prompt-mentions.md)。任务返回 `id/input/status/result?/error?/createdAt/startedAt?/finishedAt?/updatedAt`；其中 `result` 沿用原生成结果结构 `url/model/size?/asset?/storageError?`。

## 已验证与边界

- `go test -timeout 90s ./...`：设置真实 `TEST_DATABASE_URL` 后覆盖迁移、快照读写、并发版本冲突、并发幂等提交、活动任务限制、任务结果 / 错误持久化、单执行器、重启中断、只恢复转存、Gin HTTP 完整链路；测试使用独立 schema 并清理，不触碰主库画布。未设置测试数据库时这些集成测试明确跳过。
- `go vet ./...`、后端构建、前端类型检查与生产构建通过。
- 40 项 Playwright 浏览器回归通过，其中新增 6 项持久化交互验收。普通回归拦截外部生成和存储请求。
- `frontend/scripts/verify-live-persistence.mjs` 已通过真实浏览器 → Go → PostgreSQL 保存与两次刷新恢复、失败任务 / 输入快照追踪；另实际重启后端进程，再查询确认原画布和任务仍在。使用独立 `frame_space_test`、端口 8081、空模型密钥与禁用 OSS，不调用付费模型。证据在忽略目录 `frontend/artifacts/persistence-live.json` / `.png`。
- 本次没有重新调用真实付费模型或重新上传 OSS；模型与 OSS 的历史真实验证仍是 2026-09-23，当前新任务链路中的成功 / 转存情况通过真实数据库配合模拟模型和对象存储验证。

当前仍仅在本机运行，已新增用户登录和跨用户资源权限；没有协作或自动工作流调度。地址中的 UUID 用于定位，不能替代权限控制；服务保持仅监听本机。临时供应商地址可能过期，只有成功存入 OSS 的文件具备相应恢复基础。

数据库连接与事务采用 [pgxpool 官方接口](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)，单执行器使用 [PostgreSQL advisory lock](https://www.postgresql.org/docs/16/explicit-locking.html#ADVISORY-LOCKS)。
