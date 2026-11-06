#!/usr/bin/env python3
"""Read-only, repository-local evolution records. Suggestions never approve decisions."""
import argparse
from datetime import date
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import unicodedata

RECORDS = 'kb/evolution/records'
ID = re.compile(r'[A-Z][A-Z0-9_-]{1,79}\Z')
KINDS = {'question', 'attempt', 'claim', 'evidence', 'decision', 'lesson'}
BASES = {'source', 'inference', 'proposal', 'measured', 'human_decision'}
OUTCOMES = {'proposed', 'supported', 'refuted', 'deferred', 'unresolved', 'accepted', 'rejected'}
RELATIONS = {'based_on', 'depends_on', 'supports', 'contradicts', 'supersedes', 'related'}
LABELS = {'based_on': '来源', 'depends_on': '前提', 'supports': '支持',
          'contradicts': '反驳或冲突', 'supersedes': '替代范围', 'related': '相关'}
TRACE = {'based_on', 'depends_on', 'supports', 'supersedes'}
IMPACT = {'depends_on', 'supports'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, '重复JSON字段: ' + key)
        result[key] = value
    return result


def read_json(path):
    raw = Path(path).read_bytes()
    require(len(raw) <= 2_000_000, '单个记录超过2MB: ' + str(path))
    return parse_json(raw)


def parse_json(raw):
    return json.loads(raw, object_pairs_hook=unique_pairs,
                      parse_constant=lambda x: require(False, '非法JSON数值: ' + x))


def fields(value, names):
    require(isinstance(value, dict) and set(value) == set(names.split()),
            '字段必须为: ' + names)


def string(value, label):
    require(isinstance(value, str) and bool(value.strip()), '缺少文字: ' + label)


def strings(value, label, empty=False):
    require(isinstance(value, list) and (empty or bool(value)), '需要列表: ' + label)
    for item in value:
        string(item, label)
    require(len(value) == len(set(value)), '重复列表项: ' + label)


def inside(root, relative):
    require(isinstance(relative, str) and not Path(relative).is_absolute(), '必须为仓库相对路径')
    path = (root / relative).resolve()
    require(path.is_relative_to(root), '路径越界: ' + relative)
    return path


def conditions(value):
    require(isinstance(value, dict), 'conditions必须为对象')
    for key, item in value.items():
        string(key, 'condition key')
        require(item is None or isinstance(item, str) and bool(item.strip()), '条件须为非空文字或null')


def normalize(text):
    return unicodedata.normalize('NFKC', text).casefold().strip()


def terms(text):
    result = set()
    for word in re.findall(r'[a-z0-9_-]+|[\u3400-\u9fff]+', normalize(text)):
        result.add(word)
        if re.search(r'[\u3400-\u9fff]', word):
            result.update(word[i:i + 2] for i in range(len(word) - 1))
    return result


