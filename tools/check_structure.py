#!/usr/bin/env python3
"""Check document roles, local links and Issue shape; never approve design or gates."""
import argparse
import json
from pathlib import Path
import re
import sys
import unicodedata
from urllib.parse import unquote

HEADINGS = ['要解决的问题', '当前方案', '依据与取舍', '待确认项', '验证与完成条件', '关联']
ROLES = {'index', 'concept', 'specification', 'design', 'analysis', 'record', 'policy', 'compatibility'}
RELATIONS = {'based-on', 'specifies', 'verifies', 'depends-on', 'tracks', 'supersedes'}
PREFIX = 'https://github.com/wyw-ai/loom-oi/blob/alpha/'


def prose(text):
    return re.sub(r'^(`{3,}|~{3,})[^\n]*\n.*?^\1\s*$', '', text, flags=re.M | re.S)


def anchors(text):
    result = set(re.findall(r'<a\s+id=["\']([^"\']+)', text))
    seen = {}
    for h in re.findall(r'^#{1,6} (.+)$', prose(text), re.M):
        h = re.sub(r'[`*_]', '', h).lower()
        slug = ''.join(c for c in h if c in '-_ ' or unicodedata.category(c)[0] in 'LN').replace(' ', '-')
        n = seen.get(slug, 0)
        seen[slug] = n + 1
        result.add(slug + (f'-{n}' if n else ''))
    return result


def links(text):
    text = prose(text)
    # Inline and reference definitions; code fences are not rendered links.
    return re.findall(r'\]\(([^)\n]+)\)', text) + re.findall(r'^\s*\[[^\]]+\]:\s+(\S+)', text, re.M)


def local_target(root, source, url):
    url = url.strip().strip('<>')
    if url.startswith(PREFIX):
        url, base = url[len(PREFIX):], root
    elif re.match(r'^[a-zA-Z][\w+.-]*:', url):
        return None
    else:
        base = source.parent
    dest, _, anchor = unquote(url).partition('#')
    target = (base / dest).resolve() if dest else source.resolve()
    if not target.is_relative_to(root.resolve()):
        raise ValueError('link escapes repository: ' + url)
    return target, anchor


def catalog_markdown(data):
    rows = ['# 文档目录', '', '<!-- Generated from catalog.json by tools/check_structure.py; do not edit. -->', '',
            '按用途查文件；阅读路线见[文档导航](README.md)，维护规则见[结构规范](structure.md)。角色不是采用或实验状态。', '',
            '| 文件 | 角色 | 唯一用途 |', '| --- | --- | --- |']
    for item in sorted(data['documents'], key=lambda x: x['path']):
        path = item['path']
        target = path[5:] if path.startswith('docs/') else '../' + path
        rows.append(f"| [{path}]({target}) | {item['role']} | {item['purpose']} |")
    return '\n'.join(rows) + '\n'


def validate(root, data, issues=None, check_index=True):
    root = Path(root).resolve()
    errors = []
    docs = data.get('documents', [])
    if data.get('schema_version') != 1 or not isinstance(docs, list) or not docs:
        return ['Invalid or empty document catalog']
    entries, primary = {}, {}
    for x in docs:
        path = x.get('path', '')
        p = (root / path).resolve()
        if not path or not p.is_relative_to(root) or not p.is_file():
            errors.append('Missing/outside catalog path: ' + path)
        if path in entries:
            errors.append('Duplicate catalog path: ' + path)
        entries[path] = x
        if x.get('role') not in ROLES or not x.get('purpose', '').strip():
            errors.append('Missing/invalid role or purpose: ' + path)
        if x.get('primary_for'):
            key = x['primary_for']
            if key in primary:
                errors.append('Duplicate primary topic: ' + key)
            primary[key] = path
    actual = {str(p.relative_to(root)) for folder in ['docs', 'kb'] for p in (root / folder).rglob('*.md')}
    actual.discard('docs/catalog.md')
    if actual != set(entries):
        errors.append('Catalog coverage mismatch: ' + str(sorted(actual ^ set(entries))))
    graph = {p: [] for p in entries}
    for path, item in entries.items():
        for rel in item.get('relations', []):
            target = rel.get('target', '').split('#')[0]
            if rel.get('kind') not in RELATIONS or not rel.get('reason', '').strip() or target not in entries:
                errors.append('Invalid/unexplained relation: ' + path)
            elif rel['kind'] == 'depends-on':
                graph[path].append(target)
    visiting, visited = set(), set()
    def visit(path):
        if path in visiting:
            errors.append('Dependency cycle: ' + path)
            return
        if path in visited:
            return
        visiting.add(path)
        for target in graph.get(path, []):
            visit(target)
        visiting.remove(path)
        visited.add(path)
    for path in graph:
        visit(path)

    def check_url(url, source):
        try:
            pair = local_target(root, source, url)
            if pair is None:
                return
            target, anchor = pair
            if not target.exists():
                errors.append(f'{source.relative_to(root)}: missing {url}')
            elif anchor and (not target.is_file() or anchor not in anchors(target.read_text())):
                errors.append(f'{source.relative_to(root)}: missing anchor {url}')
        except (ValueError, UnicodeError, OSError) as exc:
            errors.append(f'{source.relative_to(root)}: {exc}')
    paths = [root / x for x in entries] + [root / 'README.md', root / 'AGENTS.md']
    paths += list((root / '.agents').rglob('*.md')) + list((root / '.github').rglob('*.md'))
    paths += [root / 'experiments/README.md', root / 'experiments/GATES.md']
    for p in paths:
        if p.is_file():
            for url in links(p.read_text()):
                check_url(url, p)
    for path, x in entries.items():
        for rel in x.get('relations', []):
            check_url(PREFIX + rel.get('target', ''), root / path)
    if check_index:
        p = root / 'docs/catalog.md'
        if not p.is_file() or p.read_text() != catalog_markdown(data):
            errors.append('Generated catalog is stale; run --write-index')
    if issues is not None:
        if not isinstance(issues, list):
            errors.append('Issue input must be a JSON array')
        else:
            for item in issues:
                body = item.get('body', '')
                number = item.get('issue_number', item.get('number', '?'))
                if re.findall(r'^## (.+)$', prose(body), re.M) != HEADINGS:
                    errors.append(f'Issue {number}: expected six current sections in order')
                for field in ['Type:', 'Topic:', 'Work:', 'Maturity:', 'Decision:', 'Priority:', 'I/U/N/E/C:']:
                    if field not in body:
                        errors.append(f'Issue {number}: missing {field}')
                for url in links(body):
                    check_url(url, root / 'README.md')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--issues', type=Path, help='Temporary GitHub snapshot/draft array; never persisted by this script')
    parser.add_argument('--write-index', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    try:
        data = json.loads((root / 'docs/catalog.json').read_text())
        issues = json.loads(args.issues.read_text()) if args.issues else None
        if args.write_index:
            (root / 'docs/catalog.md').write_text(catalog_markdown(data))
        errors = validate(root, data, issues)
    except (KeyError, TypeError, ValueError, OSError) as exc:
        errors = ['Invalid input: ' + str(exc)]
    if errors:
        print('\n'.join('FAIL: ' + x for x in errors))
        return 1
    print('PASS: structure/links/index and supplied Issue shape only; content, confirmation and product gates require separate review.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
