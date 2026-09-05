$env:HF_HOME = "F:\src\tts\.cache\huggingface"
$env:HF_HUB_CACHE = "F:\src\tts\.cache\huggingface\hub"
$env:TEMP = "F:\src\tts\.cache\temp"
$env:TMP = $env:TEMP
$env:TMPDIR = $env:TEMP
$env:HF_HUB_OFFLINE = "1"
$env:PYTHONUTF8 = "1"
$env:PYTHONIOENCODING = "utf-8"
$env:TORCH_HOME = "F:\src\tts\.cache\torch"
$env:TORCHINDUCTOR_CACHE_DIR = "F:\src\tts\.cache\torchinductor"
$env:TRITON_CACHE_DIR = "F:\src\tts\.cache\triton"
$env:CUDA_CACHE_PATH = "F:\src\tts\.cache\cuda"
$env:NUMBA_CACHE_DIR = "F:\src\tts\.cache\numba"
$env:XDG_CACHE_HOME = "F:\src\tts\.cache"

$ttsPython = "F:\src\tts\.venv\Scripts\python.exe"
$ttsEngineIndex = [Array]::IndexOf($args, "--engine")
if ($ttsEngineIndex -ge 0 -and $args[$ttsEngineIndex + 1] -ne "qwen") {
    $ttsPython = "F:\src\tts\.venv-faster\Scripts\python.exe"
}
& $ttsPython "F:\src\tts\tts.py" @args
exit $LASTEXITCODE
