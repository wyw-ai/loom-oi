"""Check evidence-to-analysis coverage; never claim semantic or remote-write validation."""
import argparse
import hashlib
import json
from pathlib import Path
from verify import verify

ROOT = Path(__file__).resolve().parent.parent

def check(root=ROOT):
    root = Path(root).resolve()
    runs = {p.name: p for p in (root / 'experiments/runs').iterdir() if p.is_dir()}
    covered, iteration_ids = set(), set()

    def local_file(relative):
        p = (root / relative).resolve()
        if not p.is_relative_to(root) or not p.is_file():
            raise ValueError('Missing or external artifact: ' + relative)
        return p

    for run in runs.values():
        verify(run)
    for path in sorted((root / 'experiments/iterations').glob('*.json')):
        record = json.loads(path.read_text())
        if record['schema_version'] != 1 or record['id'] in iteration_ids:
            raise ValueError('Invalid schema or duplicate iteration ID')
        iteration_ids.add(record['id'])
        analysis = local_file(record['analysis']).read_text()
        claims = record['claims']
        if not claims or len(set(claims)) != len(claims) or any(c not in analysis for c in claims):
            raise ValueError('Missing/duplicate conclusion references')
        if not record['inputs'] and not record.get('blocked_reason'):
            raise ValueError('No inputs and no blocked reason')
        seen = set()
        for item in record['inputs']:
            name = item['run']
            if name not in runs or name in seen or not item['reason'].strip():
                raise ValueError('Unknown/duplicate input or missing rationale')
            seen.add(name)
            run = runs[name]
            fingerprint = hashlib.sha256((run / 'SHA256SUMS').read_bytes()).hexdigest()
            if item['sha256sums_sha256'] != fingerprint:
                raise ValueError('Analysis input changed: ' + name)
            status = json.loads((run / 'summary.json').read_text())['status']
            if item['use'] not in {'primary', 'superseded', 'inconclusive'}:
                raise ValueError('Invalid input use')
            if status != 'observed' and item['use'] == 'primary':
                raise ValueError('Inconclusive input presented as primary evidence')
            covered.add(name)
        if not record['artifacts'] or not record['issues']:
            raise ValueError('Missing propagation record')
        for item in record['artifacts']:
            local_file(item['path'])
            if item['action'] not in {'updated', 'unchanged'} or not item['reason'].strip():
                raise ValueError('Missing artifact disposition')
        for item in record['issues']:
            if not isinstance(item['number'], int) or item['number'] < 1:
                raise ValueError('Invalid Issue number')
            if item['action'] not in {'updated', 'unchanged'} or not item['reason'].strip():
                raise ValueError('Missing Issue disposition')
    pending = sorted(set(runs) - covered)
    return {'iterations': len(iteration_ids), 'verified_runs': len(runs), 'pending_runs': pending,
            'scope': 'Input integrity and analysis records only; not semantic review or GitHub delivery proof.'}

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, default=ROOT)
    args = parser.parse_args()
    try:
        result = check(args.root)
        print(json.dumps(result, ensure_ascii=False))
        raise SystemExit(2 if result['pending_runs'] else 0)
    except (ValueError, KeyError, OSError, AssertionError, TypeError) as exc:
        print(json.dumps({'error': str(exc)}, ensure_ascii=False))
        raise SystemExit(1)
