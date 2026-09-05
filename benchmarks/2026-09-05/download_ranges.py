"""Fallback for stalled large HF downloads; verify full LFS SHA256."""
import concurrent.futures, hashlib, json, os, sys, time, urllib.request, fnmatch
from pathlib import Path
repo, dest=sys.argv[1:3];dest=Path(dest);dest.mkdir(parents=True,exist_ok=True)
meta=json.load(urllib.request.urlopen('https://huggingface.co/api/models/'+repo+'?blobs=true',timeout=60))
files=[f for f in meta['siblings'] if not f['rfilename'].startswith('.') and not f['rfilename'].endswith('.md')]
if len(sys.argv)>3:files=[f for f in files if fnmatch.fnmatch(f['rfilename'],sys.argv[3])]
for entry in files:
    name=entry['rfilename'];size=entry.get('size',0);out=dest/name;out.parent.mkdir(parents=True,exist_ok=True)
    if out.exists() and out.stat().st_size==size:
        print('EXISTS',name,flush=True);continue
    url='https://huggingface.co/'+repo+'/resolve/'+meta['sha']+'/'+name
    if size<16*1024*1024:
        with urllib.request.urlopen(url,timeout=120) as r: out.write_bytes(r.read())
        print('SMALL',name,flush=True);continue
    partial=out.with_suffix(out.suffix+'.ranged');donefile=out.with_suffix(out.suffix+'.ranges.json')
    done=set(json.loads(donefile.read_text()) if donefile.exists() else [])
    if not partial.exists():
        with partial.open('wb') as f:f.truncate(size)
        done=set()
    block=8*1024*1024;n=(size+block-1)//block
    def fetch(i):
        start=i*block;end=min(size-1,start+block-1)
        for attempt in range(6):
            try:
                req=urllib.request.Request(url,headers={'Range':f'bytes={start}-{end}'})
                with urllib.request.urlopen(req,timeout=90) as r:
                    if r.status!=206 or not r.headers.get('Content-Range','').startswith(f'bytes {start}-{end}/'):
                        raise RuntimeError('Unexpected range response '+str(r.status)+' '+r.headers.get('Content-Range',''))
                    data=r.read()
                if len(data)!=end-start+1:raise RuntimeError('Short range')
                with partial.open('r+b') as f:f.seek(start);f.write(data)
                return i
            except Exception:
                if attempt==5:raise
                time.sleep(2+attempt*2)
    print('START',name,size,'remaining',n-len(done),flush=True);last=0
    with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
        for future in concurrent.futures.as_completed([pool.submit(fetch,i) for i in range(n) if i not in done]):
            done.add(future.result());donefile.write_text(json.dumps(sorted(done)))
            if time.monotonic()-last>20:print('PROGRESS',name,len(done),n,flush=True);last=time.monotonic()
    sha=hashlib.file_digest(partial.open('rb'),'sha256').hexdigest()
    if sha!=entry['lfs']['sha256']:raise RuntimeError('Checksum mismatch '+name)
    os.replace(partial,out);print('VERIFIED',name,sha,flush=True)
(dest/'download-provenance.json').write_text(json.dumps({'repo':repo,'revision':meta['sha'],'files':files},indent=2))
