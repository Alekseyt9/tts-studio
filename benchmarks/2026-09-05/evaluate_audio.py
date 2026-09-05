import json, re, sys, time
from pathlib import Path
from faster_whisper import WhisperModel
import jiwer

ROOT=Path(__file__).resolve().parent;PROJECT=ROOT.parent.parent
model=WhisperModel(str(PROJECT/'models/faster-whisper-small'),device='cpu',compute_type='int8',cpu_threads=4,num_workers=1)
def transcribe(path):
    segments,info=model.transcribe(str(path),language='ru',beam_size=5,vad_filter=False,condition_on_previous_text=False)
    parts=[dict(start=s.start,end=s.end,text=s.text,avg_logprob=s.avg_logprob,no_speech_prob=s.no_speech_prob) for s in segments]
    return ' '.join(p['text'].strip() for p in parts),parts
def normalize(text):
    return ' '.join(re.findall(r'[а-яa-z0-9]+',text.lower().replace('ё','е')))
if len(sys.argv)>1 and sys.argv[1]=='reference':
    settings=json.loads((ROOT/'voice-settings.json').read_text(encoding='utf-8'))
    ref=Path(settings['ref_audio']);ref=ref if ref.is_absolute() else PROJECT/ref
    text,parts=transcribe(ref)
    (ROOT/'reference-transcript.txt').write_text(text,encoding='utf-8')
    (ROOT/'reference-asr.json').write_text(json.dumps(parts,ensure_ascii=False,indent=2),encoding='utf-8')
    print(text,flush=True)
else:
    results=[]
    for folder in sorted((ROOT/'tts-matched').iterdir()):
        for path in sorted(folder.glob('[0-9][0-9].wav')):
            text,parts=transcribe(path)
            target=path.with_suffix('.txt').read_text(encoding='utf-8')
            row=dict(engine=folder.name,file=path.name,transcript=text,reference=target,wer=jiwer.wer(normalize(target),normalize(text)),cer=jiwer.cer(normalize(target),normalize(text)),segments=parts)
            path.with_suffix('.asr.json').write_text(json.dumps(row,ensure_ascii=False,indent=2),encoding='utf-8')
            results.append(row);print(folder.name,row['wer'],row['cer'],text,flush=True)
    (ROOT/'audio-quality.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
