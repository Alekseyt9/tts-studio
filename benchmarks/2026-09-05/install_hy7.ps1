. "$PSScriptRoot\env.ps1"
$env:OLLAMA_HOST='127.0.0.1:11435'
while (!(Test-Path 'F:\src\tts\models\Hy-MT2-7B-GGUF\download-provenance.json')) { Start-Sleep -Seconds 2 }
& F:\programs\Ollama\ollama.exe create hy-mt2:7b-q6_k -f "$PSScriptRoot\Hy7.Modelfile" *> "$PSScriptRoot\create-hy7.log"
