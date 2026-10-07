$ErrorActionPreference = 'Stop'
$fixtureRoot = Join-Path (Split-Path -Parent $PSScriptRoot) 'frontend/tests/fixtures'
# Synthetic test patterns and rhythm; no user material or paid generation.
& ffmpeg -hide_banner -loglevel error -y -f lavfi -i 'testsrc2=size=854x480:rate=24' -t 2 -an -c:v libx264 -pix_fmt yuv420p -movflags +faststart (Join-Path $fixtureRoot 'reference-video.mp4')
if ($LASTEXITCODE -ne 0) { throw 'Video fixture generation failed' }
& ffmpeg -hide_banner -loglevel error -y -f lavfi -i 'aevalsrc=0.2*sin(2*PI*440*t)*exp(-12*mod(t\,0.5)):s=48000:d=2' -c:a pcm_s16le (Join-Path $fixtureRoot 'reference-audio.wav')
if ($LASTEXITCODE -ne 0) { throw 'WAV fixture generation failed' }
& ffmpeg -hide_banner -loglevel error -y -i (Join-Path $fixtureRoot 'reference-audio.wav') -c:a libmp3lame -b:a 96k (Join-Path $fixtureRoot 'reference-audio.mp3')
if ($LASTEXITCODE -ne 0) { throw 'MP3 fixture generation failed' }
