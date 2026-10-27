# 用户查询 MCP

本功能通过远程 Streamable HTTP 提供唯一的只读工具 `list_users`。实现位于现有 Go 后端，使用官方 Go MCP SDK v1.8.0；网页和 MCP 共用 `AdminUsersForActor → AdminUsers → PostgreSQL` 查询。授权码与 PKCE 由 go-oauth2 v4.6.0 处理，项目补充管理员身份、精确回调地址、资源绑定和事务存储。

## 请求与授权流程

```text
AI 应用 → /mcp → 401 + 受保护资源元数据地址
        → 查询 OAuth 元数据 → 注册 public client
        → 浏览器打开 /oauth/authorize
        → 现有 /admin/ 登录 → 返回授权确认页
        → 管理员确认 → 一次性授权码返回注册的回调地址
        → 客户端用授权码 + S256 PKCE verifier 换取访问令牌
        → Bearer Token 调用 /mcp 的 tools/list、tools/call
        → 验证令牌、resource、users:read、当前 active/admin
        → 共享管理员查询入口 → users + credit_accounts → MCP 结构化结果
```

模型生成工具名称与参数；AI 应用中的 MCP Client 负责请求。MCP Server 不调用大模型，也不调用 Ark 或 OSS。

管理端的 Cookie 仅用于登录和浏览器授权确认，不能作为 MCP Bearer Token。工具参数中不接收凭证、管理员身份或任意 SQL。`X-Session-User` 仍不能冒充身份。

## 工具契约

| 项目 | 内容 |
| --- | --- |
| 名称 | `list_users` |
| `q` | 可省略，默认空字符串；最多 254 字符；邮箱包含搜索或完整用户 UUID |
| `limit` | 可省略，默认 20；整数 1～100 |
| `offset` | 可省略，默认 0；整数 0～100000 |
| 排序 | 注册时间、用户 ID 倒序 |
| 返回 | `{users,total,limit,offset}` |
| 用户字段 | `id,email,role,status,createdAt,available,reserved` |
| 副作用 | 只读；不创建用户、不发放积分、不提交生成任务 |

拒绝额外参数、超限参数与显式 `null`。`total` 是匹配总数，`users` 仅是本页；下一页需要增加 offset。没有日期范围筛选。结果来自服务器查询，通过 `structuredContent` 和兼容文本内容返回，带输出 Schema。查询失败返回 MCP 工具错误，不当作空结果；底层 SQL、密码及凭证不对外返回。

调用参数示例（省略协议信封）：

```json
{"name":"list_users","arguments":{"q":"example.com","limit":20,"offset":0}}
```

## OAuth 接口与兼容范围

| 地址 | 职责 |
| --- | --- |
| `/.well-known/oauth-protected-resource/mcp` | resource、授权服务器、读取权限 |
| `/.well-known/oauth-protected-resource` | 同一元数据的根路径入口 |
| `/.well-known/oauth-authorization-server` | issuer、授权/兑换/注册/撤销地址和支持能力 |
| `POST /oauth/register` | 动态注册 public client，精确保存回调地址 |
| `GET /oauth/authorize` | 校验请求，要求管理员登录，显示授权页 |
| `POST /oauth/authorize` | 同源 + CSRF 校验，批准或拒绝授权 |
| `POST /oauth/token` | 一次性授权码 + S256 PKCE 换令牌 |
| `POST /oauth/revoke` | 客户端撤销其访问令牌 |
| `GET/POST /oauth/connections` | 管理员查看和撤销自己授予应用的授权 |

本版支持 `authorization_code`、`response_type=code`、`token_endpoint_auth_method=none`、`code_challenge_method=S256`，scope 固定 `users:read`。授权与兑换请求的 `resource` 必须等于完整 MCP URL，授权请求必须携带 `state`。只接受注册的精确回调 URL，允许 HTTPS 和本机回环 HTTP 地址，不接受通配符。

授权码有效 5 分钟，访问令牌有效 1 小时。没有 refresh token、密码授权或 client_credentials；过期后由客户端重新发起浏览器授权。动态注册客户端保留 90 天，到期后重新注册。需要客户端支持这组能力，不表示所有云端 AI 产品都已验收。跨源浏览器 JavaScript 不直接访问受保护入口，应使用 AI 应用的服务端 MCP Client 或受控代理；公开发现元数据允许跨源读取。

动态注册例子（`client_name` 是接入方自报名称，用户应核对授权页的回调地址）：

```json
{
  "client_name": "My AI App",
  "redirect_uris": ["https://your-ai-app.example/oauth/callback"],
  "token_endpoint_auth_method": "none",
  "grant_types": ["authorization_code"],
  "response_types": ["code"],
  "scope": "users:read"
}
```

支持 SDK 的现代协议和旧版 Streamable HTTP 客户端；本地验证覆盖现代 SDK 客户端以及 `2025-06-18` 初始化/发现/调用。未提供旧版独立 `/sse` 入口。

## 数据与安全机制

008 迁移新增两张授权表，保留原有用户和积分模型：

```text
mcp_oauth_clients（应用名称、精确回调地址、有效期）
       ↓ 一对多
mcp_oauth_tokens（授权码/访问令牌哈希、元数据、有效期、撤销时间）
       ↑ 多对一
users（原有用户；实时检查角色和状态）
```

