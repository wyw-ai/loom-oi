#!/usr/bin/env python3
"""Validate scoped SDD records; never infer human approval or product readiness."""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import sys

COMMENT = r'https://github\.com/wyw-ai/loom-oi/issues/\d+#issuecomment-\d+'
REQUEST = r'chat-request:[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}'
TOOL_CALL = r'tool-call:call_[A-Za-z0-9_-]+'


def local_file(root, path):
    root = Path(root).resolve()
    p = (root / path).resolve()
    if not p.is_relative_to(root) or not p.is_file():
        raise ValueError('Missing or out-of-repository file: ' + path)
    return p


def fingerprint(root, reference):
    """Hash a whole legacy file or one explicit sdd-scope block, including whitespace."""
    path, _, scope = reference.partition('#')
    if '#' in reference and not re.fullmatch(r'[A-Za-z0-9_-]+', scope):
        raise ValueError('Invalid specification scope: ' + reference)
    content = local_file(root, path).read_bytes()
    if scope:
        start = ('<!-- sdd-scope:' + scope + ' -->').encode()
        end = ('<!-- /sdd-scope:' + scope + ' -->').encode()
        if content.count(start) != 1 or content.count(end) != 1:
            raise ValueError('Missing/duplicate specification scope: ' + reference)
        begin, finish = content.index(start) + len(start), content.index(end)
        if finish <= begin or not content[begin:finish].strip():
            raise ValueError('Empty/reversed specification scope: ' + reference)
        content = content[begin:finish]
    return hashlib.sha256(content).hexdigest()


def user_response_errors(confirm, ident):
    """Check a recorded user reply's provenance, not the meaning of its answer."""
    source = confirm['source']
    locator, items, scope = source.get('locator'), confirm.get('items'), confirm.get('scope')
    if not isinstance(locator, str) or not re.fullmatch(TOOL_CALL, locator):
        return ['Invalid user response tool call: ' + ident]
    if (not isinstance(items, list) or not items or not isinstance(scope, list) or not scope
            or any(not isinstance(x, str) or not x.strip() for x in scope) or len(scope) != len(set(scope))):
        return ['Missing/invalid user response items or scope: ' + ident]
    call_id = locator.removeprefix('tool-call:')
    seen, covered, answers, errors = set(), set(), [], []
    for item in items:
        if not isinstance(item, dict):
            errors.append('Invalid user response item: ' + ident)
            continue
        try:
            key = json.loads(item.get('questionItemId', ''))
        except (TypeError, ValueError):
            key = None
        if (not isinstance(key, list) or len(key) != 3 or key[0] != 'request_user_input_async'
                or key[1] != call_id or type(key[2]) is not int or key[2] < 0):
            errors.append('User response item/call mismatch: ' + ident)
        elif key[2] in seen:
            errors.append('Duplicate user response item: ' + ident)
        else:
            seen.add(key[2])
        question, answer = item.get('question'), item.get('answer')
        if not isinstance(question, str) or not question.strip() or not isinstance(answer, str) or not answer.strip():
            errors.append('Missing user response question/answer: ' + ident)
        else:
            answers.append(answer)
        item_scope = item.get('scope')
        if (not isinstance(item_scope, list) or not item_scope
                or any(not isinstance(x, str) or not x.strip() for x in item_scope)
                or len(item_scope) != len(set(item_scope)) or not set(item_scope) <= set(scope)):
            errors.append('Invalid user response item scope: ' + ident)
            continue
        if covered.intersection(item_scope):
            errors.append('Duplicate user response requirement coverage: ' + ident)
        covered.update(item_scope)
    if covered != set(scope) or ident not in covered:
        errors.append('User response does not cover confirmation scope: ' + ident)
    if source.get('quote') != '\n'.join(answers):
        errors.append('User response quote differs from original answers: ' + ident)
    return errors


