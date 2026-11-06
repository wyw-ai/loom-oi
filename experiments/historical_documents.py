"""Read retired documentation for existing evidence records, never missing run data."""
import hashlib
from pathlib import Path, PurePosixPath
import re
import subprocess

DOCUMENT_BASE = 'ff30255f442408ffdddc9e1c9b58daed05eef2ee'
KEPT_DOCUMENTS = {'docs/concept/loom-oi.md', 'docs/architecture/alpha-v0.7.md',
                  'docs/product/operations.md'}
RETIRED_EXPERIMENT_DOCS = {'experiments/README.md', 'experiments/GATES.md',
                           'experiments/materials/README.md'}
RETIRED_TOOLS = {'tools/' + name for name in (
    'check_sdd.py', 'check_structure.py', 'check_task.py', 'issue_evolution.py',
    'coverage-policy.json', 'test_issue_evolution.py', 'test_portability.py',
    'test_sdd.py', 'test_structure.py', 'test_task.py', 'test_tool_entrypoints.py')}


def read_document(root, relative):
    root = Path(root).resolve()
    if (not isinstance(relative, str) or not relative or '\\' in relative or ':' in relative
            or '\0' in relative or PurePosixPath(relative).is_absolute()
            or any(part in ('', '.', '..') for part in relative.split('/'))):
        raise ValueError('Missing or external artifact: ' + str(relative))
    path = root / relative
    if not path.resolve().is_relative_to(root):
        raise ValueError('Missing or external artifact: ' + relative)
    # A changed existing file must still fail its old fingerprint. Do not hide
    # drift, unreadable files, directories or broken symlinks behind history.
    if path.exists() or path.is_symlink():
        if not path.is_file():
            raise ValueError('Missing or external artifact: ' + relative)
        return path.read_bytes()
    retired = (relative.startswith(('docs/', 'spec/', 'kb/', '.agents/'))
               and relative not in KEPT_DOCUMENTS) or relative in RETIRED_TOOLS or relative in RETIRED_EXPERIMENT_DOCS
    if not retired:
        raise ValueError('Missing or external artifact: ' + relative)
    try:
        top = subprocess.run(['git', '-C', str(root), 'rev-parse', '--show-toplevel'],
                             capture_output=True, check=True, timeout=10)
        if Path(top.stdout.decode('utf-8').strip()).resolve() != root:
            raise ValueError('Historical document root is not the repository root')
        blob = subprocess.run(['git', '-C', str(root), 'cat-file', 'blob',
                               DOCUMENT_BASE + ':' + relative],
                              capture_output=True, check=True, timeout=10)
    except (OSError, subprocess.SubprocessError) as exc:
        raise ValueError('Missing historical artifact at ' + DOCUMENT_BASE + ': ' + relative) from exc
    return blob.stdout


def fingerprint(root, reference):
    """Preserve the existing byte/scope fingerprint format used by gate records."""
    path, _, scope = reference.partition('#')
    if '#' in reference and not re.fullmatch(r'[A-Za-z0-9_-]+', scope):
        raise ValueError('Invalid specification scope: ' + reference)
    content = read_document(root, path)
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