class Store:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.sources, self.nodes, self.edges, self.withdrawals = {}, {}, {}, {}
        self.warnings, self.files = [], []
        ids, stamps = set(), []
        directory = inside(self.root, RECORDS)
        paths = sorted(directory.glob('*.json'))
        require(bool(paths), '没有演进记录: ' + RECORDS)
        for path in paths:
            require(path.resolve().is_relative_to(self.root), '记录文件路径越界')
            raw = path.read_bytes()
            require(len(raw) <= 2_000_000, '单个记录超过2MB')
            data = parse_json(raw)
            fields(data, 'schema_version stage recorded_on recorder coverage sources nodes edges withdrawals')
            require(type(data['schema_version']) is int and data['schema_version'] == 1, '不支持的schema_version')
            self._id(data['stage'], ids)
            require(path.stem == data['stage'], '文件名必须等于stage')
            require(isinstance(data['recorded_on'], str) and re.fullmatch(r'\d{4}-\d{2}-\d{2}', data['recorded_on']), '日期须为YYYY-MM-DD')
            date.fromisoformat(data['recorded_on'])
            string(data['recorder'], 'recorder')
            string(data['coverage'], 'coverage')
            self.warnings.append(data['coverage'])
            for name, target in [('sources', self.sources), ('nodes', self.nodes),
                                 ('edges', self.edges), ('withdrawals', self.withdrawals)]:
                require(isinstance(data[name], list), name + '必须为列表')
                for item in data[name]:
                    require(isinstance(item, dict), '记录必须为对象')
                    self._id(item.get('id'), ids)
                    target[item['id']] = item
            relative = path.relative_to(self.root).as_posix()
            self.files.append(relative)
            stamps.append([relative, sha(raw)])
        for item in self.sources.values():
            self._source(item)
        for node in self.nodes.values():
            self._node(node)
        for edge in self.edges.values():
            fields(edge, 'id from to relation scope reason sources review')
            require(edge['from'] in self.nodes and edge['to'] in self.nodes, '关系引用不存在的节点')
            require(edge['relation'] in RELATIONS, '未知关系类型')
            require(edge['from'] != edge['to'], '关系不能指向自身')
            self._reference(edge)
            string(edge['scope'], 'edge scope')
            string(edge['reason'], 'edge reason')
        for withdrawal in self.withdrawals.values():
            fields(withdrawal, 'id edge reason sources review')
            require(withdrawal['edge'] in self.edges, '撤回引用不存在的关系')
            string(withdrawal['reason'], 'withdrawal reason')
            self._reference(withdrawal)
        revoked = {w['edge'] for w in self.withdrawals.values() if w['review'] == 'verified'}
        self.active = [e for e in self.edges.values() if e['id'] not in revoked and e['review'] == 'verified']
        for kind in ['depends_on', 'supersedes']:
            self._acyclic(kind)
        self.digest = sha(json.dumps(stamps, ensure_ascii=False, separators=(',', ':')).encode())

    @staticmethod
    def _id(value, seen):
        require(isinstance(value, str) and ID.fullmatch(value), '非法ID: ' + str(value))
        require(value not in seen, '重复ID: ' + value)
        seen.add(value)

    def _source(self, item):
        fields(item, 'id url snapshot sha256 group kind')
        require(isinstance(item['url'], str) and re.match(r'^(https://|chat-request:)', item['url']), '来源URL须为https或chat-request')
        string(item['group'], 'source group')
        require(item['kind'] in {'discussion', 'repository', 'experiment', 'human'}, '未知来源类型')
        if item['snapshot'] is None:
            require(item['sha256'] is None, '无快照不可填伪哈希')
            self.warnings.append(item['id'] + ': 仅来源定位，未校验远端正文')
        else:
            path = inside(self.root, item['snapshot'])
            require(isinstance(item['sha256'], str) and re.fullmatch(r'[0-9a-f]{64}', item['sha256']), '非法来源哈希')
            require(path.is_file() and sha(path.read_bytes()) == item['sha256'], '来源缺失或哈希改变: ' + item['id'])

    def _reference(self, item):
        strings(item['sources'], 'sources')
        require(all(s in self.sources for s in item['sources']), '不存在的来源ID')
        require(item['review'] in {'pending', 'verified'}, '未知review状态')

    def _node(self, item):
        fields(item, 'id kind issue title text sources basis review mechanism aliases conditions outcome reason revisit')
        require(item['kind'] in KINDS and item['basis'] in BASES and item['outcome'] in OUTCOMES, '未知节点分类/依据/结果')
        require(type(item['issue']) is int and item['issue'] > 0, '非法Issue编号')
        for key in ['title', 'text', 'reason', 'revisit']:
            string(item[key], key)
        require(item['mechanism'] is None or isinstance(item['mechanism'], str) and bool(item['mechanism'].strip()), '非法机制标识')
        strings(item['aliases'], 'aliases', empty=True)
        conditions(item['conditions'])
        self._reference(item)
        if item['kind'] == 'decision' or item['outcome'] in {'accepted', 'rejected'}:
            require(item['basis'] == 'human_decision' and any(self.sources[s]['kind'] == 'human' for s in item['sources']), '决定必须关联人类原确认来源')
        if item['basis'] == 'measured':
            require(any(self.sources[s]['kind'] == 'experiment' for s in item['sources']), '实测必须关联运行证据')

    def _acyclic(self, relation):
        graph = {n: [] for n in self.nodes}
        for edge in self.active:
            if edge['relation'] == relation:
                graph[edge['from']].append(edge['to'])
        visiting, done = set(), set()

        def walk(node):
            require(node not in visiting, relation + '关系存在循环')
            if node in done:
                return
            visiting.add(node)
            for child in graph[node]:
                walk(child)
            visiting.remove(node)
            done.add(node)
        for node in graph:
            walk(node)

    def get(self, ident):
        require(ident in self.nodes, '不存在的节点: ' + ident)
        return self.nodes[ident]

    def envelope(self, result):
        return {'input_sha256': self.digest, 'records': self.files, 'coverage_gaps': self.warnings,
                'decision_authority': '仅辅助复核，不批准、否决或修改Issue', 'result': result}

    def sources_for(self, node):
        return [self.sources[s] for s in node['sources']]

    def search(self, query, issue=None, limit=10):
        string(query, 'query')
        require(1 <= limit <= 100, 'limit须为1至100')
        q = normalize(query)
        qt = terms(query)
        found = []
        for node in self.nodes.values():
            if issue is not None and node['issue'] != issue:
                continue
            mechanism = normalize(node['mechanism'] or '')
            alias = q in {normalize(a) for a in node['aliases']}
            text = normalize(node['title'] + ' ' + node['text'] + ' ' + ' '.join(node['aliases']))
            overlap = sorted(qt & terms(text))
            score = 100 if q == mechanism else 80 if alias else 10 if q in text else len(overlap)
            if not score:
                continue
            found.append({'id': node['id'], 'title': node['title'], 'mechanism': node['mechanism'],
                          'match': '机制标识' if q == mechanism else '登记别名' if alias else '文本候选',
                          'matched_terms': overlap, 'rank_score': score,
                          'conditions': node['conditions'], 'outcome': node['outcome'],
                          'sources': self.sources_for(node), 'basis': node['basis'], 'review': node['review']})
        found.sort(key=lambda n: (-n['rank_score'], n['id']))
        return {'matches': found[:limit], 'total': len(found), 'truncated': len(found) > limit,
                'note': '分数仅用于排序；未知同义词可能漏检，零结果不证明新颖。'}

    def walk(self, ident, mode='trace', depth=8):
        self.get(ident)
        require(1 <= depth <= 50, 'depth须为1至50')
        require(mode in {'trace', 'impact'}, '未知遍历方式')
        allowed = TRACE if mode == 'trace' else IMPACT
        visited, queue, paths, truncated = {ident}, [(ident, [], 0)], [], False
        while queue:
            node, path, level = queue.pop(0)
            for edge in sorted(self.active, key=lambda x: x['id']):
                start, end = (edge['to'], edge['from']) if mode == 'trace' else (edge['from'], edge['to'])
                if start != node or edge['relation'] not in allowed or end in visited:
                    continue
                if level >= depth:
                    truncated = True
                    continue
                visited.add(end)
                chain = path + [edge['id']]
                paths.append({'node': end, 'title': self.nodes[end]['title'], 'edges': chain,
                              'effect': '需要复核' if mode == 'impact' else '来源或前序'})
                queue.append((end, chain, level + 1))
        return {'start': ident, 'paths': paths, 'truncated': truncated,
                'relation_policy': sorted(allowed), 'note': '每节点展示首条可达路径；未核对/已撤回关系不参与推导。'}

    def concerns(self, ident):
        trace = self.walk(ident, 'trace', 50)
        related = {ident} | {p['node'] for p in trace['paths']}
        conflicts = [e['id'] for e in self.active if e['relation'] == 'contradicts' and
                     (e['from'] in related or e['to'] in related)]
        replacements = [e['id'] for e in self.active if e['relation'] == 'supersedes' and e['from'] == ident]
        unverified = sorted(n for n in related if self.nodes[n]['review'] != 'verified' or
                            any(s['snapshot'] is None for s in self.sources_for(self.nodes[n])))
        return {'conflicts': conflicts, 'replacements': replacements, 'unverified_context': unverified,
                'trace_truncated': trace['truncated']}

    def compare(self, ident, current):
        old = self.get(ident)
        fields(current, 'mechanism conditions purpose')
        require(isinstance(current['mechanism'], str) and current['mechanism'].strip(), '需明确当前机制标识')
        conditions(current['conditions'])
        require(current['purpose'] in {'evaluate', 'replicate'}, 'purpose须为evaluate或replicate')
        same, changed, unknown = [], [], []
        for key in sorted(set(old['conditions']) | set(current['conditions'])):
            before, after = old['conditions'].get(key), current['conditions'].get(key)
            if after is None:
                unknown.append({'condition': key, 'old': before, 'current': after})
            elif before is None:
                changed.append({'condition': key, 'old': before, 'current': after, 'kind': 'newly_specified'})
            elif before == after:
                same.append(key)
            else:
                changed.append({'condition': key, 'old': before, 'current': after})
        concerns = self.concerns(ident)
        grounded = all(s['snapshot'] is not None for s in self.sources_for(old))
        if old['mechanism'] is None or normalize(old['mechanism']) != normalize(current['mechanism']):
            action = 'different_mechanism'
        elif old['review'] != 'verified' or not grounded or any(concerns.values()):
            action = 'review_evidence'
        elif old['basis'] == 'human_decision':
            action = 'human_review'
        elif unknown or not old['conditions']:
            action = 'insufficient_context'
        elif current['purpose'] == 'replicate':
            action = 'replicate_with_plan'
        elif changed:
            action = 'reevaluate'
        elif old['outcome'] in {'proposed', 'unresolved', 'deferred'}:
            action = 'continue_evidence'
        else:
            action = 'reuse_prior_reasoning'
        return {'old': ident, 'suggestion': action, 'same': same, 'changed': changed, 'unknown': unknown,
                **concerns, 'old_basis': old['basis'], 'old_outcome': old['outcome'],
                'old_reason': old['reason'], 'revisit': old['revisit'], 'sources': self.sources_for(old),
                'note': '机制与条件由输入者声明；旧未知而现明确标newly_specified，不等于证明当时取值不同。未证明语义完整或真实性；任何提示均不自动阻止探索。'}

    def graph(self, issue):
        chosen = {k: v for k, v in self.nodes.items() if v['issue'] == issue}
        require(bool(chosen), '该Issue没有已记录节点')
        names = {key: 'n' + str(i) for i, key in enumerate(sorted(chosen))}
        lines = ['flowchart TD', '  %% 输入指纹 ' + self.digest, '  %% 由当前记录生成；节点与连线不证明事实或采用']
        for key in sorted(chosen):
            label = re.sub(r'["`<>\[\]\\\r\n]', ' ', chosen[key]['title'])[:70]
            lines.append(f'  {names[key]}["{label}"]')
        for edge in sorted(self.active, key=lambda x: x['id']):
            if edge['from'] in names and edge['to'] in names:
                lines.append(f'  {names[edge["from"]]} -->|{LABELS[edge["relation"]]}| {names[edge["to"]]}')
        return '\n'.join(lines) + '\n'


