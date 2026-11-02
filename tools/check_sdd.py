#!/usr/bin/env python3
"""Validate discussion traceability; never approve decisions or product gates."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys


def validate(data, root, phase='review', required=None):
    errors = []
    root = Path(root).resolve()

    def need(ok, message):
        if not ok:
            errors.append(message)

    def file_path(path):
        p = (root / path).resolve()
        if not p.is_relative_to(root) or not p.is_file():
            raise ValueError('Missing or out-of-repository file: ' + path)
        return p

    def indexed(items, name):
        need(isinstance(items, list) and bool(items), name + ' must not be empty')
        result = {}
        for item in items:
            ident = item['id']
            need(isinstance(ident, str) and bool(ident.strip()), name + ' has empty ID')
            need(ident not in result, name + ' has duplicate ID: ' + str(ident))
            result[ident] = item
        return result

    need(data['schema_version'] == 1, 'Unsupported schema_version')
    need(bool(re.fullmatch(r'DISC-\d{3,}', data['id'])), 'Invalid discussion ID')
    need(data['topic'] == 'alpha', 'This manifest must identify topic alpha')
    file_path(data['analysis'])
    sources = indexed(data['sources'], 'sources')
    for source in sources.values():
        need(bool(source['locator'].strip()), 'Source needs a locator')
        need(source['kind'] in {'discussion', 'user_instruction', 'repository'}, 'Invalid source kind')
        if source['kind'] == 'discussion':
            need(bool(re.fullmatch(r'[a-f0-9]{64}', source['sha256'])), 'Source needs SHA-256')
    issues = data['review_issues']
    need(isinstance(issues, list) and bool(issues), 'Missing developer confirmation Issue')
    need(all(type(n) is int and n > 0 for n in issues), 'Invalid Issue number')
    checks = indexed(data['verifications'], 'verifications')
    for check in checks.values():
        need(check['status'] in {'NotRun', 'Passed', 'Failed', 'Blocked'}, 'Invalid verification status')
        need(check['kind'] in {'document', 'skill', 'mechanism', 'product', 'deployment'}, 'Invalid evidence kind')
        need(bool(check['criterion'].strip()), 'Verification needs a criterion')
        if check['status'] == 'Passed':
            need(bool(check['evidence']), 'Passed check lacks evidence: ' + check['id'])
        for p in check['evidence']:
            file_path(p)
    reqs = indexed(data['requirements'], 'requirements')
    for ident, req in reqs.items():
        need(ident.startswith(data['id'] + '-R'), 'Requirement ID must belong to discussion')
        need(req['status'] in {'Pending', 'Accepted', 'Rejected', 'Deferred'}, 'Invalid decision status')
        need(bool(req['source_ids']) and set(req['source_ids']) <= sources.keys(), 'Unknown/missing source: ' + ident)
        need(bool(req['issues']) and set(req['issues']) <= set(issues), 'Missing review Issue mapping: ' + ident)
        need(bool(req['checks']) and set(req['checks']) <= checks.keys(), 'Unknown/missing verification: ' + ident)
        need(set(req['depends_on']) <= reqs.keys(), 'Unknown dependency: ' + ident)
        need(ident not in req['depends_on'], 'Self dependency: ' + ident)
        need(bool(req['fingerprints']), 'Missing specification fingerprint: ' + ident)
        for path, expected in req['fingerprints'].items():
            actual = hashlib.sha256(file_path(path).read_bytes()).hexdigest()
            need(expected == actual, 'Stale specification fingerprint: ' + path)
        confirm = req['confirmation']
        if req['status'] == 'Accepted':
            need(isinstance(confirm, dict), 'Accepted without confirmation: ' + ident)
            if isinstance(confirm, dict):
                need(bool(confirm.get('author')) and bool(confirm.get('date')), 'Missing confirmer/date: ' + ident)
                need(bool(re.fullmatch(r'https://github\.com/wyw-ai/loom-oi/issues/\d+#issuecomment-\d+', confirm.get('url', ''))), 'Missing original developer comment: ' + ident)
                need(ident in confirm.get('scope', []), 'Confirmation scope missing: ' + ident)
                need(confirm.get('fingerprints') == req['fingerprints'], 'Confirmation covers another specification: ' + ident)
        else:
            need(confirm is None, 'Non-accepted requirement must not carry an active approval: ' + ident)

    visited, visiting = set(), set()

    def visit(ident):
        if ident in visiting:
            errors.append('Dependency cycle: ' + ident)
            return
        if ident in visited or ident not in reqs:
            return
        visiting.add(ident)
        for dep in reqs[ident]['depends_on']:
            visit(dep)
        visiting.remove(ident)
        visited.add(ident)

    for ident in reqs:
        visit(ident)
    tasks = indexed(data['tasks'], 'tasks')
    covered = set()
    for task in tasks.values():
        need(bool(task['requirements']) and set(task['requirements']) <= reqs.keys(), 'Task has no valid requirement: ' + task['id'])
        need(bool(task['checks']) and set(task['checks']) <= checks.keys(), 'Task has no valid verification: ' + task['id'])
        need(task['status'] in {'Planned', 'Blocked', 'Done'}, 'Invalid task status')
        need(task['kind'] in {'documentation', 'implementation', 'validation'}, 'Invalid task kind')
        covered.update(task['requirements'])
        if task['kind'] == 'implementation' and task['status'] == 'Done':
            need(all(reqs.get(x, {}).get('status') == 'Accepted' for x in task['requirements']), 'Implementation marked Done before approval')
    need(covered == reqs.keys(), 'Requirements missing task coverage')
    if phase == 'implement':
        selected = set(required or reqs.keys())
        need(selected <= reqs.keys(), 'Unknown requested requirement')
        pending = list(selected)
        while pending:
            ident = pending.pop()
            for dep in reqs.get(ident, {}).get('depends_on', []):
                if dep not in selected:
                    selected.add(dep)
                    pending.append(dep)
        for ident in selected & reqs.keys():
            need(reqs[ident]['status'] == 'Accepted', 'Implementation blocked by ' + ident + ': ' + reqs[ident]['status'])
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    parser.add_argument('--phase', choices=['review', 'implement'], default='review')
    parser.add_argument('--require', action='append', help='Requirement scope for implement; repeat as needed')
    args = parser.parse_args()
    if args.require and args.phase != 'implement':
        parser.error('--require is only meaningful with --phase implement')
    root = Path(__file__).resolve().parents[1]
    try:
        errors = validate(json.loads(args.manifest.read_text()), root, args.phase, args.require)
    except (KeyError, TypeError, ValueError, OSError) as exc:
        errors = ['Invalid manifest: ' + str(exc)]
    if errors:
        print('\n'.join('FAIL: ' + e for e in errors))
        return 1
    print('PASS: SDD ' + args.phase + ' record checks only; verify original developer confirmation and experiment gates separately.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
