import { useEffect, useRef, useState } from 'react';
import {
  ArrowClockwise, Check, CheckCircle, Clock, DownloadSimple, FileText, Headphones, MagnifyingGlass,
  Pause, Play, Plus, Power, SpeakerHigh, SpinnerGap, Translate, Trash,
  WarningCircle, Waveform, X,
} from '@phosphor-icons/react';
import './styles.css';

const statusText = { queued:'В очереди', translating:'Перевод', unloading_translation:'Выгрузка переводчика', loading_tts:'Загрузка TTS', running:'Озвучка', pausing:'Останавливаем', paused:'Остановлено', ready:'Готово', failed:'Ошибка', merging:'Склейка' };
const workingStatuses = ['translating','unloading_translation','loading_tts','running','merging','pausing'];
const voiceDuration = seconds => seconds >= 60 ? `${Math.floor(seconds/60)}:${String(Math.round(seconds%60)).padStart(2,'0')}` : `${Math.round(seconds||0)} сек`;
const runtime = seconds => { const value=Math.max(0,Math.round(seconds||0)); return value>=60?`${Math.floor(value/60)}:${String(value%60).padStart(2,'0')}`:`0:${String(value).padStart(2,'0')}` };

function Status({value, compact=false}) {
  const Icon=value==='ready'?CheckCircle:value==='failed'?WarningCircle:value==='queued'?Clock:value==='paused'?Pause:SpinnerGap;
  return <span className={`status ${value} ${compact?'compact-status':''}`}><Icon weight="bold" className={workingStatuses.includes(value)?'spin':''}/>{statusText[value]||value}</span>;
}

function Toggle({checked,onChange,label}) {
  return <button type="button" className={`toggle ${checked?'on':''}`} onClick={onChange} role="switch" aria-checked={checked} aria-label={label}><i/></button>;
}

