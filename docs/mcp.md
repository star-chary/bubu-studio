# 普通用户画布节点 MCP

2026-11-12：普通用户画布 MCP 和首页连接弹窗已随 `20261111T160133Z` 发布；工作区源码尚未提交或推送。远程 `/mcp` 提供八个工具：读取本人画布、创建和配置节点、查询模型、报价、提交及查询生成任务。原管理员 `list_users` MCP 工具已移除；管理台自己的用户查询接口不受影响。新增生成权限需要重新授权，原有令牌不会自动获得生成能力。

## 使用流程

首页侧边栏的“MCP 连接”可打开弹窗，复制包含当前网页来源 `/mcp` 地址的 AI 接入提示词。提示词不包含账号密码或令牌，也不会自动建立连接；使用时仍需在支持 MCP 的客户端中完成普通用户授权。地址要求当前站点实际启用了同来源的 MCP 服务。

```text
MCP 客户端连接 /mcp → OAuth 元数据和应用注册
→ 浏览器使用现有普通用户登录 → 确认授权读取、编辑画布和提交生成
→ 授权码 + S256 PKCE 换取独立 Bearer 令牌
→ AI 读取画布、选择模型、创建并配置节点
→ 报价后提交生成任务，查询任务状态
→ 校验令牌、读取 / 写入 / 生成权限、用户状态和画布归属
→ 后台沿用已有任务、积分与素材保存流程
→ 用户重新打开画布查看
```

浏览器 Cookie 和 MCP Bearer 令牌各自独立，工具参数不接受用户 ID。画布 ID 可由列表取得，也可从 `/?canvas=<UUID>` 链接中获取。当前只有持久化画布可用，临时画布和网页中未保存的内容不可见。

## 工具契约

### `list_canvases`：查看本人的画布

可选 `limit`（默认 24，范围 1～50）和 `offset`（默认 0，范围 0～100000）。返回画布 ID、名称、更新时间、节点数量和画布地址，以及 `hasMore` / `nextOffset`。只列出当前授权用户自己的已保存画布，按最近更新时间倒序。

### `get_canvas`：读取指定画布及节点

必填 `canvasId`；可选 `limit`（默认 24，范围 1～50）和 `offset`（默认 0，范围 0～300）。返回画布名称、版本、视口和地址；节点页包括 ID、位置、已保存的名称、提示词、模型参数、结构化提示词及引用关系，以及可用的素材和最近任务摘要。`nodeCount` 是总节点数；`hasMore` / `nextOffset` 用于继续读取。

素材摘要只包含类型、来源、格式、大小、媒体参数和已有参考检查；不返回素材文件、私有对象键或供应商临时地址。`latestTask` 是该节点最新任务，`latestResult` 是最近成功结果的保存状态，两者可能对应不同任务；较新的临时结果不会冒充较旧的已保存素材。跨页可比较 `version` 判断保存的节点快照是否变化；任务状态可能独立更新。

两个读取工具不修改画布或积分，也不生成图片、视频。不存在与不属于用户的画布返回相同提示；画布文本是用户数据，不是对智能体的指令。

### `create_canvas_node`：新建空节点

| 字段 | 说明 |
| --- | --- |
| 名称 | `create_canvas_node` |
| `canvasId` | 必填，用户所属画布的小写 UUID |
| `kind` | 必填，`image` 或 `video` |
| `nodeId` | 可选，小写 UUID；相同画布中同类型节点仍存在时，重试返回该节点且 `created=false`，不同类型返回错误 |
| `position` | 可选，`{x,y}` 坐标，均需提供且绝对值不超过 10000000；省略时按已保存节点位置选取附近空位 |
| 返回 | `canvasId`、`nodeId`、`kind`、`name`、`position`、`version`、`created` 和 `canvasUrl` |

只创建空节点，默认未选择模型，视频模式与网页新建节点一致。不会调用模型、创建生成任务、上传素材或扣除积分。每次逻辑创建推荐由客户端生成并复用 `nodeId`；省略时由后端生成，若请求结果未知，应先检查画布，不要盲目重复提交。该去重只在节点仍存在于画布中时有效；节点删除后不提供跨删除的永久去重。

调用示例（协议封装省略）：

