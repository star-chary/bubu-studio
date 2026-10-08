# BuBu-后台管理

独立 Vue 3 / TypeScript / Vite 项目，与 `frontend/`、`backend/` 并列。提供管理员登录、用户搜索与分页、测试积分发放和积分记录。复用现有 Go 服务与 PostgreSQL。

## 本地启动

先按仓库 README 启动数据库和后端，再在本目录执行：

```powershell
npm.cmd ci
npm.cmd run dev
```

打开 `http://127.0.0.1:5176/admin/`。API 默认代理到 `http://127.0.0.1:8080`。如果后端显式配置 `APP_ORIGINS`，需包含管理端实际访问来源，例如 `http://127.0.0.1:5176`。切换 API 目标使用 `API_PROXY_TARGET` 环境变量。

只接受已授权的管理员账号，不在此入口注册或分配角色。首个管理员需要在后端应用 `007_admin.sql` 后，通过 `cmd/set-admin` 指定已核对的用户 ID；具体操作见 [管理端说明](../docs/admin.md)。密码沿用该账号现有密码。

## 构建与验证

```powershell
npm.cmd run build
npm.cmd run test:e2e
```

构建输出 `admin/dist/`，资源基址为 `/admin/`。当前采用管理页面、画布和 API 同源部署，Nginx 示例已包含 `/admin/` 配置。2026-10-08 已随 `20261008T055817Z` 发布到正式站点，现有域名和 HTTPS 证书继续使用。

访问正式站点 `/admin/`，使用已授权账号的原有密码登录。页面通过线上 `/api/admin/*` 操作线上数据库，不需要手动同步用户；本地开发默认仍连接本地数据。线上管理员需单独按已核对的用户 ID 初始化，本地同邮箱账号的权限不会自动同步。

6 项 Playwright 回归会拦截全部 API，不触碰业务数据。真实数据库验收脚本 `scripts/verify-live-admin.mjs` 需显式提供本机 `frame_space_test` 的 `TEST_DATABASE_URL`，以及刚编译的 `ADMIN_TEST_BACKEND_BIN`、`ADMIN_TEST_INIT_BIN`（相对 `backend/` 或绝对路径）。当前脚本使用 Windows / Ubuntu-24.04 WSL 的 `psql`、Chrome 与 8082/5177/5178 空闲端口；自动创建、清理临时 schema，禁用 Ark 和 OSS，证据写入被忽略的 `artifacts/`。

2026-10-08：管理端类型检查、构建、6/6 项浏览器回归和真实 Chrome → Vite → Gin → 隔离 PostgreSQL 验收通过；原画布登录／积分 18/18 项回归通过。验证覆盖响应丢失后刷新重试仅入账一次、画布端余额同步、普通用户拒绝、CSRF 与退出会话撤销。桌面与 390px 窄屏已截图检查。

正式 HTTPS 已另行通过真实 Chrome 登录、用户查询、测试发放及防重、流水审计、画布余额读取、Cookie／CSRF／权限和退出检查；临时账号及其积分记录已清理，未改动既有用户余额，未调用模型或 OSS。完整发布记录见 [部署指南](../docs/deployment.md)。
