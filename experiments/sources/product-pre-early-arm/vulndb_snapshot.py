"""Fixed official Go vulnerability input for isolated local evidence runs.

No download, extraction, tool execution or online fallback happens here.
The archived ZIP is the input; the live directory must match every member.
"""
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
from urllib.parse import urlsplit
import zipfile

SCHEMA = 'go-vulndb-snapshot-v1'
FACT_SCHEMA = 'go-vulndb-input-v1'
URL = 'https://vuln.go.dev/vulndb.zip'
TOOL = 'golang.org/x/vuln/cmd/govulncheck@v1.9.0'
ENV_KEYS = ('OI_VULNDB_ZIP', 'OI_VULNDB_PROVENANCE', 'OI_VULNDB_DIR')
INDEX = {'index/db.json', 'index/modules.json', 'index/vulns.json'}
SPEC_KEYS = {'schema', 'url', 'zip_sha256', 'zip_bytes', 'provenance_sha256',
             'download_completed_utc', 'modified', 'index_sha256', 'members',
             'vulnerability_ids', 'modules'}
HASH = re.compile(r'[0-9a-f]{64}\Z')
ID = re.compile(r'GO-[0-9]{4}-[0-9]{4,}\Z')
MAX_ZIP = 64 * 1024 * 1024
MAX_EXPANDED = 256 * 1024 * 1024


def require(value, message):
    if not value:
        raise ValueError('vulndb: ' + message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=False, indent=2) + '\n').encode()


def timestamp(value):
    require(isinstance(value, str), 'timestamp must be text')
    try:
        parsed = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError as exc:
        raise ValueError('vulndb: invalid UTC timestamp') from exc
    require(parsed.utcoffset() == datetime.timedelta(0), 'timestamp must be UTC')
    return parsed


def validate_spec(value):
    require(isinstance(value, dict) and set(value) == SPEC_KEYS, 'preregistration fields differ')
    require(value['schema'] == SCHEMA and value['url'] == URL, 'unregistered schema/source')
    for key in ('zip_sha256', 'provenance_sha256'):
        require(isinstance(value[key], str) and HASH.fullmatch(value[key]), 'invalid ' + key)
    require(isinstance(value['index_sha256'], dict) and set(value['index_sha256']) == INDEX
            and all(isinstance(v, str) and HASH.fullmatch(v) for v in value['index_sha256'].values()),
            'index hashes differ')
    for key in ('zip_bytes', 'members', 'vulnerability_ids', 'modules'):
        require(type(value[key]) is int and value[key] > 0, 'invalid ' + key)
    require(value['zip_bytes'] <= MAX_ZIP and value['members'] == value['vulnerability_ids'] + 3,
            'invalid archive bounds')
    require(timestamp(value['modified']) <= timestamp(value['download_completed_utc']),
            'database modification is after recorded retrieval')
    return value


def validate_archive(spec, archive, provenance_bytes):
    validate_spec(spec)
    require(len(archive) == spec['zip_bytes'] and sha(archive) == spec['zip_sha256'], 'ZIP digest/size differs')
    require(sha(provenance_bytes) == spec['provenance_sha256'], 'provenance digest differs')
    provenance = json.loads(provenance_bytes)
    require(isinstance(provenance, dict), 'invalid provenance object')
    require(provenance.get('url') == URL and provenance.get('resolved_url') == URL
            and provenance.get('sha256') == spec['zip_sha256']
            and provenance.get('bytes') == spec['zip_bytes']
            and provenance.get('download_completed_utc') == spec['download_completed_utc']
            and provenance.get('archive_prefix') == '', 'provenance source differs')
    require(timestamp(provenance.get('download_started_utc')) <= timestamp(spec['download_completed_utc']),
            'download interval reversed')
    members, documents = {}, {}
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
        infos = zipped.infolist()
        require(len(infos) == spec['members'] and sum(i.file_size for i in infos) <= MAX_EXPANDED,
                'archive member/expanded bound differs')
        for info in infos:
            name = info.filename
            valid = name in INDEX or (name.startswith('ID/') and name.endswith('.json')
                                      and ID.fullmatch(name[3:-5]))
            mode = info.external_attr >> 16
            require(valid and not info.is_dir() and name not in members
                    and stat.S_IFMT(mode) in (0, stat.S_IFREG), 'unsafe/duplicate ZIP member')
            data = zipped.read(info)  # Includes the actual CRC check, not a provenance boolean.
            members[name] = sha(data)
            documents[name] = json.loads(data)
    require(INDEX <= set(documents) and all(members[k] == v for k, v in spec['index_sha256'].items()),
            'actual index hashes differ')
    require(documents['index/db.json'] == {'modified': spec['modified']}, 'database metadata differs')
    ids = {name[3:-5] for name in documents if name.startswith('ID/')}
    require(len(ids) == spec['vulnerability_ids'], 'ID count differs')
    for ident in ids:
        require(isinstance(documents['ID/' + ident + '.json'], dict)
                and documents['ID/' + ident + '.json'].get('id') == ident, 'ID document identity differs')
    vulns = documents['index/vulns.json']
    require(isinstance(vulns, list) and all(isinstance(v, dict) for v in vulns), 'invalid vulnerability index')
    indexed = [v.get('id') for v in vulns]
    require(all(isinstance(v, str) for v in indexed) and len(indexed) == len(set(indexed))
            and set(indexed) == ids, 'vulnerability index closure differs')
    modules = documents['index/modules.json']
    require(isinstance(modules, list) and len(modules) == spec['modules'], 'module count differs')
    paths, references = set(), set()
    for module in modules:
        require(isinstance(module, dict) and isinstance(module.get('path'), str)
                and module['path'] and module['path'] not in paths
                and isinstance(module.get('vulns'), list), 'invalid/duplicate module entry')
        paths.add(module['path'])
        for vuln in module['vulns']:
            require(isinstance(vuln, dict) and isinstance(vuln.get('id'), str)
                    and vuln['id'] in ids, 'module refers to an absent ID')
            references.add(vuln['id'])
    require(references == ids, 'module index closure differs')
    require(provenance.get('member_sha256') == members
            and provenance.get('db_metadata') == documents['index/db.json']
            and provenance.get('members') == spec['members']
            and provenance.get('vulnerability_ids') == len(ids)
            and provenance.get('module_referenced_ids') == len(references)
            and provenance.get('modules') == len(modules)
            and provenance.get('zip_crc_valid') is True
            and provenance.get('all_index_ids_present') is True, 'provenance member facts differ')
    return members


