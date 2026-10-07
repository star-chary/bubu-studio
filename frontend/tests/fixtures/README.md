# 本地媒体测试样本

这两个小文件由 FFmpeg 的 `testsrc2` 测试图生成，不包含用户素材。用于真实浏览器验证图片解码、媒体尺寸、视频播放和批量导入；运行测试不依赖 FFmpeg。

- `sample-image.png`：960 × 540 PNG。
- `sample-video.mp4`：320 × 240，3 秒、12 fps、H.264 / yuv420p、无音轨。

第三步补充的自制参考样本（2026-10-02 已经后端 ffprobe、浏览器、OSS 验证）：

- `reference-video.mp4`：854 × 480，2 秒，24 fps，H.264 / yuv420p，无音轨，283035 字节。
- `reference-audio.wav`：2 秒，48 kHz，单声道，PCM s16le，自制节奏，192078 字节。
- `reference-audio.mp3`：同一段节奏，MP3 / 96 kbps，24812 字节；实际探测时长 2 秒。

在项目根运行 `./scripts/create-reference-fixtures.ps1` 可重新生成这三份文件，需 FFmpeg。不同 FFmpeg 版本可能产生不同字节和哈希，按实际文件重新验收。上述 2 秒是输入参考长度，后续真实生成输出默认 4 秒 / 480p。

需要重新生成时，在 `frontend/` 下执行：

```powershell
ffmpeg -f lavfi -i 'testsrc2=size=960x540:rate=1' -frames:v 1 -update 1 tests/fixtures/sample-image.png
ffmpeg -f lavfi -i 'testsrc2=size=320x240:rate=12' -t 3 -c:v libx264 -pix_fmt yuv420p -movflags +faststart tests/fixtures/sample-video.mp4
```
