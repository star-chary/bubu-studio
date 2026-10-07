# 部署指南

本文根据项目已有的 Nginx、systemd、PostgreSQL 和 OSS 运行方式整理。域名 `studio.example.com` 为占位符，部署前应替换为自己的域名；具体服务器地址、个人路径和原始发布记录仅保留在本地，不包含在公开仓库中。

## 请求与数据流程

```text
浏览器 → HTTPS / Nginx
             ├─ 静态页面 → frontend/dist
             └─ /api、/health → Go / Gin（127.0.0.1:8080）
                                      ├─ PostgreSQL：账号、画布、任务、积分
                                      ├─ 火山方舟：模型生成
                                      └─ OSS：上传素材与生成结果
```

只有 Nginx 暴露公网入口。Go、数据库与云服务之间的访问由服务器管理；Ark 和 OSS 凭据不进入前端构建产物。

## 仓库模板

| 文件 | 用途 |
| --- | --- |
| [studio.example.conf](../deploy/nginx/studio.example.conf) | 独立 HTTPS 站点模板，需替换域名和证书位置 |
| [frame-space-proxy.conf](../deploy/nginx/frame-space-proxy.conf) | 同机 API 代理、请求头和超时设置 |
| [frame-space-upload-size.conf](../deploy/nginx/frame-space-upload-size.conf) | 上传路由请求体上限 |
| [frame-space.service](../deploy/systemd/frame-space.service) | Go 服务用户、运行目录、环境文件和重启策略 |
| [后端环境变量模板](../backend/.env.example) | 复制到服务器受保护位置后填写实际配置 |

模板沿用 `frame-space` 作为 Linux 服务名、运行目录名。`systemd` 示例依赖 `postgresql@16-main.service`，适用于对应的 PostgreSQL 16 安装方式；使用其他系统或数据库服务时需要调整。模板不能直接视为所有服务器均已验证的自动部署脚本。

## 目录职责

```text
/opt/frame-space/releases/<发布版本>/
  frontend/                    # 前端构建产物
  backend/frame-space          # Linux 后端程序
/opt/frame-space/current        # 指向当前发布版本的链接
/etc/frame-space/backend.env   # 仅服务器保存的真实环境变量
/var/lib/frame-space/          # 运行目录及 .storage-tmp 暂存
```

程序、秘密配置和运行数据放在不同位置，避免更新代码时覆盖配置或业务数据。数据库底层文件由 PostgreSQL 管理，媒体文件位于自己的 OSS 桶中。

## 配置与构建

1. 安装 PostgreSQL 16、Nginx、ffprobe，创建独立的数据库角色和数据库。
2. 创建 `frame-space` 系统用户及运行目录。按服务模板准备程序和配置路径，并保证该用户对工作目录可写。
3. 将后端配置模板复制为 `/etc/frame-space/backend.env`，由 root 持有，权限限制为 `600`。填写数据库、Ark、OSS 凭据；生产站点配置 `APP_ORIGINS=https://studio.example.com`、`AUTH_COOKIE_SECURE=true`、`TRUSTED_PROXIES=127.0.0.1`，并设置独立的 `STORAGE_ENV=prod`。
4. 在 `frontend/` 运行 `npm ci`、`npm run build`。构建期间只使用公开前端配置。
5. 在 `backend/` 编译目标 Linux 架构的程序，例如 Linux amd64 构建环境中执行 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/frame-space ./cmd/server`。跨平台时根据实际 shell 设置环境变量。
6. 将产物上传到独立发布目录，核对文件摘要；原始 `.env.local`、数据库目录、私钥和日志不应打包。
7. 按模板安装 systemd 单元与 Nginx 配置。证书链与私钥通过独立受保护渠道安装；私钥应由 root 持有并限制权限。
8. 在检查通过后启用服务。修改 Nginx 配置先执行 `nginx -t`，修改 systemd 单元后执行 `systemctl daemon-reload`。

后端启动时自动执行数据库迁移，因此首次启动及升级前需要确认连接的数据库和备份策略。生成任务依赖数据库级执行器锁，当前架构不能通过启动第二个同库后端来实现无缝接管。

Nginx 覆写 `X-Forwarded-For`，后端只信任同机代理。不要将受信代理设置为所有来源。上传路由单独允许 200 MiB，并关闭请求体缓冲；请求头上限通过不代表慢网下完整大文件上传已经验收。

## 发布与回退

1. 完成修改相关测试、前端构建和后端目标平台编译。
2. 记录当前版本、检查活动生成任务、备份 PostgreSQL，并核对备份可用性。
3. 准备新版本目录；停止旧 Go 服务后切换 `current`，再启动服务。
4. 检查日志、健康接口，以及此次修改相关的登录、画布、素材等业务链路。
5. 故障时先停止新服务，确认旧程序兼容已执行的数据库迁移，再切回旧版本。

回退程序不会撤销数据库迁移。涉及数据结构时，应独立设计兼容和数据恢复步骤。已有后台任务的恢复取决于状态：已保存供应商任务 ID 的视频可以继续查询，不确定的提交阶段需要核查，避免重复付费。

推送代码到 GitHub 与发布到服务器是两项操作；本仓库没有配置 GitHub Actions 自动部署。

## 验收与维护

- `/health` 返回 200 仅说明 HTTP 服务响应。
- 独立验证注册／登录、Cookie、会话失效、CSRF 和私有资源权限。
- 验证画布保存／重读、素材上传／完整读取／Range 和浏览器刷新。
- 真实图片／视频生成会调用自己的付费供应商账户，必须独立检查结果和用量。
- 明确备份保留周期、恢复演练、日志轮转以及证书续期责任。
- 修改环境变量后需重启后端；更新证书后需校验配置并重新加载 Nginx。

历史部署已取得 HTTPS 入口、身份与画布 API、PNG 上传读取、公开页面渲染等验证；不能由此推断所有模型、完整大文件上传、长期运行或备份恢复均已通过。本指南不执行部署，也不携带线上配置。