def regular_bytes(path, limit):
    path = Path(path)
    require(not any(p.is_symlink() for p in path.parents), 'input has a linked parent')
    require(not path.is_symlink() and stat.S_ISREG(path.stat().st_mode), 'input is not a regular file')
    require(path.stat().st_size <= limit, 'input exceeds fixed size bound')
    return path.read_bytes()


def verify_directory(context):
    directory = Path(context['directory'])
    require(directory.is_absolute() and not directory.is_symlink() and directory.is_dir()
            and not any(p.is_symlink() for p in directory.parents), 'invalid DB directory')
    actual = {}
    for parent, dirs, files in os.walk(directory, followlinks=False):
        for name in dirs:
            child = Path(parent) / name
            require(not child.is_symlink() and child.relative_to(directory).as_posix() in ('ID', 'index'),
                    'unexpected/linked database directory')
        for name in files:
            child = Path(parent) / name
            rel = child.relative_to(directory).as_posix()
            require(rel in context['members'], 'unexpected database file')
            actual[rel] = sha(regular_bytes(child, MAX_EXPANDED))
    require(actual == context['members'], 'extracted database changed or is incomplete')
    return fact(context)


def validate_fact(value, spec):
    validate_spec(spec)
    require(isinstance(value, dict) and set(value) == {'schema', 'snapshot', 'uri', 'tool', 'members_sha256'},
            'input fact fields differ')
    require(value['schema'] == FACT_SCHEMA and value['snapshot'] == spec and value['tool'] == TOOL,
            'input fact source differs')
    require(isinstance(value['uri'], str), 'invalid database URI')
    uri = urlsplit(value['uri'])
    require(uri.scheme == 'file' and not uri.netloc and uri.path.startswith('/')
            and not uri.query and not uri.fragment and value['uri'].startswith('file:///'), 'DB URI is not a local absolute file URI')
    require(isinstance(value['members_sha256'], str) and HASH.fullmatch(value['members_sha256']),
            'invalid member manifest digest')
    return value


def fact(context):
    return validate_fact(dict(schema=FACT_SCHEMA, snapshot=context['spec'], uri=Path(context['directory']).as_uri(),
                              tool=TOOL, members_sha256=sha(canonical(context['members']))), context['spec'])


def command(context):
    return ['go', 'run', TOOL, '-db', fact(context)['uri'], './...']


def prepare(case, env, run):
    if 'vulndb_snapshot' not in case:
        require(not any(k in env for k in ENV_KEYS), 'snapshot environment has no preregistration')
        return None
    require(case.get('execution_profile') == 'local-isolated-vm-v1'
            and case.get('steps', []).count('govulncheck') == 1, 'snapshot requires one local govulncheck step')
    require(all(isinstance(env.get(k), str) and env[k] for k in ENV_KEYS), 'missing local snapshot path')
    spec = validate_spec(case['vulndb_snapshot'])
    archive = regular_bytes(env[ENV_KEYS[0]], MAX_ZIP)
    provenance = regular_bytes(env[ENV_KEYS[1]], 2 * 1024 * 1024)
    members = validate_archive(spec, archive, provenance)
    directory = Path(env[ENV_KEYS[2]])
    context = dict(spec=spec, directory=str(directory), members=members)
    verify_directory(context)
    for name, data in (('vulndb.zip', archive), ('vulndb-provenance.json', provenance)):
        with (Path(run) / name).open('xb') as out:
            out.write(data)
    return context


