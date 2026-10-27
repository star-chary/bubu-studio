# 静态模型基础参数来源

2026-10-26：视频基础参数与功能约束统一维护在 `backend/internal/apimart/video_models.json`，前后端共同使用。Veo 当前每任务固定一个结果；首帧用途与未进入本轮菜单的专用工作流见 [视频型号与功能接入](./apimart-video-models.md)。以下保留上游参数来源记录。

核对日期：2026-10-25。仅记录固定名单当前开放的基础参数。
运行时直接使用 `frontend/src/models/staticModelCatalog.ts`，不请求供应商目录或参数 Schema。
图片16个可选项、通用视频22个可选项，另保留3个专用工作流定义用于历史回显；方舟现有静态定义另外保留。

- 比例是文档允许值中的已开放集合，当前不开放任意像素输入。
- 清晰度沿用原字段：可灵 V3 用 `mode`；MJ v8.1/v8.2 用布尔 `hd`，不使用 `quality` 冒充分辨率。
- 固定音频表示模型能力，只读展示；没有文档开关的型号不添加可切换开关。
- 文档没有 `n` 的模型不编造生成数量参数；不把批量任务数当成单次接口输出数。
- MJ 版本来源：[APIMart Imagine](https://docs.apimart.ai/en/api-reference/images/midjourney/imagine)、[Midjourney Version](https://docs.midjourney.com/hc/en-us/articles/32199405667853-Version)。`version` / `niji` 固定在型号选择中，不作为额外面板项。
- 2026-10-26已接通22个通用视频型号，按文档开放文生、普通图片参考或单图起始画面。2026-10-27本地接通图片型号，真实付费出图尚未验收；其他视频专用工作流仍隔离。详见 [图片生成](./apimart-image-generation.md) 与 [视频型号与功能接入](./apimart-video-models.md)。

## 图片

### Seedream 5.0 Lite

- 分组：常用；选项：`seedream-5-0-lite`；上游型号：`seedream-5-0-lite`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/seedream-5-lite/generation)。
- 比例：`size`；auto / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 2:1 / 1:2 / 21:9；初值 `1:1`。
- 清晰度：`resolution`；2K / 3K / 4K；初值 `2K`。
- 生成数量：`n`；1-15；初值 `1`。

### Seedream 5.0 Pro

- 分组：常用；选项：`seedream-5-0-pro`；上游型号：`seedream-5-0-pro`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/seedream-5-0-pro/generation)。
- 比例：`size`；auto / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 2:1 / 1:2 / 21:9；初值 `auto`。
- 清晰度：`resolution`；1K / 1.5K / 2K；初值 `1K`。
- 生成数量：`n`；固定 1。

### Nano Banana 2.1

- 分组：常用；选项：`gemini-nano-banana-2.1`；上游型号：`gemini-nano-banana-2.1`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/gemini-nano-banana-2.1/generation)。
- 比例：`size`；auto / 1:1 / 4:3 / 3:4 / 16:9 / 9:16 / 3:2 / 2:3 / 4:5 / 5:4 / 21:9 / 1:4 / 4:1 / 1:8 / 8:1；初值 `auto`。
- 清晰度：`resolution`；1K / 2K / 4K；初值 `1K`。
- 生成数量：`n`；1-4；初值 `1`。

### GPT Image 2.5 Flare

- 分组：常用；选项：`gpt-image-2.5-flare`；上游型号：`gpt-image-2.5-flare`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/gpt-image-2.5/generation)。
- 比例：`size`；auto / 1:1 / 4:3 / 3:4 / 16:9 / 9:16 / 3:2 / 2:3 / 4:5 / 5:4 / 21:9 / 2:1 / 1:2 / 3:1 / 1:3 / 9:21；初值 `auto`。
- 清晰度：`resolution`；1k / 2k / 4k；初值 `1k`。
- 生成数量：`n`；1-4；初值 `1`。

### GPT Image 2.5 Sunburst

- 分组：常用；选项：`gpt-image-2.5-sunburst`；上游型号：`gpt-image-2.5-sunburst`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/gpt-image-2.5/generation)。
- 比例：`size`；auto / 1:1 / 4:3 / 3:4 / 16:9 / 9:16 / 3:2 / 2:3 / 4:5 / 5:4 / 21:9 / 2:1 / 1:2 / 3:1 / 1:3 / 9:21；初值 `auto`。
- 清晰度：`resolution`；1k / 2k / 4k；初值 `1k`。
- 生成数量：`n`；1-4；初值 `1`。

### FLUX 3 Image

