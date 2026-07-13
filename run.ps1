$env:HF_HOME = "F:\src\tts\.cache\huggingface"
$env:HF_HUB_CACHE = "F:\src\tts\.cache\huggingface\hub"
$env:TEMP = "F:\src\tts\.cache\temp"
$env:TMP = $env:TEMP

& "F:\src\tts\.venv\Scripts\python.exe" "F:\src\tts\tts.py" @args