def confirmation_errors(confirm, ident, fingerprints):
    if not isinstance(confirm, dict):
        return ['Accepted without confirmation: ' + ident]
    errors = []
    if not isinstance(confirm.get('author'), str) or not confirm['author'].strip():
        errors.append('Missing confirmer: ' + ident)
    try:
        datetime.fromisoformat(confirm.get('date', '').replace('Z', '+00:00'))
    except (ValueError, AttributeError):
        errors.append('Missing/invalid confirmation date: ' + ident)
    source = confirm.get('source')
    if source is None:  # Legacy Issue approval remains valid without changing its scope.
        if not re.fullmatch(COMMENT, confirm.get('url', '')):
            errors.append('Missing original developer comment: ' + ident)
    else:
        kind, locator = source['kind'], source['locator']
        if kind == 'user_response':
            errors.extend(user_response_errors(confirm, ident))
        else:
            pattern = {'github_comment': COMMENT, 'user_instruction': REQUEST}.get(kind)
            if pattern is None or not re.fullmatch(pattern, locator) or not source.get('quote', '').strip():
                errors.append('Invalid original confirmation source: ' + ident)
    if ident not in confirm.get('scope', []):
        errors.append('Confirmation scope missing: ' + ident)
    approved = confirm.get('fingerprints', {})
    if not all(approved.get(path) == value for path, value in fingerprints.items()):
        errors.append('Confirmation covers another specification: ' + ident)
    return errors


def validate(data, root, phase='review', required=None):
    try:
        return _validate(data, Path(root).resolve(), phase, required)
    except (KeyError, TypeError, ValueError, OSError, AttributeError) as exc:
        return ['Invalid manifest: ' + str(exc)]