- 分组：常用；选项：`flux-3-image`；上游型号：`flux-3-image`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/flux-3-image/generation)。
- 比例：`aspect_ratio`；auto / 21:9 / 2:1 / 16:9 / 3:2 / 7:5 / 4:3 / 5:4 / 1:1 / 4:5 / 3:4 / 5:7 / 2:3 / 9:16 / 1:2 / 9:21；初值 `auto`。
- 清晰度：`resolution`；768sq / 1k / 1.5k / 2k / 4k；初值 `1k`。
- 生成数量：`n`；固定 1。

### FLUX 3 Image Klein

- 分组：候选；选项：`flux-3-image-klein`；上游型号：`flux-3-image-klein`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/flux-3-image-klein/generation)。
- 比例：`aspect_ratio`；auto / 21:9 / 2:1 / 16:9 / 3:2 / 7:5 / 4:3 / 5:4 / 1:1 / 4:5 / 3:4 / 5:7 / 2:3 / 9:16 / 1:2 / 9:21；初值 `auto`。
- 清晰度：`resolution`；768sq / 1k / 1.5k / 2k / 4k；初值 `1k`。
- 生成数量：`n`；固定 1。

### Qwen Image 3.0 Pro

- 分组：常用；选项：`qwen-image-3.0-pro`；上游型号：`qwen-image-3.0-pro`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/qwen-image-3.0/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3；初值 `1:1`。
- 清晰度：`resolution`；1K / 2K；初值 `1K`。
- 生成数量：`n`；1-6；初值 `1`。

### Z Image Turbo

- 分组：常用；选项：`z-image-turbo`；上游型号：`z-image-turbo`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/z-image-turbo/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3；初值 `1:1`。
- 清晰度：`resolution`；1K / 2K；初值 `1K`。

### Nano Banana Pro

- 分组：候选；选项：`gemini-3-pro-image-preview`；上游型号：`gemini-3-pro-image-preview`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/gemini-3-pro/generation)。
- 比例：`size`；auto / 1:1 / 4:3 / 3:4 / 16:9 / 9:16 / 3:2 / 2:3 / 4:5 / 5:4 / 21:9；初值 `1:1`。
- 清晰度：`resolution`；1K / 2K / 4K；初值 `1K`。
- 生成数量：`n`；固定 1。

### FLUX 2 Flex

- 分组：候选；选项：`flux-2-flex`；上游型号：`flux-2-flex`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/flux-2/generation)。
- 比例：`size`；auto / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9 / 9:21；初值 `1:1`。
- 清晰度：`resolution`；1MP / 2MP / 3MP / 4MP；初值 `2MP`。
- 生成数量：`n`；固定 1。

### FLUX 2 Max

- 分组：候选；选项：`flux-2-max`；上游型号：`flux-2-max`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/flux-2/generation)。
- 比例：`size`；auto / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9 / 9:21；初值 `1:1`。
- 清晰度：`resolution`；1MP / 2MP / 3MP / 4MP；初值 `2MP`。
- 生成数量：`n`；固定 1。

### Midjourney v8.2

- 分组：候选；选项：`midjourney-8.2`；上游型号：`midjourney`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/midjourney/imagine)。
- 型号固定项：`{"version":"8.2","niji":false}`。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9；初值 `1:1`。
- 清晰度：`hd`；标准 (false) / 高清 (true)；初值 `false`。

### Midjourney v8.1

- 分组：候选；选项：`midjourney-8.1`；上游型号：`midjourney`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/midjourney/imagine)。
- 型号固定项：`{"version":"8.1","niji":false}`。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9；初值 `1:1`。
- 清晰度：`hd`；标准 (false) / 高清 (true)；初值 `false`。

### Midjourney v7

- 分组：候选；选项：`midjourney-7`；上游型号：`midjourney`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/midjourney/imagine)。
- 型号固定项：`{"version":"7","niji":false}`。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9；初值 `1:1`。

### Niji 7

- 分组：候选；选项：`midjourney-niji7`；上游型号：`midjourney`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/images/midjourney/imagine)。
- 型号固定项：`{"version":"7","niji":true}`。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9；初值 `1:1`。

## 视频

### Seedance 2.5

- 分组：常用；选项：`seedance-2.5`；上游型号：`seedance-2.5`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/seedance-2-5/generation)。
- 比例：`size`；adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9；初值 `adaptive`。
- 清晰度：`resolution`；480p / 720p / 1080p；初值 `720p`。
- 视频时长：`duration`；4-30 秒; -1: 自动时长；初值 `5`。
- 生成音频：`generate_audio`；true / false；初值 `true`。

### Seedance 2.0

- 分组：候选；选项：`seedance-2.0`；上游型号：`seedance-2.0`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/seedance-2-0/generation)。
- 比例：`size`；adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9；初值 `16:9`。
- 清晰度：`resolution`；480p / 720p / 1080p / 4k；初值 `720p`。
- 视频时长：`duration`；4-15 秒；初值 `5`。
- 生成音频：`generate_audio`；true / false；初值 `true`。

