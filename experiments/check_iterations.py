"""Check evidence-to-analysis coverage; never claim semantic or remote-write validation."""
import argparse
import hashlib
import json
from pathlib import Path
from verify import verify

ROOT = Path(__file__).resolve().parent.parent


def check_primary_result(manifest, summary, verification=None):
    if ('execution_profile' in manifest.get('case', {}) or 'local_execution' in manifest.get('case', {})):
        if not isinstance(verification, dict) or verification.get('execution_supervision') != 'complete':
            raise ValueError('Local primary evidence lacks complete execution supervision')
    if summary.get('status') != 'observed':
        raise ValueError('Inconclusive input presented as primary evidence')
    # Mechanism runs predate product verdicts and may support a negative
    # finding. Product primary evidence must have an explicit passing verdict;
    # an observed failure remains preserved as superseded/inconclusive input.
    case = manifest.get('case')
    kind = case.get('kind', '') if isinstance(case, dict) else ''
    product = (isinstance(kind, str) and kind.startswith('product-')) or summary.get('evidence_class') == 'product_implementation'
    if product and summary.get('verdict') != 'pass':
        raise ValueError('Product primary evidence does not have a passing verdict')


def check(root=ROOT):
    root = Path(root).resolve()
    runs = {p.name: p for p in (root / 'experiments/runs').iterdir() if p.is_dir()}
    covered, iteration_ids = set(), set()

    def local_file(relative):
        p = (root / relative).resolve()
        if not p.is_relative_to(root) or not p.is_file():
            raise ValueError('Missing or external artifact: ' + relative)
        return p

    verified = {name: verify(run) for name, run in runs.items()}
    for path in sorted((root / 'experiments/iterations').glob('*.json')):
        record = json.loads(path.read_text(encoding='utf-8'))
        if record['schema_version'] != 1 or record['id'] in iteration_ids:
            raise ValueError('Invalid schema or duplicate iteration ID')
        iteration_ids.add(record['id'])
        analysis = local_file(record['analysis']).read_text(encoding='utf-8')
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
            if item['use'] not in {'primary', 'superseded', 'inconclusive'}:
                raise ValueError('Invalid input use')
            if item['use'] == 'primary':
                manifest = json.loads((run / 'manifest.json').read_text(encoding='utf-8'))
                summary = json.loads((run / 'summary.json').read_text(encoding='utf-8'))
                check_primary_result(manifest, summary, verified[name])
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
