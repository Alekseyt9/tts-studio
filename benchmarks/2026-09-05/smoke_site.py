import json,time,urllib.request
from pathlib import Path
root=Path(__file__).resolve().parent
base='http://127.0.0.1:8097/api/'
def api(path,payload=None,method=None):
    req=urllib.request.Request(base+path,data=json.dumps(payload).encode() if payload is not None else None,
        headers={'Content-Type':'application/json'},method=method)
    with urllib.request.urlopen(req,timeout=30) as r:return json.load(r)
deadline=time.monotonic()+1200
while not (root/'translations-matched/translategemma_12b/metrics.json').exists():
    if time.monotonic()>deadline:raise RuntimeError('Translation benchmark not finished')
    time.sleep(3)
settings=json.loads((root/'voice-settings.json').read_text(encoding='utf-8'))
settings['speaker_only']=bool(settings['speaker_only'])
settings['auto_merge']=bool(settings['auto_merge'])
settings.update(tts_model='faster',translation_model='gemma4_direct')
api('settings',settings,'PUT')
results=[]
for engine in ['omni32','omni16','faster','qwen']:
    payload=dict(settings,tts_model=engine,text='He was at work again. I rang the bell and was shown up to the room.',voice='clone')
    job=api('jobs',payload);print('START',engine,job['id'],flush=True)
    deadline=time.monotonic()+300
    while True:
        job=next(j for j in api('jobs') if j['id']==job['id'])
        if job['status'] in ['ready','failed','paused'] or job.get('error_message'):break
        if time.monotonic()>deadline:api('jobs/'+job['id']+'/pause',{},'POST');raise RuntimeError('Job timeout '+engine)
        time.sleep(2)
    results.append(job)
    (root/'site-smoke-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
    print('RESULT',engine,job['status'],job.get('error_message'),flush=True)
    if job['status']!='ready':
        api('jobs/'+job['id']+'/pause',{},'POST');raise RuntimeError('Smoke failed '+engine)
    assert job['tts_model']==engine and job['translation_model']=='gemma4_direct'
    assert job['chunks'] and all(c['duration']>0 and c['audio_url'] for c in job['chunks'])
print('ALL_SITE_SMOKES_PASSED',flush=True)