def check_append_only(root, base):
    root = Path(root).resolve()
    try:
        revision = subprocess.check_output(['git', '-C', str(root), 'rev-parse', '--verify', '--end-of-options', base + '^{commit}'], stderr=subprocess.PIPE).decode().strip()
        paths = subprocess.check_output(['git', '-C', str(root), 'ls-tree', '-r', '-z', '--name-only', revision, '--', RECORDS]).decode().split('\0')
        checked = 0
        for name in filter(None, paths):
            original = subprocess.check_output(['git', '-C', str(root), 'show', revision + ':' + name], stderr=subprocess.PIPE)
            path = inside(root, name)
            require(path.is_file() and path.read_bytes() == original, '历史记录被修改或删除: ' + name)
            checked += 1
        return {'base': revision, 'preserved_files': checked, 'note': '只检查指定Git基线已有文件；不是平台不可篡改保证。'}
    except subprocess.CalledProcessError as exc:
        raise ValueError('无法读取Git基线') from exc


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    sub = parser.add_subparsers(dest='command', required=True)
    p = sub.add_parser('validate'); p.add_argument('--base')
    p = sub.add_parser('search'); p.add_argument('query'); p.add_argument('--issue', type=int); p.add_argument('--limit', type=int, default=10)
    p = sub.add_parser('compare'); p.add_argument('id'); p.add_argument('current', type=Path)
    for command in ['trace', 'impact']:
        p = sub.add_parser(command); p.add_argument('id'); p.add_argument('--depth', type=int, default=8)
    p = sub.add_parser('graph'); p.add_argument('--issue', required=True, type=int)
    args = parser.parse_args(argv)
    try:
        store = Store(args.root)
        if args.command == 'validate':
            result = {'nodes': len(store.nodes), 'edges': len(store.edges),
                      'active_edges': len(store.active), 'source_groups': len({s['group'] for s in store.sources.values()})}
            if args.base:
                result['history'] = check_append_only(args.root, args.base)
        elif args.command == 'search':
            result = store.search(args.query, args.issue, args.limit)
        elif args.command == 'compare':
            result = store.compare(args.id, read_json(args.current))
        elif args.command in {'trace', 'impact'}:
            result = store.walk(args.id, args.command, args.depth)
        else:
            print(store.graph(args.issue), end='')
            return 0
        print(json.dumps(store.envelope(result), ensure_ascii=False, indent=2))
        return 0
    except (ValueError, OSError, KeyError, TypeError, RecursionError) as exc:
        print(json.dumps({'error': str(exc)}, ensure_ascii=False), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