def validate_rows(case, rows, require_success=True):
    names = {'vulndb_snapshot', 'vulndb_scan_before', 'vulndb_scan_after'}
    selected = [(i, r) for i, r in enumerate(rows) if r.get('kind') == 'fact' and r.get('name') in names]
    if 'vulndb_snapshot' not in case:
        require(not selected, 'undeclared snapshot facts')
        return None
    spec = validate_spec(case['vulndb_snapshot'])
    require(case.get('execution_profile') == 'local-isolated-vm-v1', 'snapshot is not explicitly local')
    require(len(selected) == 3 and [r['name'] for _, r in selected]
            == ['vulndb_snapshot', 'vulndb_scan_before', 'vulndb_scan_after'], 'snapshot facts missing/duplicated/reordered')
    require([r['step'] for _, r in selected] == ['env', 'govulncheck', 'govulncheck'], 'snapshot fact step differs')
    value = validate_fact(selected[0][1].get('value'), spec)
    require(all(r.get('value') == value for _, r in selected), 'scan database changed')
    starts = [(i, r) for i, r in enumerate(rows) if r.get('step') == 'govulncheck' and r.get('kind') == 'start']
    ends = [(i, r) for i, r in enumerate(rows) if r.get('step') == 'govulncheck' and r.get('kind') == 'end']
    require(len(starts) == len(ends) == 1, 'scan start/end missing or duplicated')
    require(selected[1][0] < starts[0][0] < ends[0][0] < selected[2][0], 'scan checks do not enclose the actual step')
    require(starts[0][1].get('command') == ['go', 'run', TOOL, '-db', value['uri'], './...'], 'actual scan command differs')
    require(type(ends[0][1].get('exit_code')) is int
            and type(ends[0][1].get('timed_out')) is bool, 'invalid actual scan exit')
    if require_success:
        require(ends[0][1]['exit_code'] == 0 and ends[0][1]['timed_out'] is False, 'actual scan did not succeed')
    return value


def _material_scan_prefix(case, rows):
    """Validate only observed scan records, including a genuine interrupted tail.

    This is not a quality verdict. validate_rows keeps requiring the complete
    before/start/end/after sequence and a successful scan for acceptance.
    """
    if 'vulndb_snapshot' not in case:
        return validate_rows(case, rows, require_success=False)
    spec = validate_spec(case['vulndb_snapshot'])
    require(case.get('execution_profile') == 'local-isolated-vm-v1', 'snapshot is not explicitly local')
    names = ('vulndb_snapshot', 'vulndb_scan_before', 'vulndb_scan_after')
    sequence = ('vulndb_snapshot', 'vulndb_scan_before', 'start', 'end', 'vulndb_scan_after')
    position, value = 0, None
    for row in rows:
        kind, step = row.get('kind'), row.get('step')
        if kind == 'fact' and row.get('name') in names:
            event = row['name']
            expected_step = 'env' if event == 'vulndb_snapshot' else 'govulncheck'
            require(step == expected_step, 'snapshot fact step differs')
        elif step == 'govulncheck':
            require(kind in ('start', 'line', 'end'), 'unknown scan material record')
            event = kind
        else:
            continue
        if event == 'line':
            require(position == 3 and isinstance(row.get('text'), str), 'scan output outside active scan')
            continue
        require(position < len(sequence) and event == sequence[position],
                'scan material is not an ordered prefix (missing/duplicate/reordered record)')
        if event == 'vulndb_snapshot':
            value = validate_fact(row.get('value'), spec)
        elif event in names:
            require(row.get('value') == value, 'scan database changed')
        elif event == 'start':
            require(row.get('command') == ['go', 'run', TOOL, '-db', value['uri'], './...'],
                    'actual scan command differs')
        else:
            require(type(row.get('exit_code')) is int and type(row.get('timed_out')) is bool,
                    'invalid actual scan exit')
        position += 1
    require(position >= 1, 'initial snapshot fact missing')
    return value


def verify_materials(run, case, rows, environment):
    # Preserve genuine failed or interrupted scans without inventing an after
    # check. Quality still requires every record through validate_rows.
    value = _material_scan_prefix(case, rows)
    if value is None:
        require('vulndb_snapshot' not in environment, 'undeclared environment snapshot')
        return
    require(environment.get('vulndb_snapshot') == value, 'environment snapshot differs')
    members = validate_archive(case['vulndb_snapshot'], regular_bytes(Path(run) / 'vulndb.zip', MAX_ZIP),
                               regular_bytes(Path(run) / 'vulndb-provenance.json', 2 * 1024 * 1024))
    require(sha(canonical(members)) == value['members_sha256'], 'archived members differ from scanned input')
