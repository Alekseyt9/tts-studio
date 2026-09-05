from pathlib import Path
root=Path(__file__).resolve().parents[2]
p=root/'web/src/App.jsx'
s=p.read_text(encoding='utf-8')
s=s.replace("unloading_translation:'Выгрузка Gemma'", "unloading_translation:'Выгрузка переводчика'")
s=s.replace("  const chunkAudioRefs=", "  const [models,setModels]=useState({tts:[],translation:[]});\n  const [ttsModel,setTTSModel]=useState('faster'), [translationModel,setTranslationModel]=useState('gemma4_direct');\n  const omni=ttsModel.startsWith('omni');\n  const modelName=(kind,id)=>models[kind].find(m=>m.id===id)?.name||id;\n  const speedLabel=m=>`${m.name} — ${m.seconds.toFixed(1)} с · ×${m.speedup.toFixed(1)}`;\n  const selectedTTS=models.tts.find(m=>m.id===ttsModel);\n  const selectedTranslation=models.translation.find(m=>m.id===translationModel);\n  useEffect(()=>{fetch('/api/models').then(r=>r.json()).then(setModels).catch(e=>setError(`Не удалось загрузить модели: ${e.message}`))},[]);\n  const chunkAudioRefs=")
s=s.replace("setRefAudio(settings.ref_audio);", "setTTSModel(settings.tts_model||'faster');setTranslationModel(settings.translation_model||'gemma4_direct');setRefAudio(settings.ref_audio);")
s=s.replace("JSON.stringify({ref_audio:", "JSON.stringify({tts_model:ttsModel,translation_model:translationModel,ref_audio:")
s=s.replace("JSON.stringify({text,", "JSON.stringify({tts_model:ttsModel,translation_model:translationModel,text,")
s=s.replace("translationChunkSize,autoMerge]);", "translationChunkSize,autoMerge,ttsModel,translationModel]);")
s=s.replace("'Gemma переводит · 16K'", "`${modelName('translation',active.translation_model)} · 16K`")
s=s.replace("'Gemma выгружается'", "'Переводчик выгружается'")
s=s.replace("'Загружается Qwen3‑TTS'", "`Загружается ${modelName('tts',active.tts_model)}`")
s=s.replace("'Qwen3‑TTS запущена'", "`${modelName('tts',health.tts_model||active?.tts_model||ttsModel)} · запущена`")
s=s.replace('Gemma 4 → Qwen3‑TTS', 'Локальный перевод и озвучка')
s=s.replace('<div className="phase-track">', '<div className="job-models">{modelName(\'translation\',active.translation_model||\'gemma4_think\')} → {modelName(\'tts\',active.tts_model||\'qwen\')}</div>\n          <div className="phase-track">')
s=s.replace('<div className="settings-title">НАСТРОЙКИ</div>', '''<div className="settings-title">МОДЕЛИ</div>
        <label className="model-setting"><span>Переводчик</span><select aria-label="Модель перевода" value={translationModel} onChange={e=>setTranslationModel(e.target.value)}>{models.translation.map(m=><option key={m.id} value={m.id}>{speedLabel(m)}</option>)}</select></label>
        {selectedTranslation?.note&&<p className="model-note">{selectedTranslation.note}</p>}
        <label className="model-setting"><span>Озвучка</span><select aria-label="Модель озвучки" value={ttsModel} onChange={e=>setTTSModel(e.target.value)}>{models.tts.map(m=><option key={m.id} value={m.id}>{speedLabel(m)}</option>)}</select></label>
        {selectedTTS?.note&&<p className="model-note">{selectedTTS.note}</p>}
        <details className="benchmark-note"><summary>Как сравнивалась скорость</summary><p>Один прогон на RTX 5070: перевод — 3 904 знака, контекст 16K; озвучка — 1 095 знаков, один голос. × — ускорение относительно Gemma с рассуждениями или обычного Qwen3-TTS. Меньше секунд — быстрее.</p><p>Время после прогрева, без загрузки. Первый запуск дольше. Загрузка выбранного TTS: {selectedTTS?.load_seconds?.toFixed(1)||'—'} с. Прогрев: {selectedTTS?.warmup_seconds?.toFixed(1)||'—'} с. Темп речи у моделей отличается.</p></details>
        <div className="settings-title">НАСТРОЙКИ</div>''')
s=s.replace('disabled={speakerOnly}', 'disabled={speakerOnly&&!omni}')
s=s.replace("placeholder={speakerOnly?'Не нужен в режиме «только тембр»':'Точный текст образца'}", "placeholder={omni?'Пусто — распознать образец автоматически':speakerOnly?'Не нужен в режиме «только тембр»':'Точный текст образца'}")
s=s.replace('<div className="setting-row"><span>Только тембр</span>', '{!omni&&<div className="setting-row"><span>Только тембр</span>')
s=s.replace("{speakerOnly?'Включён':'Выключен'}</small></div></div>", "{speakerOnly?'Включён':'Выключен'}</small></div></div>}")
s=s.replace("(!speakerOnly&&!refText.trim())", "(!omni&&!speakerOnly&&!refText.trim())")
p.write_text(s,encoding='utf-8')
p=root/'web/src/styles.css'
with p.open('a',encoding='utf-8') as f:
    f.write('''
.create-panel{overflow-y:auto}.create-panel>*{flex-shrink:0}.create-panel .source-editor{min-height:140px;flex:1 0 140px}.model-setting{display:flex;flex-direction:column;gap:7px;padding:9px 0;font-size:11px;color:#58667d}.model-setting select{width:100%;min-width:0;border:1px solid var(--line);border-radius:6px;padding:9px 7px;background:#fff;color:#17243d;font-size:11px}.model-note{margin:0 0 5px;font-size:10px;line-height:1.5;color:#748096}.benchmark-note{font-size:10px;line-height:1.6;color:#748096;padding:5px 0}.benchmark-note summary{cursor:pointer;color:#2464cd}.benchmark-note p{margin:6px 0}.job-models{font-size:11px;color:#748096;padding:0 24px 10px}.model-setting select:focus-visible{outline:2px solid var(--blue);outline-offset:2px}
''')
