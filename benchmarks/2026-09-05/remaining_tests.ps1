. "$PSScriptRoot\env.ps1"
Set-Location F:\src\tts
while (!(Test-Path "$PSScriptRoot\translations-matched\gemma4_direct\metrics.json")) { Start-Sleep -Seconds 2 }
& .venv-faster\Scripts\python.exe "$PSScriptRoot\evaluate_audio.py" *> "$PSScriptRoot\audio-quality-run.log"
while (!(Test-Path 'F:\src\tts\models\OmniVoice\download-provenance.json')) { Start-Sleep -Seconds 2 }
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_tts.py" omni32 *> "$PSScriptRoot\tts-omni32-matched.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_tts.py" omni16 *> "$PSScriptRoot\tts-omni16-matched.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\evaluate_audio.py" *> "$PSScriptRoot\audio-quality-run.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_translation.py" translategemma_4b translategemma_12b *> "$PSScriptRoot\translation-tg-matched.log"
& .venv-faster\Scripts\python.exe "$PSScriptRoot\bench_translation.py" hy_mt2_1_8b hy_mt2_7b *> "$PSScriptRoot\translation-hy-matched.log"
