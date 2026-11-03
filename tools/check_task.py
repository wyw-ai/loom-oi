#!/usr/bin/env python3
"""Check task scope and local delivery evidence, not human intent or merge permission."""
import argparse
import fnmatch
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
from check_sdd import local_file, validate as validate_sdd


def source_files(root):
    root = Path(root).resolve()
    policy = json.loads(local_file(root, 'tools/coverage-policy.json').read_text())
    files = set()
    for directory in policy['source_roots']:
        path = (Path(root) / directory).resolve()
        if not path.is_relative_to(Path(root).resolve()) or not path.is_dir():
            raise ValueError('Invalid coverage source root')
        files.update(p.relative_to(root).as_posix() for p in path.rglob('*.py'))
    measured = {p for p in files if not any(fnmatch.fnmatchcase(p, omit) for omit in policy['omit'])}
    if not measured:
        raise ValueError('Empty coverage source scope')
    return files, measured


def source_digest(root):
    root = Path(root).resolve()
    files, _ = source_files(root)
    files.add('tools/coverage-policy.json')
    h = hashlib.sha256()
    for path in sorted(files):
        h.update(path.encode() + b'\0' + local_file(root, path).read_bytes() + b'\0')
    return h.hexdigest()


def coverage_errors(root, evidence):
    _, expected = source_files(root)
    report = json.loads(local_file(root, evidence['coverage_report']).read_text())
    errors = []
    if evidence['source_digest'] != source_digest(root):
        errors.append('Stale unit-test evidence: source/tests/policy changed')
    if evidence.get('suite') != 'unit' or not evidence.get('command', '').strip():
        errors.append('Unit-only test command/evidence required')
    actual, covered, total = set(), 0, 0
    for path, file in report['files'].items():
        p = local_file(root, path).relative_to(root).as_posix()
        if p in actual:
            errors.append('Duplicate coverage source: ' + p)
        actual.add(p)
        summary = file['summary']
        n, c = summary['num_statements'], summary['covered_lines']
        if type(n) is not int or type(c) is not int or not 0 <= c <= n:
            raise ValueError('Invalid coverage numerator/denominator')
        covered += c
        total += n
    if actual != expected:
        errors.append('Coverage file set differs from fixed source policy')
    if total <= 0 or covered * 100 < total * 95:
        errors.append(f'Unit coverage below 95%: {covered}/{total}')
    if (report['totals']['num_statements'], report['totals']['covered_lines']) != (total, covered):
        errors.append('Coverage totals do not match file summaries')
    return errors


def document_only(path):
    if path.endswith(('.md', '.svg', '.dot')):
        return True
    return ((path.startswith(('docs/', 'kb/', 'spec/')) and path.endswith(('.json', '.jsonl')))
            or (path.startswith('.agents/') and path.endswith('/agents/openai.yaml')))



def documentation_digest(root, changed, excluded=()):
    root = Path(root).resolve()
    h = hashlib.sha256()
    for path in sorted(set(changed) - set(excluded)):
        if document_only(path):
            p = (root / path).resolve()
            if not p.is_relative_to(root):
                raise ValueError('Document path escapes repository')
            content = p.read_bytes() if p.is_file() else b'<deleted>'
            h.update(path.encode() + b'\0' + content + b'\0')
    return h.hexdigest()


def validate(data, root, task_path, branch, changed, phase='draft'):
    try:
        return _validate(data, Path(root).resolve(), task_path, branch, changed, phase)
    except (KeyError, TypeError, ValueError, OSError, AttributeError) as exc:
        return ['Invalid task/evidence: ' + str(exc)]


