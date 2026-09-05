import hashlib, json, re, urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent
url = 'https://www.gutenberg.org/cache/epub/1661/pg1661.txt'
raw = urllib.request.urlopen(url, timeout=60).read()
(ROOT / 'source-full.txt').write_bytes(raw)
book = raw.decode('utf-8-sig').replace('\r\n', '\n')
start = book.index('To Sherlock Holmes she is always')
paras = re.split(r'\n\s*\n', book[start:])
selected = []
for para in paras:
    selected.append(re.sub(r'\s+', ' ', para).strip())
    if len(' '.join(selected).split()) >= 1200:
        break
text = '\n\n'.join(selected)
(ROOT / 'source-en.txt').write_text(text, encoding='utf-8')
chunks, current = [], ''
for para in selected:
    if current and len(current) + len(para) + 2 > 4000:
        chunks.append(current)
        current = ''
    current += ('\n\n' if current else '') + para
if current:
    chunks.append(current)
manifest = dict(title='A Scandal in Bohemia', author='Arthur Conan Doyle',
    source_url=url, book_url='https://www.gutenberg.org/ebooks/1661',
    source_sha256=hashlib.sha256(raw).hexdigest(), words=len(text.split()),
    characters=len(text), approximate_pages=len(text.split())/300,
    translation_chunks=chunks)
(ROOT / 'corpus.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding='utf-8')
print({k:v for k,v in manifest.items() if k!='translation_chunks'})
print('chunk lengths', [len(c) for c in chunks])
