. "$PSScriptRoot\env.ps1"
Set-Location F:\src\tts
while (!(Test-Path "$PSScriptRoot\tts-matched\qwen\metrics.json")) { Start-Sleep -Seconds 2 }
& .venv-faster\Scripts\python.exe "$PSScriptRoot\evaluate_audio.py" reference *> "$PSScriptRoot\reference-asr.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_tts.py" faster *> "$PSScriptRoot\tts-faster-matched.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_translation.py" gemma4_think gemma4_direct *> "$PSScriptRoot\translation-gemma-matched.log"