function App() {
  const [models,setModels]=useState({tts:[],translation:[]});
  const [ttsModel,setTTSModel]=useState('faster'), [translationModel,setTranslationModel]=useState('gemma4_direct');
  const omni=ttsModel.startsWith('omni');
  const modelName=(kind,id)=>models[kind].find(m=>m.id===id)?.name||id;
  const speedLabel=m=>`${m.name} — ${m.seconds.toFixed(1)} с · ×${m.speedup.toFixed(1)}`;
  const selectedTTS=models.tts.find(m=>m.id===ttsModel);
  const selectedTranslation=models.translation.find(m=>m.id===translationModel);
  useEffect(()=>{fetch('/api/models').then(r=>r.json()).then(setModels).catch(e=>setError(`Не удалось загрузить модели: ${e.message}`))},[]);
  const chunkAudioRefs=useRef(new Map());
  const sourceFileInput=useRef(null);
  const [text,setText]=useState(''), [sourceFileName,setSourceFileName]=useState(''), [chunkSize,setChunkSize]=useState(1200), [translationChunkSize,setTranslationChunkSize]=useState(4000), [autoMerge,setAutoMerge]=useState(true);
  const [refAudio,setRefAudio]=useState('reference_audio_2.wav'), [refText,setRefText]=useState(''), [speakerOnly,setSpeakerOnly]=useState(true);
  const [settingsLoaded,setSettingsLoaded]=useState(false), [transcriptOpen,setTranscriptOpen]=useState(false);
  const [voices,setVoices]=useState([]), [voiceOpen,setVoiceOpen]=useState(false), [voiceSearch,setVoiceSearch]=useState('');
  const [jobs,setJobs]=useState([]), [selected,setSelected]=useState(null), [health,setHealth]=useState({mode:'checking',model_loaded:false,translation_model:'gemma4:12b',translation_context:16384});
  const [busy,setBusy]=useState(false), [error,setError]=useState(''), [closed,setClosed]=useState(false);

  const load=async()=>{try{const [nextHealth,list]=await Promise.all([fetch('/api/health').then(r=>r.json()),fetch('/api/jobs').then(r=>r.json())]);setHealth(nextHealth);setJobs(list||[]);if(!selected&&list?.length)setSelected(list[0].id)}catch{if(!closed)setHealth(h=>({...h,mode:'offline'}))}};
  useEffect(()=>{load();const timer=setInterval(load,1500);return()=>clearInterval(timer)},[selected]);
  useEffect(()=>{fetch('/api/voices').then(r=>r.json()).then(setVoices).catch(()=>setVoices([]))},[]);
  useEffect(()=>{fetch('/api/settings').then(async r=>{if(!r.ok)throw new Error(await r.text());return r.json()}).then(settings=>{setTTSModel(settings.tts_model||'faster');setTranslationModel(settings.translation_model||'gemma4_direct');setRefAudio(settings.ref_audio);setRefText(settings.ref_text||'');setSpeakerOnly(Boolean(settings.speaker_only));setChunkSize(Number(settings.chunk_size)||1200);setTranslationChunkSize(Number(settings.translation_chunk_size)||4000);setAutoMerge(Boolean(settings.auto_merge));setSettingsLoaded(true)}).catch(e=>{setError(`Не удалось загрузить настройки: ${e.message}`);setSettingsLoaded(true)})},[]);
  useEffect(()=>{if(!settingsLoaded)return;const timer=setTimeout(async()=>{try{const res=await fetch('/api/settings',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({tts_model:ttsModel,translation_model:translationModel,ref_audio:refAudio,ref_text:refText,speaker_only:speakerOnly,chunk_size:Number(chunkSize),translation_chunk_size:Number(translationChunkSize),auto_merge:autoMerge})});if(!res.ok)throw new Error(await res.text())}catch(e){setError(`Не удалось сохранить настройки: ${e.message}`)}},600);return()=>clearTimeout(timer)},[settingsLoaded,refAudio,refText,speakerOnly,chunkSize,translationChunkSize,autoMerge,ttsModel,translationModel]);

  const active=jobs.find(j=>j.id===selected)||jobs[0];
  const failedChunkIndex=active?.chunks?.findIndex(chunk=>chunk.status==='failed')??-1;
  const retryLabel=active?.translation_status!=='ready'
    ? `Повторить ${Math.min((active?.translation_chunk||0)+1,active?.translation_chunks||1)}-й чанк`
    : failedChunkIndex>=0 ? `Повторить ${failedChunkIndex+1}-й фрагмент` : 'Повторить задание';
  const selectedVoice=voices.find(v=>v.ref_audio===refAudio);
  const filteredVoices=voices.filter(v=>`${v.name} ${v.description}`.toLocaleLowerCase('ru-RU').includes(voiceSearch.toLocaleLowerCase('ru-RU')));
  const chars=text.length, ready=active?.chunks?.filter(c=>c.status==='ready').length||0;
  const elapsed=active?.chunks?.reduce((total,c)=>total+(c.synthesis_seconds||c.elapsed_seconds||0),0)||0;
  const average=ready?active.chunks.reduce((total,c)=>total+(c.status==='ready'?(c.synthesis_seconds||0):0),0)/ready:0;
  const remaining=Math.max(0,(active?.chunks?.length||0)-ready)*(average||0);
  const phase=active?.status==='ready'?3:['translating'].includes(active?.status)?1:active?2:0;

  const chooseVoice=voice=>{setRefAudio(voice.ref_audio);if(voice.transcript)setRefText(voice.transcript);setVoiceOpen(false);setVoiceSearch('')};
  const loadTextFile=async event=>{const file=event.target.files?.[0];event.target.value='';if(!file)return;if(!file.name.toLocaleLowerCase().endsWith('.txt')){setError('Выберите файл с расширением .txt');return}try{const content=(await file.text()).replace(/^\uFEFF/,'');setText(content);setSourceFileName(file.name);setError('')}catch(e){setError(`Не удалось прочитать TXT-файл: ${e.message}`)}};
  const submit=async()=>{if(!text.trim()||(!omni&&!speakerOnly&&!refText.trim()))return;setBusy(true);setError('');try{const res=await fetch('/api/jobs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({tts_model:ttsModel,translation_model:translationModel,text,voice:'clone',ref_audio:refAudio,ref_text:refText,speaker_only:speakerOnly,chunk_size:Number(chunkSize),translation_chunk_size:Number(translationChunkSize),auto_merge:autoMerge})});if(!res.ok)throw new Error(await res.text());const job=await res.json();setSelected(job.id);setText('');setSourceFileName('');load()}catch(e){setError(e.message)}finally{setBusy(false)}};
  const merge=async id=>{const res=await fetch(`/api/jobs/${id}/merge`,{method:'POST'});if(!res.ok)setError(await res.text());load()};
  const clearReady=async()=>{await fetch('/api/jobs/ready',{method:'DELETE'});setSelected(null);load()};
  const removeJob=async id=>{if(!window.confirm('Удалить задание из очереди и базы данных? Аудиофайлы на диске останутся.'))return;const res=await fetch(`/api/jobs/${id}`,{method:'DELETE'});if(!res.ok){setError(await res.text());return}if(selected===id)setSelected(null);load()};
  const controlJob=async(job,action)=>{const res=await fetch(`/api/jobs/${job.id}/${action}`,{method:'POST'});if(!res.ok){setError(await res.text());return}load()};
  const chunkAudioKey=(job,chunk)=>`${job.id}:${chunk.id}`;
  const stopOtherChunks=currentKey=>{for(const [key,audio] of chunkAudioRefs.current){if(key!==currentKey&&!audio.paused)audio.pause()}};
  const playNextChunk=(job,index)=>{
    for(let next=index+1;next<job.chunks.length;next++){
      const chunk=job.chunks[next];
      if(chunk.status!=='ready'||!chunk.audio_url)continue;
      const audio=chunkAudioRefs.current.get(chunkAudioKey(job,chunk));
      if(!audio)continue;
      audio.currentTime=0;
      audio.play().catch(()=>{});
      return;
    }
  };
  const closeStudio=async()=>{if(!window.confirm('Выгрузить модель из видеопамяти и закрыть TTS Студию?'))return;setClosed(true);try{await fetch('/api/shutdown',{method:'POST'})}catch{}};
  const modelLabel=health.mode==='offline'?'Сервер недоступен':active?.status==='translating'?`${modelName('translation',active.translation_model)} · 16K`:active?.status==='unloading_translation'?'Переводчик выгружается':active?.status==='loading_tts'?`Загружается ${modelName('tts',active.tts_model)}`:health.model_loaded?`${modelName('tts',health.tts_model||active?.tts_model||ttsModel)} · запущена`:'Модели выгружены';

  if(closed)return <div className="closed-screen"><Power/><h1>TTS Студия закрыта</h1><p>Модели выгружены из видеопамяти.</p></div>;

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><div className="brand-icon"><Waveform weight="bold"/></div><div><strong>TTS Студия</strong><span>Локальный перевод и озвучка</span></div></div>
      <div className="server-state"><i className={health.mode==='offline'?'offline':health.model_loaded?'online':'idle'}/><span>{modelLabel}</span></div>
      <button className="power-button" onClick={closeStudio} disabled={jobs.some(j=>workingStatuses.includes(j.status))} title="Выгрузить модели и закрыть"><Power/><span>Остановить</span></button>
    </header>

    <main className="studio-grid">
      <aside className="queue-rail">
        <div className="rail-heading"><div><span>ОЧЕРЕДЬ</span><b>{jobs.length}</b></div><button onClick={clearReady} title="Удалить завершённые"><Trash/>Очистить</button></div>
        <div className="queue-scroll">{jobs.length===0?<div className="rail-empty"><Clock/><span>Очередь пуста</span></div>:jobs.map((job,index)=>{const done=job.chunks.filter(c=>c.status==='ready').length;return <div className={`queue-card ${active?.id===job.id?'selected':''}`} key={job.id}>
          <button className="queue-card-main" onClick={()=>setSelected(job.id)}>
            <span className="queue-number">{index+1}</span><span className="queue-copy"><strong>{job.title}</strong><small>{job.status==='translating'?`Перевод ≈ ${job.translation_progress}%`:job.chunks.length?`${done}/${job.chunks.length} фрагм.`:'Подготовка'} · {new Date(job.created_at).toLocaleTimeString('ru-RU',{hour:'2-digit',minute:'2-digit'})}</small><i><em style={{width:`${job.progress}%`}}/></i></span><Status value={job.status} compact/>
          </button>
          {active?.id===job.id&&!['ready','failed','pausing'].includes(job.status)&&<button className="queue-pause" onClick={()=>controlJob(job,'pause')} title="Остановить"><Pause/></button>}
          {job.status==='paused'&&<button className="queue-pause resume" onClick={()=>controlJob(job,'resume')} title="Продолжить"><Play/></button>}
          {job.status==='failed'&&<button className="queue-pause resume" onClick={()=>controlJob(job,'retry')} title={job.translation_status==='ready'?'Повторить незавершённый аудиофрагмент':'Повторить незавершённый переводческий чанк'}><ArrowClockwise/></button>}
          <button className="queue-remove" onClick={()=>removeJob(job.id)} title="Удалить"><Trash/></button>
        </div>})}</div>
      </aside>

      <section className="active-workspace">
        {!active?<div className="active-empty"><Headphones/><strong>Нет активного задания</strong><span>Создайте его в панели справа</span></div>:<>
          <div className="active-header"><div><span>АКТИВНОЕ ЗАДАНИЕ</span><h1>{active.title}</h1></div>{active.status==='failed'?<button className="job-action resume" onClick={()=>controlJob(active,'retry')}><ArrowClockwise/>{retryLabel}</button>:active.status==='paused'?<button className="job-action resume" onClick={()=>controlJob(active,'resume')}><Play/>Продолжить</button>:!['ready','pausing'].includes(active.status)&&<button className="job-action" onClick={()=>controlJob(active,'pause')}><Pause/>Пауза</button>}</div>
          <div className="job-models">{modelName('translation',active.translation_model||'gemma4_think')} → {modelName('tts',active.tts_model||'qwen')}</div>
          <div className="phase-track">
            <div className={`phase ${phase>=1?'done':''}`}><i><Check/></i><strong>Перевод</strong><small>{active.translation_chunks?`${Math.min(active.translation_chunk,active.translation_chunks)}/${active.translation_chunks} чанков`:'Ожидание'}</small></div>
            <div className={`phase ${phase===2?'current':phase>2?'done':''}`}><i><Waveform/></i><strong>Озвучка</strong><small>{ready}/{active.chunks.length||0} фрагм.</small></div>
            <div className={`phase ${phase===3?'done':''}`}><i><Check/></i><strong>Готово</strong><small>{phase===3?'Завершено':'Ожидание'}</small></div>
          </div>
          <div className="job-summary"><div><span>ПЕРЕВОД</span><strong>{active.translation_progress}%</strong><i><em style={{width:`${active.translation_progress}%`}}/></i></div><div><span>ПРОГРЕСС ОЗВУЧКИ</span><strong>{ready}/{active.chunks.length||0} фрагм. <small>{active.progress}%</small></strong><i><em style={{width:`${active.progress}%`}}/></i></div><div><span>ПРОШЛО / ОСТАЛОСЬ</span><strong>{runtime(elapsed)} <small>{remaining?`/ ≈ ${runtime(remaining)}`:'—'}</small></strong></div></div>
          <div className="result-actions">
            <a className={!active.translation_url?'disabled':''} href={active.translation_url||undefined} download><DownloadSimple/>Скачать перевод</a>
            <a className={!active.merged_url?'disabled':''} href={active.merged_url||undefined} download><DownloadSimple/>Скачать итог</a>
            <button disabled={!active.chunks.length||ready!==active.chunks.length||active.status==='merging'} onClick={()=>merge(active.id)}><Waveform/>{active.status==='merging'?'Склеиваем…':'Склеить вручную'}</button>
          </div>
          {active.chunks.length===0?(active.status==='failed'?<div className="failure-state"><WarningCircle/><strong>Перевод остановлен</strong><span>{active.error_message||'Не удалось завершить переводческий чанк'}</span><small>Готово {active.translation_chunk} из {active.translation_chunks||1} чанков</small><button onClick={()=>controlJob(active,'retry')}><ArrowClockwise/>Повторить {Math.min(active.translation_chunk+1,active.translation_chunks||1)}-й чанк</button></div>:<div className="phase-wait"><SpinnerGap className={workingStatuses.includes(active.status)?'spin':''}/><strong>{statusText[active.status]}</strong><span>{active.status==='translating'?`Чанк ${Math.min(active.translation_chunk+1,active.translation_chunks||1)} из ${active.translation_chunks||1}${active.translation_sections?` · секция ${active.translation_section} из ${active.translation_sections}`:''} · ≈ ${active.translation_progress}%${active.translation_attempt>1?` · попытка ${active.translation_attempt}`:''}`:active.status==='paused'?'Незавершённый чанк начнётся с начала':'Подготовка модели'}</span></div>):<div className="chunk-table"><div className="chunk-table-head"><span>#</span><span>ФРАГМЕНТ</span><span>СТАТУС</span><span>АУДИО</span><span>ВРЕМЯ</span><span></span></div><div className="chunk-scroll">{active.chunks.map((chunk,index)=><div className={`chunk-row ${chunk.status==='running'?'active':''}`} key={chunk.id}>
            <span>{index+1}</span><span className="chunk-name"><strong>{chunk.start}–{chunk.end}</strong><small>{chunk.characters} знаков</small></span><span className="chunk-state"><Status value={chunk.status}/>{chunk.status==='running'&&<small>≈ {chunk.progress||5}%</small>}</span>
            <span className="chunk-audio">{chunk.status==='ready'?<><audio controls preload="none" src={chunk.audio_url} ref={node=>{const key=chunkAudioKey(active,chunk);if(node)chunkAudioRefs.current.set(key,node);else chunkAudioRefs.current.delete(key)}} onPlay={()=>stopOtherChunks(chunkAudioKey(active,chunk))} onEnded={()=>playNextChunk(active,index)}/><a href={chunk.audio_url} download title="Скачать"><DownloadSimple/></a></>:<i><em style={{width:chunk.status==='running'?`${chunk.progress||5}%`:'0%'}}/></i>}</span>
            <span className="chunk-time">{chunk.status==='running'?<><strong>{runtime(chunk.elapsed_seconds)}</strong><small>прошло</small></>:chunk.status==='ready'?<><strong>{chunk.synthesis_seconds>0?runtime(chunk.synthesis_seconds):'—'}</strong><small>{chunk.duration?`${chunk.duration.toFixed(1)} с аудио`:'готово'}</small></>:<strong>—</strong>}</span><span></span>
          </div>)}</div></div>}
          <div className="active-footer"><span>Общее время <strong>{runtime(elapsed)}</strong></span><span>Озвучено <strong>{ready}/{active.chunks.length||0}</strong></span><span>Готово через <strong>{remaining?`≈ ${runtime(remaining)}`:'—'}</strong></span></div>
        </>}
      </section>

      <aside className="create-panel">
        <div className="create-heading"><span>СОЗДАТЬ ЗАДАНИЕ</span><button type="button" onClick={()=>sourceFileInput.current?.click()} title={sourceFileName||'Выбрать TXT-файл'}><FileText/><span>{sourceFileName||'TXT-файл'}</span></button><input ref={sourceFileInput} type="file" accept=".txt,text/plain" onChange={loadTextFile}/></div>
        <div className="source-editor"><textarea value={text} onChange={e=>setText(e.target.value)} placeholder="Вставьте английский текст или выберите TXT-файл…"/><span>{chars.toLocaleString('ru-RU')} знаков</span></div>
        <div className="voice-title"><span>ВЫБРАННЫЙ ГОЛОС</span><button onClick={()=>setVoiceOpen(true)}><Headphones/>Библиотека голосов</button></div>
        <div className="voice-preview"><SpeakerHigh/><strong>{selectedVoice?.name||'Загрузка…'}</strong>{selectedVoice&&<audio controls preload="metadata" src={selectedVoice.audio_url}/>}</div>
        <button className={`transcript-toggle ${transcriptOpen?'open':''}`} onClick={()=>setTranscriptOpen(!transcriptOpen)}><span>ТРАНСКРИПТ</span><b>{transcriptOpen?'−':'+'}</b></button>
        {transcriptOpen&&<input className="transcript-input" value={refText} onChange={e=>setRefText(e.target.value)} disabled={speakerOnly&&!omni} placeholder={omni?'Пусто — распознать образец автоматически':speakerOnly?'Не нужен в режиме «только тембр»':'Точный текст образца'}/>} 
        <div className="settings-title">МОДЕЛИ</div>
        <label className="model-setting"><span>Переводчик</span><select aria-label="Модель перевода" value={translationModel} onChange={e=>setTranslationModel(e.target.value)}>{models.translation.map(m=><option key={m.id} value={m.id}>{speedLabel(m)}</option>)}</select></label>
        {selectedTranslation?.note&&<p className="model-note">{selectedTranslation.note}</p>}
        <label className="model-setting"><span>Озвучка</span><select aria-label="Модель озвучки" value={ttsModel} onChange={e=>setTTSModel(e.target.value)}>{models.tts.map(m=><option key={m.id} value={m.id}>{speedLabel(m)}</option>)}</select></label>
        {selectedTTS?.note&&<p className="model-note">{selectedTTS.note}</p>}
        <details className="benchmark-note"><summary>Как сравнивалась скорость</summary><p>Один прогон на RTX 5070: перевод — 3 904 знака, контекст 16K; озвучка — 1 095 знаков, один голос. × — ускорение относительно Gemma с рассуждениями или обычного Qwen3-TTS. Меньше секунд — быстрее.</p><p>Время после прогрева, без загрузки. Первый запуск дольше. Загрузка выбранного TTS: {selectedTTS?.load_seconds?.toFixed(1)||'—'} с. Прогрев: {selectedTTS?.warmup_seconds?.toFixed(1)||'—'} с. Темп речи у моделей отличается.</p></details>
        <div className="settings-title">НАСТРОЙКИ</div>
        <label className="setting-row"><span>Чанк перевода</span><select value={translationChunkSize} onChange={e=>setTranslationChunkSize(e.target.value)}><option value="2500">2 500 знаков</option><option value="4000">4 000 знаков</option><option value="6000">6 000 знаков</option></select></label>
        <label className="setting-row"><span>Размер фрагмента</span><select value={chunkSize} onChange={e=>setChunkSize(e.target.value)}><option value="600">600 знаков</option><option value="1200">1 200 знаков</option><option value="2400">2 400 знаков</option></select></label>
        <div className="setting-row"><span>Автосклейка</span><div><Toggle checked={autoMerge} onChange={()=>setAutoMerge(!autoMerge)} label="Автосклейка"/><small>{autoMerge?'Включена':'Выключена'}</small></div></div>
        {!omni&&<div className="setting-row"><span>Только тембр</span><div><Toggle checked={speakerOnly} onChange={()=>setSpeakerOnly(!speakerOnly)} label="Только тембр"/><small>{speakerOnly?'Включён':'Выключен'}</small></div></div>}
        <button className="add-job" disabled={!text.trim()||busy||health.mode==='offline'||(!omni&&!speakerOnly&&!refText.trim())} onClick={submit}><Plus weight="bold"/>{busy?'Добавляем…':'Добавить в очередь'}</button>
      </aside>
    </main>

    {voiceOpen&&<div className="voice-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)setVoiceOpen(false)}}><section className="voice-dialog"><header><div><strong>Библиотека голосов</strong><span>{filteredVoices.length} из {voices.length}</span></div><label><MagnifyingGlass/><input autoFocus value={voiceSearch} onChange={e=>setVoiceSearch(e.target.value)} placeholder="Найти голос…"/></label><button onClick={()=>setVoiceOpen(false)}><X/></button></header><div className="voice-list">{filteredVoices.length===0?<div className="voice-empty">Ничего не найдено</div>:filteredVoices.map(voice=><article className={voice.ref_audio===refAudio?'selected':''} key={voice.id}><SpeakerHigh/><div><strong>{voice.name}</strong><small>{voice.description} · {voiceDuration(voice.duration)}</small></div><audio controls preload="none" src={voice.audio_url}/><button onClick={()=>chooseVoice(voice)}>{voice.ref_audio===refAudio?<><Check/>Выбран</>:<>Выбрать</>}</button></article>)}</div></section></div>}
    {error&&<button className="error-toast" onClick={()=>setError('')}><WarningCircle/><span>{error}</span><X/></button>}
  </div>;
}

export { App };
