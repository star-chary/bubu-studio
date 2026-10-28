# 中转站视频模型与功能接入

2026-10-26，本地实现。型号文档、请求转换、前端操作与真实供应商验收分别记录；尚未发布线上。

2026-10-28 更新：新任务接入固定积分报价，因计价不确定隐藏 Seedance 2.5 和 Gemini Omni 1.1 Flash，现可选择 20 个视频模型。历史能力映射和验收记录仍保留；当前计费与模型范围以 [APIMart 固定积分](./apimart-fixed-credits.md) 为准。

## 功能与交互

视频节点统一使用「模型 → 功能 → 参数」。功能列表固定为文生视频、全能参考、图生视频；模型不支持的项直接置灰，没有暂未开放或待确认等原因分类。没有接入当前工作流的型号不进入新选择列表；旧画布仍保留其选择和草稿。

- 空输入时也可先选择模型支持的功能；缺提示词或素材只限制生成按钮。
- 切换模型保留兼容功能；不兼容时优先文生视频，否则使用该型号的第一个受支持功能。历史无模式的方舟节点仍按全能参考恢复。
- 文生不提交素材，但保留原参考草稿与连线；提示词中的素材标签须先移除。
- 全能参考本轮接入普通图片参考，按型号校验数量；视频和音频参考尚未扩展。
- 图生只提交用户明确指定的一张起始图片。切换功能不自动选取第一张旧参考图；用户可以点击参考缩略图的「设为起始图」，或在图生模式下主动选择图片。其他参考素材仍保留，但不参与本次请求。
- 起始图片保存在节点 `firstFrameId` 中，随现有画布快照保存；删除参考关系时一并清理。任务素材绑定使用 `role: "first_frame"`，不会把内容参考自动解释为首帧。

## 静态契约与请求流程

`backend/internal/apimart/video_models.json` 是前后端共同使用的发布时配置，包含型号身份、功能、原生基础参数、图片用途与请求字段。Go 嵌入该文件，Vue 构建时导入；进入画布不请求模型目录或能力接口。

```text
节点模型、功能、参数与明确的起始图片
  → 前端输入校验，保存画布
  → POST /api/credits/quote → 展示固定积分 → POST /api/videos/generations
  → Session / CSRF / 画布归属 / 已保存型号校验
  → 按共同静态契约验证功能、原生 parameters、素材角色
  → 复核报价并在事务中冻结积分，固定任务 provider、model、mode、parameters 和素材绑定
  → 后台签发自有素材地址，按型号转换为供应商字段
  → 一次提交 APIMart → 查询原任务 → 转存 OSS
  → 素材与结果落库 → 原节点播放、刷新恢复
```

请求的可选 `parameters` 只接受开放的基础字段。历史没有此字段的 Seedance 任务继续读取原有比例、清晰度、时长和音频字段。未知高级字段不转发；已有草稿高级值仍保留。

型号之间的区别包含：`size` / `aspect_ratio`、清晰度 `resolution` / `mode`、`audio` / `generate_audio`、普通 `image_urls`、带角色的图片、`first_frame_image`、PixVerse 的 `img_references` 和 SkyReels 的 `ref_images`。提示词素材标签也按型号编译，保留普通文字。

沿用现有任务表、素材表、积分账户／流水和画布 JSONB，没有新表或迁移。APIMart 新任务参与平台积分报价、冻结和结算；原有方舟链路保留。

## 型号范围

以下为本轮三种产品功能的文档映射。`✓` 表示已实现对应请求；具体真实调用结果、等待过久或失败的型号另见验收记录。

