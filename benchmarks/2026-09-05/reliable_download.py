"""Resumable public model downloads with per-process DNS cache and SHA-256 verification."""
import concurrent.futures, hashlib, json, os, socket, threading, time
from pathlib import Path
import requests

ROOT=Path(__file__).resolve().parent
original_getaddrinfo=socket.getaddrinfo
dns={};dns_lock=threading.Lock();local=threading.local()
def cached_getaddrinfo(host,port,*args,**kwargs):
    key=(host,port,args,tuple(sorted(kwargs.items())))
    with dns_lock:
        if key not in dns:dns[key]=original_getaddrinfo(host,port,*args,**kwargs)
        return dns[key]
socket.getaddrinfo=cached_getaddrinfo
def session():
    if not hasattr(local,'session'):local.session=requests.Session()
    return local.session
def get(url,**kwargs):
    for attempt in range(15):
        try:
            r=session().get(url,timeout=(30,90),**kwargs);r.raise_for_status();return r
        except Exception:
            if attempt==14:raise
            time.sleep(min(30,2+attempt*2))
def download(url,out,size,sha=None):
    out=Path(out);out.parent.mkdir(parents=True,exist_ok=True)
    if out.exists() and out.stat().st_size==size:
        if not sha or hashlib.file_digest(out.open('rb'),'sha256').hexdigest()==sha:
            print('EXISTS',out.name,flush=True);return
    if size<16*1024*1024:
        data=get(url).content
        if len(data)!=size:raise RuntimeError('Size mismatch '+str(out))
        if sha and hashlib.sha256(data).hexdigest()!=sha:raise RuntimeError('SHA mismatch '+str(out))
        out.write_bytes(data);return
    part=out.with_suffix(out.suffix+'.ranged');state=out.with_suffix(out.suffix+'.ranges.json')
    done=set(json.loads(state.read_text()) if state.exists() else [])
    if not part.exists():
        with part.open('wb') as f:f.truncate(size)
        done=set()
    # Resolve the CDN redirect once; keep TLS hostname verification intact.
    response=get(url,headers={'Range':'bytes=0-0'},stream=True)
    direct=response.url;response.close()
    block=8*1024*1024;n=(size+block-1)//block
    def fetch(i):
        start=i*block;end=min(size-1,start+block-1)
        for attempt in range(15):
            try:
                with get(direct,headers={'Range':f'bytes={start}-{end}'},stream=True) as r:
                    if r.status_code!=206 or r.headers.get('Content-Range')!=f'bytes {start}-{end}/{size}':
                        raise RuntimeError('Unexpected range '+r.headers.get('Content-Range',''))
                    length=0
                    with part.open('r+b') as f:
                        f.seek(start)
                        for data in r.iter_content(1024*1024):f.write(data);length+=len(data)
                    if length!=end-start+1:raise RuntimeError('Short body')
                return i
            except Exception as e:
                print('RETRY',out.name,i,attempt,type(e).__name__,flush=True)
                if attempt==14:raise
                time.sleep(2+attempt)
    print('START',out.name,len(done),n,flush=True);last=0
    with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
        for future in concurrent.futures.as_completed([pool.submit(fetch,i) for i in range(n) if i not in done]):
            done.add(future.result());state.write_text(json.dumps(sorted(done)))
            if time.monotonic()-last>20:print('PROGRESS',out.name,len(done),n,flush=True);last=time.monotonic()
    actual=hashlib.file_digest(part.open('rb'),'sha256').hexdigest()
    if sha and actual!=sha:raise RuntimeError('SHA mismatch '+str(out))
    os.replace(part,out);print('VERIFIED',out.name,actual,flush=True)
def hf(repo,dest,filename=None):
    dest=Path(dest)
    meta=get('https://huggingface.co/api/models/'+repo+'?blobs=true').json()
    files=[f for f in meta['siblings'] if (f['rfilename']==filename if filename else not f['rfilename'].endswith('.md'))]
    for f in files:
        download('https://huggingface.co/'+repo+'/resolve/'+meta['sha']+'/'+f['rfilename'],dest/f['rfilename'],f['size'],f.get('lfs',{}).get('sha256'))
    (dest/'download-provenance.json').write_text(json.dumps({'repo':repo,'revision':meta['sha'],'files':files},indent=2))
def ollama(tag):
    base='https://registry.ollama.ai/v2/library/translategemma/'
    manifest=get(base+'manifests/'+tag).json()
    for f in manifest['layers']+[manifest['config']]:
        download(base+'blobs/'+f['digest'],Path('F:/ollama/models/blobs')/f['digest'].replace(':','-'),f['size'],f['digest'].split(':')[1])
    target=Path('F:/ollama/models/manifests/registry.ollama.ai/library/translategemma')/tag
    target.parent.mkdir(parents=True,exist_ok=True);target.write_text(json.dumps(manifest))
    print('INSTALLED TranslateGemma',tag,flush=True)
if __name__=='__main__':
    import sys
    if sys.argv[1]=='ollama':ollama(sys.argv[2])
    else:hf(sys.argv[1],sys.argv[2],sys.argv[3] if len(sys.argv)>3 else None)