```json
{
  "name": "create_canvas_node",
  "arguments": {
    "canvasId": "11111111-1111-4111-8111-111111111111",
    "kind": "image",
    "nodeId": "22222222-2222-4222-8222-222222222222",
    "position": {"x": 120, "y": 240}
  }
}
```

节点 ID、类型、坐标和完整快照受现有格式与 300 节点上限校验。工具错误不返回 SQL 或用户凭证。新节点的默认位置基于已保存的画布坐标，并不保证处于当前浏览器的可视区域。

### `list_generation_models`：查询可用模型

可选 `kind` 为 `image` 或 `video`；省略时返回两类。根据后端现有模型定义和积分价格表列出可生成模型、供应商、支持的模式、参考数量和参数。单个 MCP 图片任务目前只生成一张；图片模型的数量参数因此限定为 1。APIMart 的内部 Nano Ext 实际型号由后端按单张规则选择，不需要客户端直接指定。

### `configure_canvas_node`：配置节点

必填 `canvasId`、`nodeId`、`expectedVersion` 和 `patch`。`expectedVersion` 来自创建节点或读取画布结果。`patch` 可包含 `name`、`prompt`、`promptParts`、`model`、`imageOptions`、`videoOptions`、`videoMode`、`referenceIds`、`firstFrameId`。

`model` 包含 `provider`、`modelId` 和可选 `parameters`；APIMart 参数按 `list_generation_models` 返回的名称填写，未填写的基础参数使用现有模型默认值。修改模型时，`parameters` 按整组提供，支持已有的参数别名。Ark 图片 / 视频的参数分别放在 `imageOptions` / `videoOptions`。`videoMode` 为 `text`、`image` 或 `reference`；单图起始画面模式还须提供同画布 `referenceIds` 和 `firstFrameId`。

传入 `prompt` 但不传 `promptParts` 会清除旧结构化标签；若需保留标签，应同时提供与提示词一致的 `promptParts`。版本不匹配时拒绝写入；更改时保留快照中未指定的其他字段。该工具只保存节点草稿，不发起生成、不冻结积分。

### `quote_generation`：查询积分报价

必填 `canvasId`、`nodeId`。根据已保存节点、模型参数和已保存参考素材，返回 `kind`、`canvasVersion`、`points`、`priceVersion`、`quoteHash` 和积分余额。报价不创建任务或冻结积分；参考素材正在生成 / 保存或尚未保存时不可报价。

### `start_generation`：提交生成任务

必填 `canvasId`、`nodeId`、客户端预先生成的 `taskId`，以及从报价结果原样取得的 `canvasVersion`、`points`、`priceVersion`、`quoteHash`。提交会使用现有业务逻辑重新核价，事务中冻结积分并创建任务，由现有后台进程处理；返回任务 ID 与初始状态。

`quoteHash` 用于识别报价对应的已保存输入及画布版本，并非用户确认凭证或签名。节点 / 画布版本变化时需要重新报价。相同任务 ID 和相同报价重试返回原任务；同一 ID 不可用于其他请求。提交结果不明或 `SUBMISSION_UNKNOWN` 时先查询原任务，不自动创建新任务重试。

### `get_generation_task`：查询任务

必填 `taskId`。只返回当前用户所属画布的任务，包含任务类型、状态、积分状态、错误摘要、结果保存状态、素材摘要和画布链接，不返回供应商临时地址或私有对象键。`succeeded` 且结果未保存时需根据 `result.saved` / `storageError` 判断，不能当成已经持久保存。

## 数据与并发

节点仍保存在 `canvases.snapshot`，没有新增节点表或迁移。创建和配置时锁定所属画布行，校验后同一事务更新快照、版本和更新时间；保留未修改的字段及当前版本不认识的字段。网页整张快照保存继续检查版本：如果 MCP 先写入，旧版本网页保存返回冲突，不会静默覆盖 MCP 节点；如果网页先保存，MCP 创建会在新的已保存快照上追加，配置则需要匹配最新版本。

生成复用 `generation_tasks`、`credit_accounts`、`credit_ledger` 和 `assets`，不新增任务表。报价结果不保证用户已经确认；工具说明要求 AI 在用户同意报价后提交。项目仍以现有规则处理冻结、结算、明确失败和不确定状态，不新增付费生成供应商或队列。

