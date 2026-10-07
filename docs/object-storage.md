# 对象存储接入

2026-10-05：素材上传和读取（含 Range）已加入 Session 与数据库所有权校验；私有预览响应为 no-store。已清空旧画布关联的素材记录，原私有 OSS 文件保留但不再通过应用访问。当前权限与本轮验证边界见 [登录与鉴权](./authentication.md)。

2026-10-03：已修复 OSS 连接超时过短、写入失败原因被统一隐藏的问题。连接及 TLS 握手超时调整为 15 秒，仅对建立 HTTP 连接前的可恢复故障最多尝试 3 次；保存接口返回具体错误类别并记录脱敏诊断。真实图片 / 视频上传、内容一致性和视频 Range 已重新验收，细节见下方“上传失败排查与恢复”。

2026-10-02：已扩展 WAV/MP3 上传、真实媒体参数及视频参考单文件校验；音频大小≤15000000字节，路径类型增加 `audios`，存储配置接口增加 `maxAudioBytes`，资产响应增加 `media` 和 `videoReference`。真实音视频 Range 与刷新播放通过，详见 [素材与引用](./media-references.md)。

云厂商已确认为阿里云 OSS。代码已接入 Go SDK V2 v1.6.0、本地文件保存、生成结果转存与私有文件预览；目录分配、接口及 SDK 协议已通过模拟测试。2026-09-22 已在本地配置私有 Bucket `bubu2`、地域 `cn-hangzhou`、外网 Endpoint 和凭据，启用 `OSS_ENABLED=true` 并启动后端。真实图片、视频上传、读取内容一致性及视频 Range 验证通过。2026-09-23 已完成 Lite 真实图生图、结果转存和展示，详见 [图片生成说明](./image-generation.md)；关闭 OSS 时继续原有本地预览及供应商图片地址模式。

## 需要提供的配置

| 配置 | 用途 |
| --- | --- |
| 云厂商 | 已确认阿里云 OSS |
| Bucket 名称 | 选择实际保存资源的存储桶 |
| Region | Bucket 所在地域，例如阿里云 `cn-hangzhou` |
| Endpoint | 可不填，由 Region 推导公网 API 地址；自填时为 HTTPS OSS 服务地址，不包含 Bucket 名称和文件路径 |
| 访问凭据 | 后端 RAM / IAM 子账号的 AccessKey ID、Secret；使用临时凭据时还需 Security Token |
| Bucket 访问权限 | 建议私有；由后端授权读取，不为展示媒体而开放整个桶 |
| 自定义域名 | 当前通过后端预览私有文件，不需要先绑定域名，也不需要给浏览器配置 OSS CORS |

凭据只放在被 Git 忽略的 `backend/.env.local` 或后端运行环境中，不放前端、公开文档或日志。存储凭据与已有方舟模型 API Key 是两套独立凭据。已有配置文件不要整份覆盖，以免丢失模型配置。

已在 `backend/.env.local` 配置以下项目并启用，保留原有方舟配置。下方不展示实际凭据，空白密钥项不代表本地文件未填写。新环境需自行配置 AccessKey ID / Secret 并重启后端；明确启用但缺少必填项时，服务会报错退出，避免静默不保存：

```dotenv
OSS_ENABLED=true
OSS_REGION=cn-hangzhou
OSS_ENDPOINT=https://oss-cn-hangzhou.aliyuncs.com
OSS_BUCKET=bubu2
OSS_ACCESS_KEY_ID=
OSS_ACCESS_KEY_SECRET=
OSS_SESSION_TOKEN=
STORAGE_PREFIX=frame-space
STORAGE_ENV=dev
```

