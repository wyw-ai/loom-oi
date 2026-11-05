"""Declared local scheduling must preserve full quality inputs and raw proof."""
import copy
import json
from pathlib import Path
import unittest

import test_product_local_adapter as adapter


HERE = Path(__file__).parent
fixture, main, runner = adapter.fixture, adapter.main, adapter.runner


def local_case():
    return json.loads((HERE / 'manifests/G1-P1-R5.json').read_bytes())['case']


def passing_input():
    case, rows, *_ = fixture()
    case['steps'].insert(1, 'race')
    case['go_steps'].append('race')
    case['quality_package_parallelism'] = 1
    table = runner.step_table(Path('work'), dict(local_case(), quality_package_parallelism=1))
    start = next(r for r in rows if r['kind'] == 'start' and r['step'] == 'unit')
    start.update(command=table['unit'][0], env=table['unit'][1])
    rows.extend([
        dict(step='race', kind='start', command=table['race'][0], env=table['race'][1]),
        dict(step='race', kind='go', event=dict(Action='pass', Package='p', Test='TestRace')),
        dict(step='race', kind='end', exit_code=0, timed_out=False),
    ])
    rows.insert(0, dict(step='env', kind='fact', name='go_flags', value=''))
    return case, rows


class QualityParallelismTests(unittest.TestCase):
    def analyze(self, case, rows):
        return adapter.LocalCollectorTests().analyze(case, rows)

    def test_only_unit_and_race_scheduling_changes(self):
        for name in ('G1-P1-R5', 'G4-P2-R4', 'G3-P2-R2', 'G2-P2'):
            case = json.loads((HERE / 'manifests' / (name + '.json')).read_bytes())['case']
            before = copy.deepcopy(case)
            old = runner.step_table(Path('work'), case)
            new = runner.step_table(Path('work'), dict(case, quality_package_parallelism=1))
            for step in old:
                with self.subTest(manifest=name, step=step):
                    if step in ('unit', 'race'):
                        self.assertEqual(new[step][0].count('-p=1'), 1)
                        self.assertEqual([x for x in new[step][0] if x != '-p=1'], old[step][0])
                        self.assertEqual(new[step][1:], old[step][1:])
                    else:
                        self.assertEqual(new[step], old[step])
            self.assertEqual(case, before)

    def test_invalid_declarations_cannot_fall_back(self):
        cases = []
        for value in (None, True, False, 1.0, '1', 0, 2, -1, [], {}):
            cases.append(dict(local_case(), quality_package_parallelism=value))
        foreign = dict(local_case(), quality_package_parallelism=1)
        del foreign['execution_profile'], foreign['local_execution']
        cases.append(foreign)
        missing = dict(local_case(), quality_package_parallelism=1)
        missing['steps'].remove('race')
        cases.append(missing)
        for case in cases:
            with self.subTest(value=case.get('quality_package_parallelism')):
                with self.assertRaisesRegex(ValueError, 'quality_package_parallelism'):
                    runner.step_table(Path('work'), case)
                with self.assertRaisesRegex(ValueError, 'quality_package_parallelism'):
                    main.analyze(case, [], 1)

    def test_full_raw_inputs_and_legacy_case_still_pass(self):
        case, rows = passing_input()
        summary = self.analyze(case, rows)
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        case, rows, *_ = fixture()
        self.assertEqual(self.analyze(case, rows)['verdict'], 'pass')

    def test_raw_filters_missing_flags_and_wrong_environment_fail(self):
        mutations = ('missing_start', 'duplicate_start', 'no_p', 'different_p', 'duplicate_p',
                     'filter', 'skip', 'missing_package', 'missing_count', 'missing_race',
                     'missing_coverage', 'empty_profile', 'extra_environment', 'missing_fact',
                     'duplicate_fact', 'foreign_fact', 'nonempty_flags', 'null_flags')
        for step in ('unit', 'race'):
            for change in mutations:
                if change == 'missing_race' and step != 'race' or change in ('missing_coverage', 'empty_profile') and step != 'unit':
                    continue
                case, rows = passing_input()
                start = next(r for r in rows if r['kind'] == 'start' and r['step'] == step)
                command = start['command']
                fact = rows[0]
                if change == 'missing_start': rows.remove(start)
                elif change == 'duplicate_start': rows.append(copy.deepcopy(start))
                elif change == 'no_p': command.remove('-p=1')
                elif change == 'different_p': command[3] = '-p=2'
                elif change == 'duplicate_p': command.insert(3, '-p=2')
                elif change == 'filter': command.insert(3, '-run=TestOnlyOne')
                elif change == 'skip': command.insert(3, '-skip=TestImportant')
                elif change == 'missing_package': command.remove('./cmd/...')
                elif change == 'missing_count': command.remove('-count=1')
                elif change == 'missing_race': command.remove('-race')
                elif change == 'missing_coverage': command.remove('-covermode=atomic')
                elif change == 'empty_profile': command[6] = '-coverprofile='
                elif change == 'extra_environment': start['env']['GOFLAGS'] = '-run=TestOnlyOne'
                elif change == 'missing_fact': rows.remove(fact)
                elif change == 'duplicate_fact': rows.append(copy.deepcopy(fact))
                elif change == 'foreign_fact': fact['step'] = 'vectors'
                elif change == 'nonempty_flags': fact['value'] = '-run=TestOnlyOne'
                elif change == 'null_flags': fact['value'] = None
                with self.subTest(step=step, change=change):
                    summary = self.analyze(case, rows)
                    self.assertEqual(summary['verdict'], 'fail')
                    self.assertTrue(any('quality' in p for p in summary['problems']), summary['problems'])


if __name__ == '__main__':
    unittest.main()
