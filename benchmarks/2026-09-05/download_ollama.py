import json, time, urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parent
models=['hf.co/tencent/Hy-MT2-7B-GGUF:Q6_K','hf.co/tencent/Hy-MT2-1.8B-GGUF:Q8_0','translategemma:4b','translategemma:12b']
for model in models:
    print('PULL',model,flush=True)
    req=urllib.request.Request('http://127.0.0.1:11435/api/pull',data=json.dumps({'model':model,'stream':True}).encode(),headers={'Content-Type':'application/json'})
    last=0
    try:
        with urllib.request.urlopen(req,timeout=7200) as r:
            for line in r:
                d=json.loads(line)
                if 'error' in d: raise RuntimeError(d['error'])
                if time.monotonic()-last>30 or d.get('status')=='success':
                    print(model,d,flush=True);last=time.monotonic()
        (ROOT / (model.split('/')[-1].replace(':','_')+'.installed')).write_text(model)
    except Exception as e: print('FAILED',model,str(e),flush=True)
