import { useEffect, useState } from 'react';
import {
  ArrowClockwise, Check, CheckCircle, Clock, DownloadSimple, Headphones, MagnifyingGlass,
  Pause, Play, Plus, Power, SpeakerHigh, SpinnerGap, Translate, Trash,
  WarningCircle, Waveform, X,
} from '@phosphor-icons/react';
import './styles.css';

const sample = `A traveler set out on a long journey to see the mountains, seas, and cities he had read about in books. The road was long and difficult, but every new day brought something important.\n\nHe met different people, learned their stories, and gradually understood the world around him better.`;
const statusText = { queued:'В очереди', translating:'Перевод', unloading_translation:'Выгрузка Gemma', loading_tts:'Загрузка TTS', running:'Озвучка', pausing:'Останавливаем', paused:'Остановлено', ready:'Готово', failed:'Ошибка', merging:'Склейка' };
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
  const [text,setText]=useState(sample), [chunkSize,setChunkSize]=useState(1200), [translationChunkSize,setTranslationChunkSize]=useState(4000), [autoMerge,setAutoMerge]=useState(true);
  const [refAudio,setRefAudio]=useState('reference_audio_2.wav'), [refText,setRefText]=useState(''), [speakerOnly,setSpeakerOnly]=useState(true);
  const [settingsLoaded,setSettingsLoaded]=useState(false), [transcriptOpen,setTranscriptOpen]=useState(false);
  const [voices,setVoices]=useState([]), [voiceOpen,setVoiceOpen]=useState(false), [voiceSearch,setVoiceSearch]=useState('');
  const [jobs,setJobs]=useState([]), [selected,setSelected]=useState(null), [health,setHealth]=useState({mode:'checking',model_loaded:false,translation_model:'gemma4:12b',translation_context:16384});
  const [busy,setBusy]=useState(false), [error,setError]=useState(''), [closed,setClosed]=useState(false);

  const load=async()=>{try{const [nextHealth,list]=await Promise.all([fetch('/api/health').then(r=>r.json()),fetch('/api/jobs').then(r=>r.json())]);setHealth(nextHealth);setJobs(list||[]);if(!selected&&list?.length)setSelected(list[0].id)}catch{if(!closed)setHealth(h=>({...h,mode:'offline'}))}};
  useEffect(()=>{load();const timer=setInterval(load,1500);return()=>clearInterval(timer)},[selected]);
  useEffect(()=>{fetch('/api/voices').then(r=>r.json()).then(setVoices).catch(()=>setVoices([]))},[]);
  useEffect(()=>{fetch('/api/settings').then(async r=>{if(!r.ok)throw new Error(await r.text());return r.json()}).then(settings=>{setRefAudio(settings.ref_audio);setRefText(settings.ref_text||'');setSpeakerOnly(Boolean(settings.speaker_only));setChunkSize(Number(settings.chunk_size)||1200);setTranslationChunkSize(Number(settings.translation_chunk_size)||4000);setAutoMerge(Boolean(settings.auto_merge));setSettingsLoaded(true)}).catch(e=>{setError(`Не удалось загрузить настройки: ${e.message}`);setSettingsLoaded(true)})},[]);
  useEffect(()=>{if(!settingsLoaded)return;const timer=setTimeout(async()=>{try{const res=await fetch('/api/settings',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({ref_audio:refAudio,ref_text:refText,speaker_only:speakerOnly,chunk_size:Number(chunkSize),translation_chunk_size:Number(translationChunkSize),auto_merge:autoMerge})});if(!res.ok)throw new Error(await res.text())}catch(e){setError(`Не удалось сохранить настройки: ${e.message}`)}},600);return()=>clearTimeout(timer)},[settingsLoaded,refAudio,refText,speakerOnly,chunkSize,translationChunkSize,autoMerge]);

  const active=jobs.find(j=>j.id===selected)||jobs[0];
  const selectedVoice=voices.find(v=>v.ref_audio===refAudio);
  const filteredVoices=voices.filter(v=>`${v.name} ${v.description}`.toLocaleLowerCase('ru-RU').includes(voiceSearch.toLocaleLowerCase('ru-RU')));
  const chars=[...text].length, ready=active?.chunks?.filter(c=>c.status==='ready').length||0;
  const elapsed=active?.chunks?.reduce((total,c)=>total+(c.synthesis_seconds||c.elapsed_seconds||0),0)||0;
  const average=ready?active.chunks.reduce((total,c)=>total+(c.status==='ready'?(c.synthesis_seconds||0):0),0)/ready:0;
  const remaining=Math.max(0,(active?.chunks?.length||0)-ready)*(average||0);
  const phase=active?.status==='ready'?3:['translating'].includes(active?.status)?1:active?2:0;

  const chooseVoice=voice=>{setRefAudio(voice.ref_audio);if(voice.transcript)setRefText(voice.transcript);setVoiceOpen(false);setVoiceSearch('')};
  const submit=async()=>{if(!text.trim()||(!speakerOnly&&!refText.trim()))return;setBusy(true);setError('');try{const res=await fetch('/api/jobs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text,voice:'clone',ref_audio:refAudio,ref_text:refText,speaker_only:speakerOnly,chunk_size:Number(chunkSize),translation_chunk_size:Number(translationChunkSize),auto_merge:autoMerge})});if(!res.ok)throw new Error(await res.text());const job=await res.json();setSelected(job.id);setText('');load()}catch(e){setError(e.message)}finally{setBusy(false)}};
  const merge=async id=>{const res=await fetch(`/api/jobs/${id}/merge`,{method:'POST'});if(!res.ok)setError(await res.text());load()};
  const clearReady=async()=>{await fetch('/api/jobs/ready',{method:'DELETE'});setSelected(null);load()};
  const removeJob=async id=>{if(!window.confirm('Удалить задание из очереди и базы данных? Аудиофайлы на диске останутся.'))return;const res=await fetch(`/api/jobs/${id}`,{method:'DELETE'});if(!res.ok){setError(await res.text());return}if(selected===id)setSelected(null);load()};
  const controlJob=async(job,action)=>{const res=await fetch(`/api/jobs/${job.id}/${action}`,{method:'POST'});if(!res.ok){setError(await res.text());return}load()};
  const closeStudio=async()=>{if(!window.confirm('Выгрузить модель из видеопамяти и закрыть TTS Студию?'))return;setClosed(true);try{await fetch('/api/shutdown',{method:'POST'})}catch{}};
  const modelLabel=health.mode==='offline'?'Сервер недоступен':active?.status==='translating'?'Gemma переводит · 16K':active?.status==='unloading_translation'?'Gemma выгружается':active?.status==='loading_tts'?'Загружается Qwen3‑TTS':health.model_loaded?'Qwen3‑TTS запущена':'Модели выгружены';

  if(closed)return <div className="closed-screen"><Power/><h1>TTS Студия закрыта</h1><p>Модели выгружены из видеопамяти.</p></div>;

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><div className="brand-icon"><Waveform weight="bold"/></div><div><strong>TTS Студия</strong><span>Gemma 4 → Qwen3‑TTS</span></div></div>
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
          {job.status==='failed'&&job.translation_status!=='ready'&&<button className="queue-pause resume" onClick={()=>controlJob(job,'retry')} title="Повторить незавершённый чанк"><ArrowClockwise/></button>}
          <button className="queue-remove" onClick={()=>removeJob(job.id)} title="Удалить"><Trash/></button>
        </div>})}</div>
      </aside>

      <section className="active-workspace">
        {!active?<div className="active-empty"><Headphones/><strong>Нет активного задания</strong><span>Создайте его в панели справа</span></div>:<>
          <div className="active-header"><div><span>АКТИВНОЕ ЗАДАНИЕ</span><h1>{active.title}</h1></div>{active.status==='failed'&&active.translation_status!=='ready'?<button className="job-action resume" onClick={()=>controlJob(active,'retry')}><ArrowClockwise/>Повторить {Math.min(active.translation_chunk+1,active.translation_chunks||1)}-й чанк</button>:active.status==='paused'?<button className="job-action resume" onClick={()=>controlJob(active,'resume')}><Play/>Продолжить</button>:!['ready','failed','pausing'].includes(active.status)&&<button className="job-action" onClick={()=>controlJob(active,'pause')}><Pause/>Пауза</button>}</div>
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
          {active.chunks.length===0?(active.status==='failed'?<div className="failure-state"><WarningCircle/><strong>Перевод остановлен</strong><span>{active.error_message||'Не удалось завершить переводческий чанк'}</span><small>Готово {active.translation_chunk} из {active.translation_chunks||1} чанков</small><button onClick={()=>controlJob(active,'retry')}><ArrowClockwise/>Повторить {Math.min(active.translation_chunk+1,active.translation_chunks||1)}-й чанк</button></div>:<div className="phase-wait"><SpinnerGap className={workingStatuses.includes(active.status)?'spin':''}/><strong>{statusText[active.status]}</strong><span>{active.status==='translating'?`Чанк ${Math.min(active.translation_chunk+1,active.translation_chunks||1)} из ${active.translation_chunks||1} · ≈ ${active.translation_progress}%${active.translation_attempt>1?` · попытка ${active.translation_attempt}/3`:''}`:active.status==='paused'?'Незавершённый чанк начнётся с начала':'Подготовка модели'}</span></div>):<div className="chunk-table"><div className="chunk-table-head"><span>#</span><span>ФРАГМЕНТ</span><span>СТАТУС</span><span>АУДИО</span><span>ВРЕМЯ</span><span></span></div><div className="chunk-scroll">{active.chunks.map((chunk,index)=><div className={`chunk-row ${chunk.status==='running'?'active':''}`} key={chunk.id}>
            <span>{index+1}</span><span className="chunk-name"><strong>{chunk.start}–{chunk.end}</strong><small>{chunk.characters} знаков</small></span><span className="chunk-state"><Status value={chunk.status}/>{chunk.status==='running'&&<small>≈ {chunk.progress||5}%</small>}</span>
            <span className="chunk-audio">{chunk.status==='ready'?<><audio controls preload="none" src={chunk.audio_url}/><a href={chunk.audio_url} download title="Скачать"><DownloadSimple/></a></>:<i><em style={{width:chunk.status==='running'?`${chunk.progress||5}%`:'0%'}}/></i>}</span>
            <span className="chunk-time">{chunk.status==='running'?<><strong>{runtime(chunk.elapsed_seconds)}</strong><small>прошло</small></>:chunk.status==='ready'?<><strong>{chunk.synthesis_seconds>0?runtime(chunk.synthesis_seconds):'—'}</strong><small>{chunk.duration?`${chunk.duration.toFixed(1)} с аудио`:'готово'}</small></>:<strong>—</strong>}</span><span></span>
          </div>)}</div></div>}
          <div className="active-footer"><span>Общее время <strong>{runtime(elapsed)}</strong></span><span>Озвучено <strong>{ready}/{active.chunks.length||0}</strong></span><span>Готово через <strong>{remaining?`≈ ${runtime(remaining)}`:'—'}</strong></span></div>
        </>}
      </section>

      <aside className="create-panel">
        <div className="create-heading">СОЗДАТЬ ЗАДАНИЕ</div>
        <div className="source-editor"><textarea value={text} onChange={e=>setText(e.target.value)} placeholder="Вставьте английский текст…"/><span>{chars.toLocaleString('ru-RU')} знаков</span></div>
        <div className="voice-title"><span>ВЫБРАННЫЙ ГОЛОС</span><button onClick={()=>setVoiceOpen(true)}><Headphones/>Библиотека голосов</button></div>
        <div className="voice-preview"><SpeakerHigh/><strong>{selectedVoice?.name||'Загрузка…'}</strong>{selectedVoice&&<audio controls preload="metadata" src={selectedVoice.audio_url}/>}</div>
        <button className={`transcript-toggle ${transcriptOpen?'open':''}`} onClick={()=>setTranscriptOpen(!transcriptOpen)}><span>ТРАНСКРИПТ</span><b>{transcriptOpen?'−':'+'}</b></button>
        {transcriptOpen&&<input className="transcript-input" value={refText} onChange={e=>setRefText(e.target.value)} disabled={speakerOnly} placeholder={speakerOnly?'Не нужен в режиме «только тембр»':'Точный текст образца'}/>} 
        <div className="settings-title">НАСТРОЙКИ</div>
        <label className="setting-row"><span>Чанк перевода</span><select value={translationChunkSize} onChange={e=>setTranslationChunkSize(e.target.value)}><option value="2500">2 500 знаков</option><option value="4000">4 000 знаков</option><option value="6000">6 000 знаков</option></select></label>
        <label className="setting-row"><span>Размер фрагмента</span><select value={chunkSize} onChange={e=>setChunkSize(e.target.value)}><option value="600">600 знаков</option><option value="1200">1 200 знаков</option><option value="2400">2 400 знаков</option></select></label>
        <div className="setting-row"><span>Автосклейка</span><div><Toggle checked={autoMerge} onChange={()=>setAutoMerge(!autoMerge)} label="Автосклейка"/><small>{autoMerge?'Включена':'Выключена'}</small></div></div>
        <div className="setting-row"><span>Только тембр</span><div><Toggle checked={speakerOnly} onChange={()=>setSpeakerOnly(!speakerOnly)} label="Только тембр"/><small>{speakerOnly?'Включён':'Выключен'}</small></div></div>
        <button className="add-job" disabled={!text.trim()||busy||health.mode==='offline'||(!speakerOnly&&!refText.trim())} onClick={submit}><Plus weight="bold"/>{busy?'Добавляем…':'Добавить в очередь'}</button>
      </aside>
    </main>

    {voiceOpen&&<div className="voice-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)setVoiceOpen(false)}}><section className="voice-dialog"><header><div><strong>Библиотека голосов</strong><span>{filteredVoices.length} из {voices.length}</span></div><label><MagnifyingGlass/><input autoFocus value={voiceSearch} onChange={e=>setVoiceSearch(e.target.value)} placeholder="Найти голос…"/></label><button onClick={()=>setVoiceOpen(false)}><X/></button></header><div className="voice-list">{filteredVoices.length===0?<div className="voice-empty">Ничего не найдено</div>:filteredVoices.map(voice=><article className={voice.ref_audio===refAudio?'selected':''} key={voice.id}><SpeakerHigh/><div><strong>{voice.name}</strong><small>{voice.description} · {voiceDuration(voice.duration)}</small></div><audio controls preload="none" src={voice.audio_url}/><button onClick={()=>chooseVoice(voice)}>{voice.ref_audio===refAudio?<><Check/>Выбран</>:<>Выбрать</>}</button></article>)}</div></section></div>}
    {error&&<button className="error-toast" onClick={()=>setError('')}><WarningCircle/><span>{error}</span><X/></button>}
  </div>;
}

export { App };