凭证使用 32 字节随机数，数据库仅存 SHA-256 哈希；元数据禁止保存 Code/Access/Refresh 原文。外键随应用或用户删除清理授权记录。凭证签发时清理该账号的到期记录；注册时清理到期客户端，并限制客户端总量为 1000。

授权码校验、消费和访问令牌签发在一个 PostgreSQL 事务内完成。按用户/客户端的事务级锁串行处理兑换和撤销；错误 PKCE 会回滚，成功兑换后该授权码不能再次兑换。授权记录重启后仍有效。撤销某应用会同时撤销该管理员授予它的待兑换授权码和访问令牌。

每次 MCP 请求检查令牌有效期、资源、scope 和数据库中当前角色/账号状态；工具执行时再次检查管理员权限。退出浏览器 Session 与撤销应用授权是两个动作，退出管理台不会自动撤销已经授予的应用令牌。

HTTP 入口固定校验配置中的 Host；浏览器授权表单校验同源与 CSRF；授权页面禁止嵌入，回跳只允许本站的两个授权页面。请求体受限，按 IP 和管理员限流。Go 和 Nginx 不记录 OAuth/MCP 请求 URL 参数或凭证；成功工具日志只记录管理员 ID、分页和返回数量，不记录邮箱搜索条件或用户列表。

## 启用与接入

默认 `MCP_PUBLIC_URL` 为空，MCP/OAuth 入口关闭。正式部署设置：

```dotenv
MCP_PUBLIC_URL=https://studio.example.com/mcp
APP_ORIGINS=https://studio.example.com
AUTH_COOKIE_SECURE=true
TRUSTED_PROXIES=127.0.0.1
```

`MCP_PUBLIC_URL` 必须使用 `/mcp` 路径，不能有 query/fragment；其来源必须包含在 `APP_ORIGINS` 中。只有本机回环地址允许 HTTP。按 Nginx 模板把 `/mcp`、`/oauth/` 和三个元数据地址转发到后端。后台程序仍只监听本机，由 Nginx 提供 HTTPS；008 迁移随后端启动执行，发布前需备份数据库。

在支持上述 OAuth 能力的 AI 应用中填写完整 MCP URL，按浏览器提示用已有管理员账号登录并批准读取。随后可以询问“查询最近注册的 20 位用户”或“查找邮箱包含 example.com 的用户”。管理台的“AI 应用授权”入口可撤销授权。

也可使用 [MCP Inspector](https://modelcontextprotocol.io/docs/tools/inspector) 检查发现、授权和工具调用。不同客户端的配置界面不同；不要将管理员密码或浏览器 Cookie 填入工具参数。

本机调试使用管理端 Vite 的统一来源，例如 `MCP_PUBLIC_URL=http://127.0.0.1:5176/mcp`，同时加入 `APP_ORIGINS`。Vite 已代理 MCP、OAuth 与元数据路径，保留 Host，使浏览器登录和授权在同源完成。

## 验证记录

2026-10-18：真实隔离 PostgreSQL + Go SDK 验证发现、调用、搜索/分页/空结果、Schema 校验、额外工具拒绝、权限变化、过期/撤销/resource/scope 校验、错误 PKCE 回滚及 8 路并发单次兑换。查询故障被分类为工具错误；数据字段与凭证持久化检查通过。Go 全包测试、vet 和构建通过。

Chrome → Vite → Gin → 隔离数据库的实际浏览器流程验证登录回跳、授权页桌面/390px 手机布局、取消零签发、授权兑换、旧版协议初始化/调用和管理台撤销。没有浏览器脚本错误，未调用 Ark/OSS；临时 schema 已清理。证据位于忽略目录 `admin/artifacts/mcp/verification.json` 及截图。

复跑后端专项需要设置指向本机 `*_test` 数据库的 `TEST_DATABASE_URL`：`go test ./internal/server -run 'TestMCP|TestAdmin' -count=1`。浏览器实测脚本为 `admin/scripts/verify-live-mcp.mjs`，需要先构建 `backend/bin/mcp-test-server.exe`、`backend/bin/mcp-test-set-admin.exe`，使用本机 `frame_space_test` 数据库和空闲的 8083/5179/5180 端口；脚本只操作自己创建的 schema。

正式发布和线上验收以 [部署指南](./deployment.md) 的对应记录为准；本地测试不能代替具体第三方 AI 产品的最终接入验收。

2026-10-18 已发布 `20261018T111607Z`。正式 HTTPS 的真实 Chrome 走通管理员登录、授权页、取消零签发、S256 授权码兑换、`initialize → tools/list → tools/call` 和撤销流程；MCP 查询限定临时账号，结果与原管理 API 完全一致，撤销后旧令牌返回 401。安全 Cookie、桌面 / 手机布局和浏览器脚本错误检查通过。验收账号、Session、注册积分、应用及授权记录已清理，没有模型 / OSS 调用。线上报告及截图保留在 `.data/mcp-release/production-verification.json` 和同目录中；尚未在具体第三方 AI 应用里完成添加与对话调用。
