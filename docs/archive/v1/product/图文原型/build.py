"""Rebuild the offline review data from page specifications and Markdown sources."""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent
PRODUCT = ROOT.parent
specs = json.loads((ROOT / 'pages.json').read_text())
paths = {str((PRODUCT / p['source']).resolve()): p['id'] for p in specs}

def lines(text):
    return [s.strip().removeprefix('- ') for s in text.strip().splitlines() if s.strip()]

pages = []
for spec in specs:
    source = PRODUCT / spec['source']
    raw = source.read_text()
    parts = re.split(r'^## (.+)\n', raw, flags=re.M)
    sections = dict(zip(parts[1::2], parts[2::2]))
    actions = []
    for line in sections['按钮与跳转'].splitlines():
        if not line.startswith('|'):
            continue
        label, description = [s.strip() for s in line.strip('|').split('|')][:2]
        if label.startswith('按钮') or re.fullmatch(r'[- :]+', label):
            continue
        targets = []
        for title, href in re.findall(r'\[([^]]+)\]\(([^)]+)\)', description):
            target = paths.get(str((source.parent / href).resolve()))
            if target:
                targets.append({'title': title, 'id': target})
        actions.append({'label': label, 'description': description, 'targets': targets})
    pages.append({**spec, 'title': raw.splitlines()[0].removeprefix('# '),
                  'intro': sections['用途与入口'].strip(),
                  'display': lines(sections['页面展示']), 'actions': actions,
                  'dialogs': lines(sections['弹窗与抽屉']),
                  'states': lines(sections['状态与边界'])})

assert len(pages) == 39, 'Expected all 39 page documents'
assert len({p['id'] for p in pages}) == len(pages)
page_ids = {p['id'] for p in pages}
normalize = lambda s: re.sub(r'[\s／/（）()]', '', s)
for p in pages:
    assert p['regions'] and p['actions'], p['id']
    for label in p['controls']:
        assert any(normalize(label) in normalize(a['label']) for a in p['actions']), (p['id'], label)
    for r in p['regions']:
        assert r.get('note'), (p['id'], r)
        if r['kind'] == 'rows':
            for row in r['items']:
                if len(row) > 3:
                    assert row[3] in page_ids or row[3].startswith('modal:'), (p['id'], row[3])

(ROOT / 'assets/pages.js').write_text('window.PAGES = ' + json.dumps(pages, ensure_ascii=False, indent=2) + ';\n')
print(f'Built {len(pages)} page wireframes with {sum(len(p["actions"]) for p in pages)} action descriptions.')