### Seedance 2.0 Fast

- 分组：常用；选项：`seedance-2.0-fast`；上游型号：`seedance-2.0-fast`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/seedance-2-0/generation)。
- 比例：`size`；adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9；初值 `16:9`。
- 清晰度：`resolution`；480p / 720p；初值 `720p`。
- 视频时长：`duration`；4-15 秒；初值 `5`。
- 生成音频：`generate_audio`；true / false；初值 `true`。

### Kling 4.0

- 分组：常用；选项：`kling-v4`；上游型号：`kling-v4`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/kling-v4/generation)。
- 比例：`aspect_ratio`；16:9 / 9:16 / 1:1 / 21:9 / auto；初值 `16:9`。
- 清晰度：`resolution`；720p / 1080p；初值 `720p`。
- 视频时长：`duration`；3-30 秒；初值 `5`。
- 生成音频：`audio`；固定 true。

### Kling 3.0 Turbo

- 分组：常用；选项：`kling-3.0-turbo`；上游型号：`kling-3.0-turbo`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/kling-3.0-turbo/generation)。
- 比例：`aspect_ratio`；16:9 / 9:16 / 1:1；初值 `16:9`。
- 清晰度：`resolution`；720p / 1080p；初值 `720p`。
- 视频时长：`duration`；3-15 秒；初值 `5`。

### Kling 3.0

- 分组：候选；选项：`kling-v3`；上游型号：`kling-v3`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/kling-v3/generation)。
- 比例：`aspect_ratio`；16:9 / 9:16 / 1:1；初值 `16:9`。
- 清晰度：`mode`；720P (std) / 1080P (pro) / 4K (4k)；初值 `std`。
- 视频时长：`duration`；3-15 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `false`。

### Kling 3.0 Omni

- 分组：专用；选项：`kling-v3-omni`；上游型号：`kling-v3-omni`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/kling-v3-omni/generation)。
- 比例：`aspect_ratio`；16:9 / 9:16 / 1:1；初值 `16:9`。
- 清晰度：`mode`；720P (std) / 1080P (pro) / 4K (4k)；初值 `std`。
- 视频时长：`duration`；3-15 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `false`。

### Kling 3.0 Motion Control

- 分组：专用；选项：`kling-v3-motion-control`；上游型号：`kling-v3-motion-control`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/kling-v2-6/kling-v2-6-motion-control-generation)。
- 清晰度：`mode`；标准 (std) / 高品质 (pro)；初值 `std`。

### Wan 3.0

- 分组：常用；选项：`wan3.0-video`；上游型号：`wan3.0-video`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/wan3.0-video/generation)。
- 比例：`size`；adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `adaptive`。
- 清晰度：`resolution`；480P / 720P / 1080P；初值 `1080P`。
- 视频时长：`duration`；2-30 秒; -1: 自动时长；初值 `5`。
- 生成音频：`audio`；true / false；初值 `true`。

### Wan 3.0 Prime

- 分组：候选；选项：`wan3.0-video-prime`；上游型号：`wan3.0-video-prime`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/wan3.0-video/generation)。
- 比例：`size`；adaptive / 16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `adaptive`。
- 清晰度：`resolution`；480P / 720P / 1080P；初值 `1080P`。
- 视频时长：`duration`；2-30 秒; -1: 自动时长；初值 `5`。
- 生成音频：`audio`；true / false；初值 `true`。

### MiniMax H3

- 分组：常用；选项：`MiniMax-H3`；上游型号：`MiniMax-H3`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/minimax-h3/generation)。
- 比例：`aspect_ratio`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9；初值 `16:9`。
- 清晰度：`resolution`；768P / 2K；初值 `2K`。
- 视频时长：`duration`；4-15 秒；初值 `5`。
- 生成音频：`audio`；固定 true。

### MiniMax H3 Max

- 分组：常用；选项：`MiniMax-H3-Max`；上游型号：`MiniMax-H3-Max`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/minimax-h3/max)。
- 比例：`aspect_ratio`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9；初值 `16:9`。
- 清晰度：`resolution`；480P / 768P / 1080P；初值 `768P`。
- 视频时长：`duration`；5-15 秒；初值 `5`。
- 生成音频：`audio`；固定 true。

### MiniMax H3 Regeneration

- 分组：专用；选项：`MiniMax-H3-Regeneration`；上游型号：`MiniMax-H3-Regeneration`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/minimax-h3/regeneration)。
- 清晰度：`resolution`；固定 2K。
- 生成音频：`audio`；固定 true。

### HappyHorse 1.1

- 分组：常用；选项：`happyhorse-1.1`；上游型号：`happyhorse-1.1`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/happyhorse-1.1/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；720P / 1080P；初值 `1080P`。
- 视频时长：`duration`；3-15 秒；初值 `5`。

