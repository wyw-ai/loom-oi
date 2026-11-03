#!/usr/bin/env python3
"""Validate preparation records; never execute experiments or approve readiness."""
import argparse
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
BUNDLES = {'R1': 'r1-remote-exec', 'R2': 'r2-app-combinations'}
FIELDS = ('oi_commit', 'consumer_version', 'provider_version', 'transport_and_auth_profile',
          'isolated_environment_record', 'resource_limits_and_cleanup',
          'fault_driver_and_recording', 'preregistered_order_repeats_and_budgets')


def validate(root=ROOT, ready=None):
    root = Path(root).resolve()
    errors, missing = [], []
    for bundle, folder in BUNDLES.items():
        base = root / 'experiments/materials' / folder
        manifest = json.loads((base / 'manifest.json').read_text(encoding='utf-8'))
        def local(name):
            p = (base / name).resolve()
            if not p.is_relative_to(root) or not p.is_file():
                raise ValueError('物料引用越界或缺失：' + name)
            return p
        if manifest.get('bundle') != bundle or manifest.get('evidence_level') != 'Preparation':
            errors.append(bundle + '：物料身份或证据层级错误')
        if manifest.get('status') != 'Blocked' or manifest.get('observations') != []:
            errors.append(bundle + '：计划不得伪装运行结果')
        cases = json.loads(local(manifest['cases_file']).read_text(encoding='utf-8'))
        inputs = json.loads(local(manifest['inputs_file']).read_text(encoding='utf-8'))
        if not cases or not inputs:
            errors.append(bundle + '：缺少测试用例或输入')
        for collection in [cases, inputs]:
            ids = [x.get('id') for x in collection]
            if any(not x for x in ids) or len(ids) != len(set(ids)):
                errors.append(bundle + '：缺少或重复条目ID')
        covered = set()
        for case in cases:
            if case.get('result') != 'NotRun':
                errors.append(bundle + '：用例计划不能填写已通过')
            for key in ['setup', 'steps', 'observations_required', 'pass_rule', 'falsifier', 'verification_ids']:
                if not case.get(key):
                    errors.append(bundle + '/' + case['id'] + '：缺少 ' + key)
            covered.update(case.get('verification_ids', []))
        if covered != set(manifest['verification_ids']):
            errors.append(bundle + '：验证覆盖不一致')
        if len(manifest.get('groups', [])) < 2 or not manifest.get('competing_explanation'):
            errors.append(bundle + '：缺少对照或竞争解释')
        if bundle == 'R1':
            local(manifest['fixture'])
            for item in inputs:
                if item.get('mode') == 'argv' and item.get('argv') != item.get('expected_argv'):
                    errors.append(bundle + '：参数预期必须逐项一致')
        else:
            expected = {'independent_endpoints', 'provider_handles', 'shared_conflict', 'replaceable_instance'}
            if {x.get('kind') for x in inputs} != expected:
                errors.append(bundle + '：缺少资源分类对照')
        template = json.loads((base / 'result-template.json').read_text(encoding='utf-8'))
        if template.get('status') != 'NotRun' or template.get('observations') != [] or template.get('raw_artifacts') != []:
            errors.append(bundle + '：结果模板含伪造观测')
        for field in FIELDS:
            value = manifest.get('execution_requirements', {}).get(field)
            if not isinstance(value, str) or not value.strip():
                if ready in (bundle, 'all'):
                    missing.append(bundle + ':' + field)
    return errors, missing


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ready', choices=['R1', 'R2', 'all'])
    args = parser.parse_args()
    try:
        errors, missing = validate(ready=args.ready)
    except (ValueError, OSError, KeyError, TypeError) as exc:
        errors, missing = [str(exc)], []
    if errors:
        print('\n'.join('失败：' + x for x in errors))
        return 1
    if missing:
        print('运行受阻，缺少：' + '、'.join(missing))
        return 2
    print('通过：仅物料/前置记录结构检查；未运行实验，实际隔离/权限/阶段及结果需独立核验。')
    return 0


if __name__ == '__main__':
    sys.exit(main())