def _validate(data, root, task_path, branch, changed, phase):
    errors = []
    directory = local_file(root, task_path).parent
    ident, name = data['id'], directory.name
    if data['schema_version'] != 1 or not re.fullmatch(r'TASK-\d{3,}', ident):
        errors.append('Invalid task schema/ID')
    if directory.parent != root / 'spec' or not name.startswith(ident + '-'):
        errors.append('Task must live in its own spec directory')
    if branch != data['branch'] or branch != 'feature/' + name:
        errors.append('Branch must bind exactly this task directory')
    if not data['goal'].strip() or not data['base_branch'].strip() or not re.fullmatch(r'[a-f0-9]{40}', data['base_commit']):
        errors.append('Task needs goal and exact integration base')
    if phase not in {'draft', 'review'}:
        errors.append('Invalid task phase')
    for candidate in (root / 'spec').glob('*/task.json'):
        if candidate != directory / 'task.json' and candidate.parent.name != '_template':
            if json.loads(candidate.read_text())['id'] == ident:
                errors.append('Duplicate task ID: ' + ident)
    for path in changed:
        if not any(fnmatch.fnmatchcase(path, allowed) for allowed in data['allowed_paths']):
            errors.append('Change outside task scope: ' + path)
        parts = Path(path).parts
        if len(parts) > 2 and parts[0] == 'spec' and parts[1] not in {name, '_template'}:
            errors.append('Another task directory changed: ' + path)
    manifest = json.loads(local_file(root, data['manifest']).read_text())
    if not data['requirements']:
        errors.append('Task has no requirement scope')
    else:
        errors.extend(validate_sdd(manifest, root, 'implement', data['requirements']))
    if phase == 'review':
        evidence = data['verification']
        if evidence['status'] != 'Passed':
            errors.append('Verification has not passed')
        if not local_file(root, evidence['document_review']).read_text().strip():
            errors.append('Document-code consistency record is empty')
        excluded = {task_path, evidence['document_review'], evidence.get('coverage_report', '')}
        if evidence['reviewed_document_digest'] != documentation_digest(root, changed, excluded):
            errors.append('Document review is stale: reviewed documents changed')
        if evidence['reviewed_source_digest'] != source_digest(root):
            errors.append('Document review refers to another source version')
        if any(not document_only(path) for path in changed):
            _, measured = source_files(root)
            if any(path.endswith(('.py', '.go', '.rs', '.js', '.ts', '.sh', '.mjs', '.tsx', '.jsx'))
                   and path not in measured and not fnmatch.fnmatchcase(path, 'tools/test_*.py') for path in changed):
                errors.append('Changed executable code is outside this coverage policy')
            errors.extend(coverage_errors(root, evidence))
        elif not evidence.get('coverage_not_applicable_reason', '').strip():
            errors.append('Document-only task needs a coverage applicability reason')
    return errors


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


def main(argv=None, root=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('task', help='Repository-relative spec task.json')
    parser.add_argument('--phase', choices=['draft', 'review'], default='draft')
    parser.add_argument('--branch', help='Explicit head branch for detached CI checkout')
    args = parser.parse_args(argv)
    root = Path(root) if root else Path(__file__).resolve().parents[1]
    try:
        data = json.loads(local_file(root, args.task).read_text())
        base = data['base_commit']
        if not re.fullmatch(r'[a-f0-9]{40}', base):
            raise ValueError('Invalid base commit')
        if git(root, 'rev-parse', data['base_branch']) != base:
            raise ValueError('Integration branch advanced; sync and revalidate first')
        subprocess.run(['git', '-C', str(root), 'merge-base', '--is-ancestor', base, 'HEAD'], check=True, capture_output=True)
        changed = set(git(root, 'diff', '--name-only', base).splitlines())
        changed.update(git(root, 'ls-files', '--others', '--exclude-standard').splitlines())
        branch = args.branch or git(root, 'branch', '--show-current')
        errors = validate(data, root, args.task, branch, changed, args.phase)
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as exc:
        errors = ['Invalid task/Git input: ' + str(exc)]
    if errors:
        print('\n'.join('FAIL: ' + error for error in errors))
        return 1
    print('PASS: task ' + args.phase + ' records; human scope/content review and merge approval remain separate.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
