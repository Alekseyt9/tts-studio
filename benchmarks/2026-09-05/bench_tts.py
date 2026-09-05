import argparse, hashlib, importlib.metadata, json, random, re, time, traceback
from pathlib import Path
import numpy as np
import soundfile as sf
import torch

ROOT=Path(__file__).resolve().parent;PROJECT=ROOT.parent.parent
parser=argparse.ArgumentParser();parser.add_argument('engine',choices=['qwen','faster','omni32','omni16']);args=parser.parse_args()
out=ROOT/'tts-matched'/args.engine;out.mkdir(parents=True,exist_ok=True)
settings=json.loads((ROOT/'voice-settings.json').read_text(encoding='utf-8'))
ref=Path(settings['ref_audio']);ref=ref if ref.is_absolute() else PROJECT/ref
canonical=ROOT/'tts-common-ru.txt'
if not canonical.exists():canonical.write_text((ROOT/'translations/gemma4_direct/translation-ru.txt').read_text(encoding='utf-8'),encoding='utf-8')
text=canonical.read_text(encoding='utf-8')
sentences=re.split(r'(?<=[.!?…])\s+|\n\n+',text)
chunks=[];current=''
for sentence in sentences:
    if current and len(current)+len(sentence)+1>1200:chunks.append(current);current=''
    current+=(' ' if current else '')+sentence
if current:chunks.append(current)
chunks=chunks[:1]
(ROOT/'tts-corpus-matched.json').write_text(json.dumps({'text_source':'translations/gemma4_direct/translation-ru.txt','chunk_size':1200,'chunks':chunks,'reference_audio':str(ref),'reference_sha256':hashlib.sha256(ref.read_bytes()).hexdigest()},ensure_ascii=False,indent=2),encoding='utf-8')
torch.set_num_threads(8)
torch.cuda.reset_peak_memory_stats()
started=time.perf_counter()
if args.engine=='qwen':
    from qwen_tts import Qwen3TTSModel
    model=Qwen3TTSModel.from_pretrained(str(PROJECT/'models/Qwen3-TTS-12Hz-1.7B-Base'),device_map='cuda:0',dtype=torch.bfloat16,attn_implementation='sdpa')
elif args.engine=='faster':
    from faster_qwen3_tts import FasterQwen3TTS
    model=FasterQwen3TTS.from_pretrained(str(PROJECT/'models/Qwen3-TTS-12Hz-1.7B-Base'),device='cuda',dtype=torch.bfloat16,attn_implementation='sdpa',max_seq_len=2048,local_files_only=True)
else:
    from omnivoice import OmniVoice
    model=OmniVoice.from_pretrained(str(PROJECT/'models/OmniVoice'),device_map='cuda:0',dtype=torch.bfloat16,attn_implementation='sdpa')
torch.cuda.synchronize();load_seconds=time.perf_counter()-started
def generate(text,seed):
    random.seed(seed);np.random.seed(seed);torch.manual_seed(seed);torch.cuda.manual_seed_all(seed)
    if args.engine=='qwen':
        return model.generate_voice_clone(text=text,language='Russian',ref_audio=str(ref),ref_text=settings['ref_text'],x_vector_only_mode=settings['speaker_only'])
    if args.engine=='faster':
        return model.generate_voice_clone(text=text,language='Russian',ref_audio=str(ref),ref_text=settings['ref_text'],xvec_only=settings['speaker_only'])
    transcript=(ROOT/'reference-transcript.txt').read_text(encoding='utf-8').strip()
    return model.generate(text=text,language='Russian',ref_audio=str(ref),ref_text=transcript,num_step=32 if args.engine=='omni32' else 16),24000
print('LOADED',args.engine,load_seconds,flush=True)
start=time.perf_counter();generate('Он снова был за работой. Я позвонил в дверь, и меня провели в комнату.',42);torch.cuda.synchronize();warmup_seconds=time.perf_counter()-start
print('WARMUP',args.engine,warmup_seconds,flush=True)
rows=[];combined=[];sr=24000
for i,chunk in enumerate(chunks,1):
    print('START',args.engine,i,len(chunks),len(chunk),flush=True)
    (out/f'{i:02d}.txt').write_text(chunk,encoding='utf-8')
    start=time.perf_counter();torch.cuda.reset_peak_memory_stats()
    try:
        wavs,sr=generate(chunk,20260905+i);torch.cuda.synchronize();seconds=time.perf_counter()-start
        audio=np.asarray(wavs[0],dtype=np.float32).reshape(-1);sf.write(str(out/f'{i:02d}.wav'),audio,sr)
        duration=len(audio)/sr
        row=dict(chunk=i,characters=len(chunk),seconds=seconds,audio_seconds=duration,rtf=seconds/duration,
            peak_vram_allocated_gb=torch.cuda.max_memory_allocated()/1024**3,peak_vram_reserved_gb=torch.cuda.max_memory_reserved()/1024**3,
            rms=float(np.sqrt(np.mean(audio**2))),peak=float(np.max(np.abs(audio))),clipped_fraction=float(np.mean(np.abs(audio)>=0.999)))
        combined.append(audio)
    except Exception as e:
        row=dict(chunk=i,error=str(e),seconds=time.perf_counter()-start);traceback.print_exc()
    rows.append(row);(out/'progress.json').write_text(json.dumps(rows,indent=2),encoding='utf-8');print('DONE',args.engine,row,flush=True)
if combined:sf.write(str(out/'full.wav'),np.concatenate(combined),sr)
versions={}
for p in ['torch','qwen-tts','qwen-tts-hf','faster-qwen3-tts','omnivoice','transformers']:
    try:versions[p]=importlib.metadata.version(p)
    except importlib.metadata.PackageNotFoundError:pass
metrics=dict(engine=args.engine,reference_audio=str(ref),speaker_only=bool(settings['speaker_only']) if args.engine in ['qwen','faster'] else False,
    seed=20260905,load_seconds=load_seconds,warmup_seconds=warmup_seconds,versions=versions,chunks=rows,
    total_seconds=sum(r['seconds'] for r in rows),audio_seconds=sum(r.get('audio_seconds',0) for r in rows))
(out/'metrics.json').write_text(json.dumps(metrics,ensure_ascii=False,indent=2),encoding='utf-8')
print('COMPLETE',args.engine,metrics['total_seconds'],metrics['audio_seconds'],flush=True)
