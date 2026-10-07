# Bubu Studio · 视频创作工具

基于画布的 AI 图片与视频创作网站。通过节点组织素材、编写提示词、连接参考关系，并追踪生成任务与结果。

本仓库统一管理 Vue 前端、Go 后端、数据库迁移、部署模板和项目文档。

## 已实现的功能

- 多画布：命名创建、历史列表、自动／手动保存和刷新恢复。
- 媒体素材：上传图片、视频和音频，在画布中预览、播放及引用。
- 图片生成：Seedream Lite / Pro，支持文生图和图片参考。
- 视频生成：Seedance 多模型选择，支持文生视频和全能参考模式。
- 提示词引用：通过 `@` 插入素材，引用关系同步为画布连线。
- 持久化任务：排队、并发控制、状态查询和部分中断恢复。
- 账号与权限：邮箱密码登录／自动注册、私有画布、Session 和 CSRF 校验。
- 测试积分：生成前报价、冻结余额、交付后结算及失败处理。

当前没有支付或充值功能，新用户初始积分为 0。功能实现不代表所有模型均已完成真实付费验收，具体范围见 [验证边界](#验证边界)。

## 系统流程

```text
用户在 Vue 画布上上传素材、编辑提示词、点击生成
  ↓ HTTP /api 请求
Go / Gin：会话鉴权、归属检查、参数与报价校验
  ↓
PostgreSQL：保存画布、素材登记、任务状态、积分账户与流水
  ↓ 后台任务执行器
火山方舟：图片／视频生成
  ↓
阿里云 OSS：保存上传素材和生成结果
  ↓
后端更新任务和积分，前端轮询并展示结果
```

浏览器通过后端访问私有素材和生成服务；Ark、OSS、数据库凭据只由后端读取。正式部署由 Nginx 同源提供前端页面和 API 代理。

## 技术栈与目录

| 位置 | 内容 |
| --- | --- |
| `frontend/` | Vue 3、TypeScript、Vite、Vue Flow；浏览器测试使用 Playwright |
| `backend/` | Go 1.26、Gin、pgx；包含 SQL 迁移与后台任务 |
| `compose.yaml` | 本地 PostgreSQL 16 的 Docker 配置 |
| `deploy/` | Nginx 示例与 systemd 服务模板 |
| `docs/` | 功能流程、接口、数据模型与验证记录 |
| `design/` | 早期 Figma 原型资料，不等同于已实现功能 |
| `scripts/` | 本机数据库启动与测试素材生成辅助脚本 |

历史代码、数据库名和服务模板仍使用 `frame-space` / `frame_space` 标识；仓库名称不要求同步重命名这些运行标识。

## 本地运行

需要 Node.js 20.19+（20 系列）或 22.12+、npm、Go 1.26+、PostgreSQL 16。上传音视频时还需要 FFmpeg 中的 `ffprobe`。真实生成需要自己的火山方舟访问权限、API Key，以及配置正确的 OSS 存储。

### 1. 获取代码并准备配置

```bash
git clone git@github.com:star-chary/bubu-studio.git
cd bubu-studio
```

将 `backend/.env.example` 复制为 `backend/.env.local`，填写自己的配置。已有 `.env.local` 时不要覆盖。

| 后端配置 | 用途 |
| --- | --- |
| `DATABASE_URL` | PostgreSQL 连接；登录与业务接口必需 |
| `ARK_API_KEY` | 火山方舟 API Key；真实图片／视频生成必需 |
| `OSS_ENABLED` | 配好 OSS 后设为 `true` |
| `OSS_REGION`、`OSS_BUCKET` | 对象存储地域与桶 |
| `OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET` | 后端 OSS 访问凭据 |
| `OSS_SESSION_TOKEN` | 使用 STS 临时凭据时填写 |
| `FFPROBE_PATH` | 可选；未配置时从 PATH 查找 `ffprobe` |
| `APP_ORIGINS` | 允许访问的前端来源；模板包含两个本地开发地址 |
| `AUTH_COOKIE_SECURE` | 本地 HTTP 为 `false`，正式 HTTPS 必须为 `true` |

完整变量与默认值见 [后端配置模板](backend/.env.example)。任何真实密钥都不要放进前端或 `VITE_*` 变量。

### 2. 准备 PostgreSQL

可以连接已有 PostgreSQL，或使用仓库中的 Docker 配置：先把根目录 `.env.example` 复制为 `.env.local`，填写一个自己的强密码，再运行：

```bash
docker compose --env-file .env.local up -d postgres
```

然后在 `backend/.env.local` 中设置连接串，密码与根目录一致：

```dotenv
DATABASE_URL=postgres://frame_space:YOUR_PASSWORD@127.0.0.1:5432/frame_space?sslmode=disable
```

`YOUR_PASSWORD` 是占位符；如果密码含 URL 特殊字符，需要进行 URL 编码。`sslmode=disable` 仅适用于本机开发连接。

Windows + 已安装 PostgreSQL 16 的 Ubuntu-24.04 WSL 也可使用 `scripts/start-postgres-wsl.ps1`。该脚本有固定的 WSL 发行版和数据库命名约定，不能直接用于所有机器；不要与 Docker 同时占用 5432 端口。WSL 流程已有历史验证，Docker 配置尚未在本轮启动验收，详见 [持久化说明](docs/persistence.md)。

### 3. 启动后端

在仓库根目录打开终端：

```bash
cd backend
go mod download
go run ./cmd/server
```

后端默认监听 `127.0.0.1:8080`，从当前后端目录加载 `.env.local`，已有进程环境变量优先。连接数据库后自动运行版本化迁移。

健康检查为 `http://127.0.0.1:8080/health`。它只证明 HTTP 服务响应，不能代替数据库、OSS 和生成模型的业务验证。

### 4. 启动前端

另开终端，从仓库根目录运行：

```bash
cd frontend
npm ci
npm run dev
```

打开 `http://127.0.0.1:5173`。前端通过 Vite 将 `/api` 代理到本机后端。Windows PowerShell 若拦截 `npm.ps1`，可使用 `npm.cmd`。

注册／登录后创建画布。只有配置好模型、存储和测试积分，才能完成真实生成；画布可访问不代表新账号已有生成额度。

### 5. 本地测试积分

仅在自己的开发数据库恰好有一位用户时，可在 `backend/` 中运行以下命令预览发放：

```bash
go run ./cmd/grant-test-credits
```

确认连接的是自己的开发数据库后，加 `--apply` 为该用户幂等发放 200 测试积分。该工具不提供支付充值，也不支持多用户任意发放。详见 [测试积分](docs/test-credits.md)。

## 验证

在 `frontend/` 中运行：

```bash
npm run build
npm run test:e2e -- --workers=4
```

浏览器测试默认使用本机 Google Chrome；也可安装 Playwright Chromium 后通过 `PLAYWRIGHT_CHANNEL=chromium` 选择。浏览器测试使用模拟业务接口，不证明供应商账户、费用或真实生成结果。

在 `backend/` 中运行：

```bash
go test ./...
go vet ./...
go build ./...
```

部分集成测试只有设置 `TEST_DATABASE_URL` 才会运行。必须使用独立测试数据库，不能指向本机业务库或生产库。

### 验证边界

- 画布、登录与资源归属、持久化任务、测试积分已有分阶段本地或独立数据库验证，详情见各功能文档。
- 历史记录包含真实图片、视频生成和 OSS 转存验收；新增视频模型尚有账户授权与真实付费生成待验证项，见 [视频模型](docs/video-models.md)。
- 上传完整大文件、慢网、长期运行、备份恢复等需要独立验收。
- 本仓库不携带数据库内容、用户素材、真实配置或云服务访问额度。

## 配置与提交边界

`.gitignore` 排除真实环境配置、私钥、数据与备份、登录会话、日志、依赖和构建输出。仓库保留配置模板、源码、数据库迁移、依赖锁文件及自行生成的测试素材。

提交前检查 `git status` 和暂存区差异；不要使用 `git add -f` 强制添加真实配置。已被 Git 跟踪的文件不会因为增加忽略规则而自动移出历史。密钥如果泄漏，应先撤销／轮换，再清理历史。

## 文档入口

- [项目地图](PROJECT_MAP.md)
- [后端启动与接口](backend/README.md)
- [登录与鉴权](docs/authentication.md)
- [画布与持久化](docs/persistence.md)
- [对象存储](docs/object-storage.md)
- [素材引用与生成](docs/reference-generation.md)
- [任务队列](docs/generation-queue.md)
- [部署指南](docs/deployment.md)
- [开发过程与历史验证记录](docs/development-notes.md)
