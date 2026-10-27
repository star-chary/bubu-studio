# 固定模型与基础参数

当前状态：2026-10-26。图片、视频模型和基础参数随前端发布，运行时不再读取 APIMart 模型目录。

## 实际数据流

```text
staticModelCatalog.ts + video_models.json（固定型号、功能与基础参数）
  → ImageModelSelect（直接显示模型）
  → CatalogParameterForm / CatalogParameterField（直接显示基础参数）
  → 节点 modelSelection
  → 原有画布保存接口
  → canvases.snapshot JSONB
```

原有方舟节点继续使用 `imageModels.ts` / `videoModels.ts`；新建节点仍由用户明确选择供应商与型号。
方舟保留 2 个图片、3 个视频可选项；历史 Seedance 2.0 Mini 定义仍用于旧画布回显，不加入新选择菜单。
APIMart 固定为 16 个图片选项、22 个通用视频选项；3 个专用工作流定义只保留历史回显，菜单统一放在 APIMart 一组，条目仅显示型号名称。
配置中的常用、候选、专用标记仅用于内部维护，不拆分界面分组。
已移出清单的历史型号、未知参数、复杂参数对象仍可保存，不自动切换到其他型号。

## 参数开放范围

面板只允许比例、清晰度、视频时长、生成音频、生成数量五类；不支持的类别不展示，固定能力只读展示。
不再有“更多设置”、随机种子、输出格式、源任务 ID 或素材 URL 等额外编辑项。
基础项及初值按供应商文档维护，来源与具体值见 [参数来源](./apimart-parameter-sources.md)。

- 比例使用文档确认的选项卡片；清晰度使用等宽按钮。
- 连续时长使用滑块和数值框；离散时长保持离散，特殊值与连续范围分开。
- `-1` 自动时长、`0` 原视频时长不扩展普通时长范围。
- 可灵 V3 的清晰度保存到 `mode`；MJ 高清保存到布尔 `hd`；音频沿用各模型的 `audio` 或 `generate_audio`。
- Gemini Omni 1.1 没有时长参数，不增加时长滑块。
- 视频通常单任务输出，不编造 `n` 参数；Veo 上游支持 `sample_count` 1–4 个，本轮按单结果节点链路固定为 1 个；图片固定单张的型号只读显示 1 张。
- 前端基础项隐藏不改变历史草稿。修改基础项仅更新该字段、处理别名及受其联动影响的基础值，高级字段完整保留。

## Midjourney 型号身份

四个界面选项：Midjourney v8.2、v8.1、v7、Niji 7。
菜单 `optionId` 分别为 `midjourney-8.2`、`midjourney-8.1`、`midjourney-7`、`midjourney-niji7`。
这些仅用于区分界面选项，保存的上游 `modelId` 始终为 `midjourney`：

| 界面选项 | version | niji |
| --- | --- | --- |
| Midjourney v8.2 | 8.2 | false |
| Midjourney v8.1 | 8.1 | false |
| Midjourney v7 | 7 | false |
| Niji 7 | 7 | true |

切换主版本会明确清除 Niji 状态；刷新根据型号与固定版本字段恢复选中项。
只在 v8.1/v8.2 开放 `hd`；质量、速度、动作等其他设置留待后续功能开发。

## 已移除的运行时目录功能

- 后端 `internal/modelcatalog/`、目录 HTTP 客户端、Schema 归一化、定时同步及缓存。
- `GET /api/model-catalog` 路由；已登录访问该旧路径返回 404。
- 前端目录请求、账号监听、IndexedDB 模型目录缓存和缓存过期刷新。
- 运行时以接口 Schema 补充本地参数的合并层。
- `.env.example` 中仅用于动态目录的配置。已有本地私密配置不修改，旧目录变量不再读取。

旧浏览器 IndexedDB 数据不再参与展示，不需要用户清理缓存。
前端型号和参数随发布版本更新，新增型号需先核对文档并补配置。

## 生成边界

2026-10-26 已扩展22个通用视频型号的文生、普通图片参考或单图起始画面生成，以各型号文档为准。功能菜单统一三项，不支持的项置灰；未纳入工作流的型号不进入菜单。
2026-10-27 本地已连接 APIMart 图片型号，暂不请求平台积分；任务、参考图、结果转存和测试边界见 [APIMart 图片生成](./apimart-image-generation.md)。视频专用工作流仍隔离。
模型目录与参数不发起供应商请求。具体能力、请求流程与真实调用记录见 [视频型号与功能接入](./apimart-video-models.md)。
原有方舟生成、上传、任务处理、登录、权限及画布保存接口保持原有职责。
没有新增数据库表或迁移，`catalogVersion` / `schemaVersion` 继续记录前端静态配置版本。

## 核心文件与验收

- `frontend/src/models/staticModelCatalog.ts`：固定型号与文档参数；菜单版本身份。
- `frontend/src/models/modelCatalog.ts`：参数校验、初值、别名与草稿读取。
- `frontend/src/components/ImageModelSelect.vue`：固定模型分组、搜索和键盘选择。
- `frontend/src/components/CatalogParameterForm.vue` / `CatalogParameterField.vue`：五类参数控件。
- `frontend/src/canvas/useMediaNodes.ts`：保存模型与基础参数修改，保留历史高级字段。
- `frontend/tests/static-models.spec.ts`：完整固定清单、零目录请求、全部型号展开、MJ 四版本保存恢复、旧草稿和联动。
- `backend/internal/server/model_selection_test.go`：生成隔离、供应商身份与旧目录路由移除。

```powershell
cd frontend
npm run build
npm run test:e2e -- tests/static-models.spec.ts tests/image-models.spec.ts tests/video-controls.spec.ts
cd ../backend
go test ./...
go vet ./...
```

已验证：前端相关 42 项回归通过；包括遍历 41 个 APIMart 选项、零目录请求、Midjourney 四版本保存恢复、生成隔离、方舟参数、积分、保存和素材工具。后端全包测试（独立测试库）与 vet 通过。

数据库集成测试只使用以 `_test` 结尾的独立测试数据库，不使用业务库。