地域、凭据与客户端配置以[阿里云 OSS Go SDK V2 文档](https://help.aliyun.com/zh/oss/developer-reference/manual-for-go-sdk-v2/)为依据；杭州外网 Endpoint 已对照[地域和 Endpoint](https://help.aliyun.com/zh/oss/user-guide/regions-and-endpoints)核实。当前只使用 `PutObject` 和 `GetObject`，权限模板见 `backend/oss-policy.example.json`：已限定到 `bubu2/frame-space/dev/*`，可用于给后端使用的 RAM 用户授权；后续改 Bucket、`STORAGE_PREFIX` 或 `STORAGE_ENV` 时需要同步调整。这里只准备本地权限模板，尚未执行云端授权，不会修改桶策略、公开访问权限、生命周期规则或删除已有文件。操作权限参考 [PutObject](https://help.aliyun.com/zh/oss/developer-reference/putobject) 与 [RAM Policy](https://help.aliyun.com/zh/oss/user-guide/ram-policy/)。

## 已实现的文件组织规则

统一格式：

```text
{项目前缀}/{环境}/canvases/{画布ID}/{来源}/{媒体类型}/{节点ID}/{资源ID}.{扩展名}
```

例如：

```text
frame-space/
  dev/
    canvases/
      <画布ID>/
        uploads/
          images/<节点ID>/<唯一资源ID>.png
          videos/<节点ID>/<唯一资源ID>.mp4
          audios/<节点ID>/<唯一资源ID>.wav
        generated/
          images/<节点ID>/<唯一资源ID>.webp
          videos/<节点ID>/<唯一资源ID>.mp4
  prod/
    canvases/...
```

`generated/videos` 已用于第二阶段的视频生成结果，当前实现和验收见 [引用生成第二阶段](./reference-generation.md)。

- 开发与生产分开，上传原件与生成结果分开，图片与视频分开。
- 同一画布的素材集中在该画布目录，进一步通过节点 ID 追溯。
- 每次分配新的资源 UUID，不使用用户输入的原始文件名作为存储路径，不覆盖同名上传或以前的生成结果。
- 检查目录前缀、画布 / 节点 ID、来源和媒体类型，拒绝路径穿越；调用方仍必须检查实际文件内容，扩展名校验不能代替内容校验。
- 当前没有用户账号体系。画布 / 节点 ID 用于组织资源，不代表访问权限；多用户上线前需要服务端所有权校验。

实现：`backend/internal/storage/keys.go`。对象同时保存 `canvas-id`、`node-id`、`source`、`asset-file` 元数据；生成结果还记录 `model`。上传时声明私有 ACL、标准存储并禁止覆盖。提示词、名称、节点坐标和引用关系保存到 PostgreSQL 画布快照；Object Key 等资源信息登记在 assets，文件内容仍在 OSS，详见 [持久化说明](./persistence.md)。

## 已实现的流程

本地导入：浏览器验证可预览并保留本地 `blob:` 地址 → 查询保存功能是否启用 → 最多两个文件同时经后端上传 → 后端验证内容类型和大小 → 分配 Object Key → 上传到 OSS → 前端记录 `asset`，移除“保存中…”提示。成功后不显示“已保存”角标，继续复用本地预览，避免视频重新加载和重复下载文件。失败保留预览，可点击“重新保存”。读取配置失败会明确提示，不会误报保存成功。

文生图：先保存画布 / 节点 → 创建持久化任务 → 后台调用方舟 → 记录临时结果 → 转存图片 → 事务登记资源与任务 → 前端查询并在原节点显示图片。成功后不显示“已保存”角标；转存失败时任务仍记录已生成图片及 `storageError`。生成状态和文件保存状态分开，不重新调用模型。当前仅本地上传提供重新保存按钮；生成结果转存失败会提示及时下载，点击“生成图片”是一次新的生成。

第一阶段采用后端中转上传及预览，便于统一校验并读取私有文件，无需长期有效的公共链接。视频 `Range` 请求转发到 OSS 并返回 `206 / Content-Range`，支持进度跳转。代价是文件传输经过后端，暂未做浏览器直传、分片、断点续传和 CDN。

图片上限 20 MiB，视频上限 200 MiB，音频上限 15 MB（15000000字节）；支持 PNG、JPEG、WebP、GIF、AVIF、BMP、MP4、WebM、MOV、M4V、OGV、WAV、MP3，是否能在浏览器播放另取决于编码。后端识别实际文件头，不信任扩展名或客户端 MIME；音视频还通过 ffprobe 读取真实参数，SVG / HTML 不进入云端保存。普通上传范围宽于视频模型参考范围，不合格参考可保存播放但显示原因。临时文件位于后端 `.storage-tmp/`，正常完成或失败后清理；进程被强制结束时可能留下本次临时文件。前后端分别限制同时保存数量为 2，图片生成接口仍同一时间仅处理 1 次。

只下载后端模型返回的 HTTPS 地址，不提供任意 URL 转存接口；拒绝重定向、内网 / 回环地址，校验 DNS 后直连公网 IP，限制大小和耗时。密钥、供应商错误原文和签名地址不写日志。参考准备最多 30 秒、模型 180 秒、转存 90 秒；前端轮询任务。历史临时模式的长请求不再通过当前业务入口提供。上传请求最多等待 125 秒。

### Nginx 上传大小配置（已部署）

2026-10-07：新增 [frame-space-upload-size.conf](../deploy/nginx/frame-space-upload-size.conf)，将媒体上传路由的 Nginx 请求体上限设置为 `200m`（200 MiB，209715200 字节），与后端 `MaxVideoBytes = 200 << 20` 一致。当前上传使用 `application/octet-stream` 直接发送单个文件，不包含 multipart 表单额外开销。

该片段已在历史部署中安装到 Nginx snippets 目录，并在本项目 `location = /api/assets` 中引入。后端监听 `127.0.0.1:8080`；公开仓库中的域名 `studio.example.com` 是占位符。HTTPS、可信代理头和超时配置见 [站点示例](../deploy/nginx/studio.example.conf) 与 [部署指南](./deployment.md)。

```nginx
location = /api/assets {
    include /etc/nginx/snippets/frame-space-upload-size.conf;
    include /etc/nginx/snippets/frame-space-proxy.conf;
    proxy_request_buffering off;
}
```

Nginx 先检查请求体大小，超过 200 MiB 时返回 413；通过后仍由 Go 检查身份、素材归属、真实文件类型和分类大小限制，图片 20 MiB、音频 15 MB 的限制继续有效。此片段仅用于本项目的上传路由，不应放入所有站点共用的 `http` 配置块。指令说明见 [Nginx 官方文档](https://nginx.org/en/docs/http/ngx_http_core_module.html#client_max_body_size)。

2026-10-07 实际验收：`nginx -t` 和重新加载通过；公网 `Expect: 100-continue` 请求声明 200 MiB 时收到 100 Continue，声明 200 MiB + 1 字节时收到 413，测试没有发送大文件请求体。另已通过正式 HTTPS 接口完成小 PNG 上传到 `frame-space/prod/`、完整读取哈希一致及 Range 206 验证，并清理测试对象。尚未实际上传超过 1 MiB 的合法文件或完整 200 MiB 视频；Go 上传预算仍为 120 秒、前端等待 125 秒，代理超时设置 150 秒不能消除慢网限制。

## HTTP 契约

| 接口 | 请求 / 返回 |
| --- | --- |
| `GET /api/storage/config` | 仅返回 `enabled`、图片 / 视频大小上限，不返回 Bucket 或密钥 |
| `POST /api/assets?canvasId=<UUID>&nodeId=<UUID>` | `Content-Type: application/octet-stream`，请求体为单个文件原始字节，成功返回 201 和资源信息 |
| `GET /api/assets/content?key=<Object Key>` | 由后端读取私有对象；只接受当前项目前缀 / 环境内符合规则的资源路径，支持单段 Range |
| `POST /api/images/generations` | 接收 `prompt/model/canvasId/nodeId/referenceKeys?`；数据库模式额外要求 `taskId`，返回 202 任务对象；参考图需已保存，后端签发短时读取地址 |

资源结构包含 `key`、`url`、`kind`（`images/videos`）、`contentType`、`bytes`、`source`（`uploads/generated`）、`canvasId`、`nodeId`。保存成功的任务 `result` 包含 `asset`，`url` 指向需登录并验证归属的 `/api/assets/content?key=...`；保存失败保留供应商 `url` 并包含 `storageError`。

接口错误使用 `error.code` / `error.message`，包括无效归属或路径 400、不支持类型 415、超限 413、存储繁忙 429、未启用 503。OSS 超时返回 504，网络 / DNS / 证书问题返回 502，凭据 / 权限 / 桶配置及上游服务异常返回 503；详细分类如下。

## 上传失败排查与恢复

实际链路是：浏览器保留本地预览 → 保存画布节点 → `POST /api/assets` → 后端校验并暂存文件 → OSS `PutObject` → PostgreSQL 登记 assets → 返回资源信息。能看到本地预览不代表文件已经写入 OSS；原提示“保存到 OSS 失败，请检查网络、存储桶配置及写入权限”只说明 `PutObject` 失败，不能直接判定是哪一种原因。

2026-10-03 本次排查找到同一 PNG 两次上传失败，日志时间为 13:27:15、13:27:26，均约 5.02 秒返回 502。原客户端没有设置连接超时，使用 OSS SDK V2 v1.6.0 默认的 5 秒，也没有记录底层失败类别。修改前，同一运行环境已能读取旧对象并成功写入测试 PNG，说明当时验证的凭据、桶和写入权限可用。结合失败耗时，更可能是短暂连接 / TLS 握手超时；旧日志证据不足，无法确定具体网络阶段，也不能证明网络问题已永久消失。本次没有修改密钥、桶策略或公开访问权限。

修复位于 `backend/internal/storage/oss.go`、`oss_errors.go` 和 `store.go`：

- 明确设置连接及 TLS 握手超时为 15 秒，读写超时仍为 60 秒；上传整体预算仍为 120 秒，前端等待上限仍为 125 秒。
- SDK 通用重试仍关闭；应用只在尚未获得 HTTP 连接、输入可回卷且故障可恢复时重试，最多 3 次尝试，间隔为 300 / 600 毫秒。每次使用同一个对象 Key、原始字节和禁止覆盖设置。
- 已获得连接后的超时可能意味着文件已送达但响应丢失，因此不自动重传；凭据 / 权限错误、永久 DNS 错误、证书错误和请求取消也不重试。该策略同时适用于本地上传与生成结果转存，不触发新的模型调用。
- 保留存储层的安全错误码，前端沿用已有错误提示和“重新保存”。后端只记录错误类别、状态、经过过滤的供应商错误码 / Request ID、次数和耗时，不打印原始 SDK 错误、密钥或带签名地址。

| 错误码 | 含义 / 排查方向 |
| --- | --- |
| `STORAGE_TIMEOUT` | 连接或传输超时，检查后端网络及 VPN / 代理 |
| `STORAGE_DNS_FAILED` / `STORAGE_NETWORK_FAILED` | 地址解析或网络连接失败 |
| `STORAGE_TLS_FAILED` | HTTPS 证书验证失败，检查系统时间、代理和证书配置 |
| `STORAGE_ACCESS_DENIED` | OSS 拒绝访问，检查后端账号授权和桶策略 |
| `STORAGE_CREDENTIALS_INVALID` | 密钥无效、签名不符或临时令牌过期 |
| `STORAGE_BUCKET_CONFIG_INVALID` | 桶名称、地域或 Endpoint 不匹配 |
| `STORAGE_CLOCK_SKEW` | 后端系统时间与 OSS 不一致 |
| `STORAGE_ACCOUNT_UNAVAILABLE` / `STORAGE_SERVICE_UNAVAILABLE` | 账号状态、额度、请求受限或上游服务异常 |
| `STORAGE_OBJECT_EXISTS` | 禁止覆盖保护生效；没有覆盖旧对象 |
| `STORAGE_CANCELED` | 请求已中断 |

当前页面仍保留原文件时，可在失败节点点击“重新保存”；如果已经刷新，浏览器中的本地文件引用会丢失，需要重新选择原文件。OSS 与 PostgreSQL 仍没有跨服务事务；响应丢失后手动再次保存仍可能留下孤立对象，本次未增加对象清理或上传幂等协议。

本次验证：完整 Go 测试（包含独立 PostgreSQL 测试库）、`go vet` 和后端构建通过；5 项浏览器存储回归通过，覆盖超时 / 权限信息、保留预览和手动重试不调用模型。模拟测试确认连接失败后恢复、最多 3 次尝试、已连接后的不确定写入不自动重试及日志脱敏。18:26（北京时间）真实浏览器上传 PNG 54,178 字节和 MP4 75,720 字节均返回 201，从 OSS 读回 SHA-256 一致，视频 Range 返回 206 且字节匹配；证据为 `frontend/artifacts/storage-live.json` / `.png`，诊断画布为 `6b912181-754c-45a0-a65e-cca760c91c51`。真实服务验证未人为干扰网络，连接故障恢复分支由模拟测试验证；未调用付费模型。

## 验证与边界

后端模拟测试覆盖路径分层、内容与大小校验、临时文件清理、私有 ACL、SDK 请求、Range、转存失败保留结果、错误脱敏及下载地址限制。浏览器新增 4 项模拟测试覆盖批量保存、保存失败重试、已存生成图片展示及转存失败提示。普通浏览器测试统一拦截云请求，不使用真实模型或存储。

真实上传验收脚本为 `frontend/scripts/verify-live-storage.mjs`：配置并启动后，在 `frontend/` 显式设置 `RUN_LIVE_STORAGE=1` 后执行。它从前端上传一张测试图和一个小视频，再从后端读取验证 SHA-256 一致性及视频 Range，证据写入被忽略的 `artifacts/storage-live.json` / `.png`。上传响应另存 `artifacts/storage-live-uploaded.json`，便于后续读取校验失败时仍能定位资源。每次运行会保留两份测试资源，不删除已有文件。

2026-09-22 16:11（北京时间）真实验收通过：PNG 54,178 字节、MP4 75,720 字节，两个上传请求均为 201，私有资源完整读取与源文件 SHA-256 一致，视频 `bytes=0-31` 返回 206 且内容匹配；浏览器两个节点均显示“已保存”。资源位于 `frame-space/dev/canvases/ea544c25-9a3b-46ee-87dd-870466cb142e/uploads/` 下的图片 / 视频各自节点目录，完整 Key 见验收记录。

此前一次验收已成功上传两个文件，但脚本延迟读取网络响应时遇到 Chrome 调试缓存清理，未完成内容校验；该次资源保留在画布目录 `2c165e73-d155-4951-a7e5-8be653e84060`。修复方式是仅在验收浏览器中及时复制真实上传响应，避免依赖稍后可能被清理的调试缓存，不改变产品上传协议。本轮共保留四个小型测试文件，没有删除对象或变更云端权限。

真实文生图转存可使用已有 `verify-live-generation.mjs`，需显式允许模型调用，成功时记录 `storedAssetKey`。历史 Lite / Pro 真实生成记录发生在 OSS 接入前，不能作为本轮 OSS 验证依据。

OSS 负责文件，PostgreSQL 负责账号、Session、画布、资源记录与生成任务。刷新后先恢复登录身份，再使用地址中的固定画布 ID 读取当前用户的快照和资源。上传前先保存节点，后端校验画布所有权，上传成功再登记 assets；任务完成与生成资源登记在同一个数据库事务中提交。OSS 和数据库之间仍无跨服务事务：写文件后崩溃或上传响应丢失，可能留下孤立 / 重复资源；没有对象列表和旧版本回收。数据库不可用时业务入口关闭。素材预览与 Range 读取均检查资源登记和画布所有者，旧的未登记对象不能经应用读取。服务仅监听本机。