| 型号 | 文生视频 | 全能参考（图片） | 图生视频 |
| --- | --- | --- | --- |
| [Seedance 2.5](https://docs.apimart.ai/en/api-reference/videos/seedance-2-5/generation) | ✓ | ✓ | ✓ |
| [Seedance 2.0](https://docs.apimart.ai/en/api-reference/videos/seedance-2-0/generation) | ✓ | ✓ | ✓ |
| [Seedance 2.0 Fast](https://docs.apimart.ai/en/api-reference/videos/seedance-2-0/generation) | ✓ | ✓ | ✓ |
| [Kling 4.0](https://docs.apimart.ai/en/api-reference/videos/kling-v4/generation) | ✓ | ✓ | ✓ |
| [Kling 3.0 Turbo](https://docs.apimart.ai/en/api-reference/videos/kling-3.0-turbo/generation) | ✓ | — | ✓ |
| [Kling 3.0](https://docs.apimart.ai/en/api-reference/videos/kling-v3/generation) | ✓ | — | ✓ |
| [Kling 3.0 Omni](https://docs.apimart.ai/en/api-reference/videos/kling-v3-omni/generation) | ✓ | ✓ | ✓ |
| [Wan 3.0](https://docs.apimart.ai/en/api-reference/videos/wan3.0-video/generation) | ✓ | ✓ | ✓ |
| [Wan 3.0 Prime](https://docs.apimart.ai/en/api-reference/videos/wan3.0-video/generation) | ✓ | ✓ | ✓ |
| [MiniMax H3](https://docs.apimart.ai/en/api-reference/videos/minimax-h3/generation) | ✓ | ✓ | ✓ |
| [MiniMax H3 Max](https://docs.apimart.ai/en/api-reference/videos/minimax-h3/max) | ✓ | ✓ | ✓ |
| [HappyHorse 1.1](https://docs.apimart.ai/en/api-reference/videos/happyhorse-1.1/generation) | ✓ | ✓ | ✓ |
| [HappyHorse 1.0](https://docs.apimart.ai/en/api-reference/videos/happyhorse-1.0/generation) | ✓ | ✓ | ✓ |
| [Gemini Omni 1.1 Flash](https://docs.apimart.ai/en/api-reference/videos/gemini-omni-1.1-flash/generation) | ✓ | ✓ | ✓ |
| [Vidu Q4 Preview](https://docs.apimart.ai/en/api-reference/videos/vidu-q4-preview/generation) | — | ✓ | ✓ |
| [Vidu Q3 Pro](https://docs.apimart.ai/en/api-reference/videos/vidu-q3-pro/generation) | ✓ | — | ✓ |
| [Grok Imagine Video 1.5](https://docs.apimart.ai/en/api-reference/videos/grok-imagine/official) | ✓ | ✓ | — |
| [FLUX 3 Video](https://docs.apimart.ai/en/api-reference/videos/flux-3-video/generation) | ✓ | — | ✓ |
| [PixVerse V6](https://docs.apimart.ai/en/api-reference/videos/pixverse-v6/generation) | ✓ | ✓ | ✓ |
| [Veo 3.1 Quality](https://docs.apimart.ai/en/api-reference/videos/veo3/generation-official) | ✓ | — | ✓ |
| [Veo 3.1 Fast](https://docs.apimart.ai/en/api-reference/videos/veo3/generation-official) | ✓ | — | ✓ |
| [SkyReels V4 Std](https://docs.apimart.ai/en/api-reference/videos/skyreels-v4/generation) | ✓ | ✓ | ✓ |

三项专用工作流不加入本轮菜单：Kling 3.0 Motion Control 需要动作视频与人物图片；MiniMax H3 Regeneration 需要原任务 ID；Wan 2.7 VideoEdit 需要源视频编辑流程。定义保留供旧画布回显，后端拒绝将它们作为普通生成型号调用。

基础参数没有增加新的编辑种类。为符合当前单结果节点链路，Veo 每个任务固定生成一个视频，避免请求多个结果却只展示第一个。Gemini Omni 没有文档时长字段，提交不补造该字段；Seedance 2.5 图生按文档使用 `adaptive` 比例。

## 验证与长等待记录

- 请求格式测试逐一检查22个型号的三种功能组合，覆盖普通参考与起始图片的区别、禁止功能、原生字段校验和提示词标签。
- Go 数据库测试覆盖原生参数和首帧角色持久化、任务不随画布切换改变、归属、幂等和平台积分隔离。
- 浏览器测试覆盖统一置灰、空输入先选功能、兼容功能保留、不兼容回退、明确选择起始图、保存恢复，以及原方舟、积分、引用和任务恢复流程。
- 真实验收脚本 `frontend/scripts/verify-live-apimart-models.mjs` 每型号最多创建一个任务，使用合成图片与低清晰度短视频。任务 ID 在提交前保存；已有报告必须显式续查，不重新生成。
- 按现有三个视频并发位验收。每个型号观察3分钟后仍未完成，记录 `delayed`、任务ID和所处状态，再继续下一个；最后只续查已提交的原任务。验收等待超时不取消或重新提交上游任务。

2026-10-26最终复查：22个型号各提交一个代表性任务，全部成功；22份视频素材转存成功，22项前端播放和刷新恢复通过。后端切换到最终构建后再次仅续查和播放原任务，没有追加生成。

每型号真实验收一种代表性功能；其余已声明功能通过请求格式和交互测试，未穷举所有功能与参数的真实生成组合。

| 型号 | 真实验收功能 | 实际视频尺寸 | 实际秒数 | 提交至保存完成 | 播放/恢复 |
| --- | --- | --- | --- | --- | --- |
| Seedance 2.5 | 图生视频 | 854×480 | 4.042 | 155秒 | 通过 |
| Seedance 2.0 | 文生视频 | 864×496 | 4.042 | 194秒 | 通过 |
| Seedance 2.0 Fast | 图片参考 | 864×496 | 4.042 | 102秒 | 通过 |
| Kling 4.0 | 图片参考 | 1280×720 | 3.042 | 94秒 | 通过 |
| Kling 3.0 Turbo | 图生视频 | 1280×720 | 3.042 | 174秒 | 通过 |
| Kling 3.0 | 文生视频 | 1280×720 | 3.042 | 53秒 | 通过 |
| Kling 3.0 Omni | 图片参考 | 1280×720 | 3.042 | 87秒 | 通过 |
| Wan 3.0 | 文生视频 | 854×480 | 2.000 | 92秒 | 通过 |
| Wan 3.0 Prime | 图生视频 | 854×480 | 2.000 | 43秒 | 通过 |
| MiniMax H3 | 图片参考 | 1344×768 | 4.458 | 164秒 | 通过 |
| MiniMax H3 Max | 图生视频 | 832×480 | 5.184 | 22秒 | 通过 |
| HappyHorse 1.1 | 文生视频 | 1280×720 | 3.163 | 93秒 | 通过 |
| HappyHorse 1.0 | 图生视频 | 1280×720 | 3.163 | 92秒 | 通过 |
| Gemini Omni 1.1 Flash | 文生视频 | 640×360 | 10.027 | 32秒 | 通过 |
| Vidu Q4 Preview | 图生视频 | 960×540 | 3.042 | 103秒 | 通过 |
| Vidu Q3 Pro | 文生视频 | 960×528 | 1.042 | 32秒 | 通过 |
| Grok Imagine Video 1.5 | 图片参考 | 736×400 | 1.042 | 64秒 | 通过 |
| FLUX 3 Video | 图生视频 | 1280×704 | 5.042 | 186秒 | 通过 |
| PixVerse V6 | 图片参考 | 640×360 | 1.042 | 45秒 | 通过 |
| Veo 3.1 Quality | 文生视频 | 1280×720 | 4.000 | 45秒 | 通过 |
| Veo 3.1 Fast | 图生视频 | 1280×720 | 4.000 | 42秒 | 通过 |
| SkyReels V4 Std | 图片参考 | 864×480 | 3.021 | 73秒 | 通过 |

长等待记录（3分钟为本轮观察阈值，包含排队、素材准备、生成和保存；不能直接认定为网络故障）：

| 型号 | 总耗时 | 记录与最终结果 | 平台任务 ID |
| --- | --- | --- | --- |
| Seedance 2.0 | 194秒 | 并发推进其他型号期间完成，最终成功；数据库审计记录总耗时超过3分钟 | `897368ec-b0a6-45a1-95d6-9cb0254d2f61` |
| FLUX 3 Video | 186秒 | 观察超过3分钟后继续其他型号，末尾续查同一任务，最终成功 | `df657674-99cf-413f-86b5-b062e7aa169e` |

数据库核对：该验收画布共22个生成任务、22份生成素材；无重复生成任务。平台积分消费流水0条、冻结0分，合成账号保留200分注册赠分。

验证：51项浏览器回归全部通过；416项Go测试（含子用例）通过，数据库用例没有跳过；`go vet ./...`、后端构建、前端类型检查及生产构建通过。

首个 Seedance 2.5 的历史双任务验收见 [2026-10-25 接入记录](./apimart-video-generation.md)。