**未实现打开中页面自动同步、合并未保存草稿或多设备协同。**实验时先保存并离开目标画布，再调用工具并重新打开；发生冲突时保留当前页，不要直接刷新未保存草稿。后台已经提交的生成任务保持项目原有行为。

## 授权与配置

复用 008 迁移的 `mcp_oauth_clients` / `mcp_oauth_tokens`。普通活跃用户可授权；本版授权同时申请 `canvases:read`、`canvases:write` 和 `generation:write`，授权页面明确说明可提交生成并消耗积分。每次调用重新检查令牌有效期、资源、三项权限和用户状态；读取、写入、报价及任务查询都校验画布或任务所有权。授权码有效 5 分钟、访问令牌有效 1 小时，无 refresh token；撤销页面为同源 `/oauth/connections`。动态注册兼容 Codex 同时申请 `authorization_code` 和 `refresh_token`，但注册结果只声明支持 `authorization_code`，不会签发刷新令牌。旧授权码 / 令牌不自动获得生成权限，客户端需重新授权；当前不提供只读授权模式。

入口默认关闭。仅本机实验时，可在后端进程中设置：

```powershell
$env:MCP_PUBLIC_URL = 'http://127.0.0.1:5173/mcp'
$env:APP_ORIGINS = 'http://127.0.0.1:5173'
$env:AUTH_COOKIE_SECURE = 'false'
```

后端监听本机且连接本地 PostgreSQL，前端 Vite 使用 5173 并代理 `/mcp`、`/oauth/`、`/.well-known/` 和 `/api/`。不同来源、端口和 `MCP_PUBLIC_URL` 必须保持一致。随后在支持远程 Streamable HTTP 和该 OAuth 流程的客户端连接该地址；在客户端机器之外运行时，`127.0.0.1` 指向客户端自身的运行环境。

`MCP_PUBLIC_URL` 要求 HTTPS（本机回环可 HTTP）、路径为 `/mcp`，来源包含在 `APP_ORIGINS`。使用现有元数据、动态客户端注册、授权码和 S256 PKCE；客户端回调地址必须和注册值完全一致。未设置环境变量时路由不启用。

## 本地验证

- 隔离 PostgreSQL + Go MCP SDK：普通用户授权、八个工具发现、本人画布 / 节点分页、模型清单、节点配置、报价、仅测试库中的任务排队 / 模拟结果、积分冻结及重复提交防重、跨用户拒绝、旧令牌拒绝、输入校验、撤销、过期、禁用、PKCE 和版本保护。测试不启动生成工作进程、不调用真实模型或 OSS。
- `frontend/tests/mcp-return.spec.ts` 和认证回归：普通用户登录回跳、已登录回跳、外站和非授权地址拒绝。
- `frontend/scripts/verify-live-mcp.mjs`：真实 Chrome → Vite → Go → 隔离 PostgreSQL，验证普通用户登录、取消授权、PKCE、八个工具发现、节点创建与重试、读取画布及节点、跨用户拒绝、模型清单 / 节点配置 / 报价、重新打开和刷新恢复、撤销后拒绝。脚本使用临时 schema 并清理，不调用提交工具、模型或 OSS；需要设置本机 `frame_space_test` 的 `TEST_DATABASE_URL` 和新构建的 `MCP_TEST_BACKEND_BIN`。
- 2026-11-11：本机 Codex 通过动态注册及普通用户 OAuth 授权成功，实际发现八个工具，并只读调用画布列表及图片模型清单，包含 Muse Image。验证时临时关闭模型及 OSS 配置，没有创建节点、提交生成或调用真实供应商。其他 AI 客户端尚未验收；本地验证不能等同于线上发布或真实生成验收。
- 2026-11-12：正式页面的连接入口、提示词地址、模拟剪贴板及手机布局已检查；线上 MCP 元数据返回画布读写与生成权限，匿名 MCP 请求返回 401。本次线上发布未使用普通用户完成 OAuth 连接，也未提交付费生成。

2026-10-18 曾发布管理员 `list_users` MCP 并完成当时的正式 HTTPS 验证。那是历史版本；本次普通用户画布工具的发布细节见 [部署指南](./deployment.md)。