def _validate(data, root, phase, required):
    errors = []
    def need(ok, message):
        if not ok:
            errors.append(message)
    def indexed(items, name):
        need(isinstance(items, list) and bool(items), name + ' must not be empty')
        result = {}
        for item in items:
            ident = item['id']
            need(isinstance(ident, str) and bool(ident.strip()), name + ' has empty ID')
            need(ident not in result, name + ' has duplicate ID: ' + str(ident))
            result[ident] = item
        return result
    need(phase in {'review', 'implement'}, 'Invalid phase')
    need(not required or phase == 'implement', 'Scope selection requires implement phase')
    need(data['schema_version'] in {1, 2}, 'Unsupported schema_version')
    need(bool(re.fullmatch(r'DISC-\d{3,}', data['id'])), 'Invalid discussion ID')
    need(data['topic'] == 'alpha', 'This manifest must identify topic alpha')
    local_file(root, data['analysis'])
    sources = indexed(data['sources'], 'sources')
    for source in sources.values():
        need(bool(source['locator'].strip()), 'Source needs a locator')
        need(source['kind'] in {'discussion', 'user_instruction', 'repository'}, 'Invalid source kind')
        if source['kind'] == 'discussion':
            need(bool(re.fullmatch(r'[a-f0-9]{64}', source['sha256'])), 'Source needs SHA-256')
    issues = data['review_issues']
    need(isinstance(issues, list) and bool(issues), 'Missing tracking/review Issue')
    need(all(type(n) is int and n > 0 for n in issues), 'Invalid Issue number')
    reqs = indexed(data['requirements'], 'requirements')
    checks = indexed(data['verifications'], 'verifications')
    tasks = indexed(data['tasks'], 'tasks')
    # Global identity/reference integrity is structural; adoption and bytes are scoped.
    for ident, req in reqs.items():
        need(ident.startswith(data['id'] + '-R'), 'Requirement ID must belong to discussion')
        need(set(req['depends_on']) <= reqs.keys(), 'Unknown dependency: ' + ident)
    selected = set(required or reqs) if phase == 'implement' else set(reqs)
    need(selected <= reqs.keys(), 'Unknown requested requirement')
    visited, visiting = set(), set()
    def visit(ident):
        if ident in visiting:
            errors.append('Dependency cycle: ' + ident)
            return
        if ident in visited or ident not in reqs:
            return
        visiting.add(ident)
        for dep in reqs[ident]['depends_on']:
            selected.add(dep)
            visit(dep)
        visiting.remove(ident)
        visited.add(ident)
    for ident in list(selected):
        visit(ident)
    relevant_checks = set()
    for ident in sorted(selected & reqs.keys()):
        req = reqs[ident]
        need(ident.startswith(data['id'] + '-R'), 'Requirement ID must belong to discussion')
        need(req['status'] in {'Pending', 'Accepted', 'Rejected', 'Deferred'}, 'Invalid decision status')
        need(bool(req['source_ids']) and set(req['source_ids']) <= sources.keys(), 'Unknown/missing source: ' + ident)
        need(bool(req['issues']) and set(req['issues']) <= set(issues), 'Missing review Issue mapping: ' + ident)
        need(bool(req['checks']) and set(req['checks']) <= checks.keys(), 'Unknown/missing verification: ' + ident)
        need(set(req['depends_on']) <= reqs.keys(), 'Unknown dependency: ' + ident)
        need(ident not in req['depends_on'], 'Self dependency: ' + ident)
        need(bool(req['fingerprints']), 'Missing specification fingerprint: ' + ident)
        for path, expected in req['fingerprints'].items():
            need('#' not in path or data['schema_version'] == 2, 'Scoped fingerprints require schema 2')
            need(expected == fingerprint(root, path), 'Stale specification fingerprint: ' + path)
        confirm = req.get('confirmation')
        if req.get('confirmation_ref'):
            need(data['schema_version'] == 2 and confirm is None, 'Ambiguous/legacy confirmation reference: ' + ident)
            confirm = json.loads(local_file(root, req['confirmation_ref']).read_text(encoding='utf-8'))
        if req['status'] == 'Accepted':
            errors.extend(confirmation_errors(confirm, ident, req['fingerprints']))
        else:
            need(confirm is None, 'Non-accepted requirement must not carry an active approval: ' + ident)
        if phase == 'implement':
            need(req['status'] == 'Accepted', 'Implementation blocked by ' + ident + ': ' + req['status'])
        relevant_checks.update(req['checks'])
    covered = set()
    for task in tasks.values():
        if phase == 'implement' and not (set(task['requirements']) & selected):
            continue
        need(bool(task['requirements']) and set(task['requirements']) <= reqs.keys(), 'Task has no valid requirement: ' + task['id'])
        need(bool(task['checks']) and set(task['checks']) <= checks.keys(), 'Task has no valid verification: ' + task['id'])
        need(task['status'] in {'Planned', 'Blocked', 'Done'}, 'Invalid task status')
        need(task['kind'] in {'documentation', 'implementation', 'validation'}, 'Invalid task kind')
        covered.update(task['requirements'])
        relevant_checks.update(task['checks'])
        if task['kind'] == 'implementation' and task['status'] == 'Done':
            need(all(reqs.get(x, {}).get('status') == 'Accepted' for x in task['requirements']), 'Implementation marked Done before approval')
    need(selected <= covered, 'Requirements missing task coverage')
    for ident, check in checks.items():
        if phase == 'implement' and ident not in relevant_checks:
            continue
        need(check['status'] in {'NotRun', 'Passed', 'Failed', 'Blocked'}, 'Invalid verification status')
        need(check['kind'] in {'document', 'skill', 'mechanism', 'product', 'deployment'}, 'Invalid evidence kind')
        need(bool(check['criterion'].strip()), 'Verification needs a criterion')
        if check['status'] == 'Passed':
            need(bool(check['evidence']), 'Passed check lacks evidence: ' + check['id'])
        for path in check['evidence']:
            local_file(root, path)
    return errors


def main(argv=None, root=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    parser.add_argument('--phase', choices=['review', 'implement'], default='review')
    parser.add_argument('--require', action='append', help='Repeat for the exact implementation scope')
    args = parser.parse_args(argv)
    if args.require and args.phase != 'implement':
        parser.error('--require is only meaningful with --phase implement')
    root = Path(root) if root else Path(__file__).resolve().parents[1]
    try:
        errors = validate(json.loads(args.manifest.read_text(encoding='utf-8')), root, args.phase, args.require)
    except (ValueError, OSError) as exc:
        errors = ['Invalid manifest: ' + str(exc)]
    if errors:
        print('\n'.join('FAIL: ' + x for x in errors))
        return 1
    print('PASS: SDD ' + args.phase + ' record checks only; verify original confirmation and product gates separately.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