### HappyHorse 1.0

- 分组：专用；选项：`happyhorse-1.0`；上游型号：`happyhorse-1.0`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/happyhorse-1.0/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；720P / 1080P；初值 `1080P`。
- 视频时长：`duration`；3-15 秒；初值 `5`。

### Gemini Omni 1.1 Flash

- 分组：常用；选项：`gemini-omni-1.1-flash`；上游型号：`gemini-omni-1.1-flash`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/gemini-omni-1.1-flash/generation)。
- 比例：`aspect_ratio`；16:9 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；360p / 720p / 1080p / 4k；初值 `720p`。
- 生成音频：`audio`；固定 true。

### Vidu Q4 Preview

- 分组：常用；选项：`viduq4-preview`；上游型号：`viduq4-preview`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/vidu-q4-preview/generation)。
- 比例：`aspect_ratio`；16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；540p / 720p / 1080p / 2K / 4K；初值 `720p`。
- 视频时长：`duration`；3-16 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `true`。

### Vidu Q3 Pro

- 分组：候选；选项：`viduq3-pro`；上游型号：`viduq3-pro`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/vidu-q3-pro/generation)。
- 比例：`aspect_ratio`；16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；540p / 720p / 1080p；初值 `720p`。
- 视频时长：`duration`；1-16 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `true`。

### Grok Imagine Video 1.5

- 分组：常用；选项：`grok-imagine-video-1.5`；上游型号：`grok-imagine-video-1.5`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/grok-imagine/official)。
- 比例：`aspect_ratio`；auto / 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3；初值 `auto`。
- 清晰度：`resolution`；480p / 720p / 1080p；初值 `480p`。
- 视频时长：`duration`；1-15 秒；初值 `8`。

### FLUX 3 Video

- 分组：候选；选项：`flux-3-video`；上游型号：`flux-3-video`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/flux-3-video/generation)。
- 比例：`aspect_ratio`；auto / 21:9 / 2:1 / 16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `auto`。
- 清晰度：`resolution`；hd / fhd；初值 `hd`。
- 视频时长：`duration`；5-20 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `true`。

### PixVerse V6

- 分组：候选；选项：`pixverse-v6`；上游型号：`pixverse-v6`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/pixverse-v6/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 3:2 / 2:3 / 21:9；初值 `16:9`。
- 清晰度：`resolution`；360p / 540p / 720p / 1080p；初值 `540p`。
- 视频时长：`duration`；1-15 秒；初值 `5`。
- 生成音频：`audio`；true / false；初值 `false`。

### Veo 3.1 Quality

- 分组：候选；选项：`veo3.1-quality-official`；上游型号：`veo3.1-quality-official`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/veo3/generation-official)。
- 比例：`aspect_ratio`；16:9 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；720p / 1080p / 4K；初值 `720p`。
- 视频时长：`duration`；4 / 6 / 8；初值 `8`；联动 `[{"when":{"resolution":"4K"},"enum":[8]}]`。
- 生成数量：`sample_count`；1-4 个；初值 `1`。
- 生成音频：`generate_audio`；true / false；初值 `false`。

### Veo 3.1 Fast

- 分组：候选；选项：`veo3.1-fast-official`；上游型号：`veo3.1-fast-official`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/veo3/generation-official)。
- 比例：`aspect_ratio`；16:9 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；720p / 1080p；初值 `720p`。
- 视频时长：`duration`；4 / 6 / 8；初值 `8`；联动 `[{"when":{"resolution":"4K"},"enum":[8]}]`。
- 生成数量：`sample_count`；1-4 个；初值 `1`。
- 生成音频：`generate_audio`；true / false；初值 `false`。

### SkyReels V4 Std

- 分组：候选；选项：`skyreels-v4-std`；上游型号：`skyreels-v4-std`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/skyreels-v4/generation)。
- 比例：`aspect_ratio`；16:9 / 4:3 / 1:1 / 3:4 / 9:16；初值 `16:9`。
- 清晰度：`resolution`；480p / 720p / 1080p；初值 `1080p`。
- 视频时长：`duration`；3-15 秒；初值 `5`。

### Wan 2.7 VideoEdit

- 分组：专用；选项：`wan2.7-videoedit`；上游型号：`wan2.7-videoedit`。
- 来源：[APIMart 模型文档](https://docs.apimart.ai/en/api-reference/videos/wan2.7-videoedit/generation)。
- 比例：`size`；16:9 / 4:3 / 1:1 / 3:4 / 9:16。
- 清晰度：`resolution`；720P / 1080P；初值 `1080P`。
- 视频时长：`duration`；2-10 秒; 0: 原视频时长；初值 `0`。
- 生成音频：`audio_setting`；重新生成 (`auto`) / 保留原音频 (`origin`)；初值 `auto`。
