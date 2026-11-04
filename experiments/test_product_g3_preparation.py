"""Auxiliary rehearsal output must neither poison nor replace formal evidence."""
import copy
import json
import unittest
from unittest.mock import patch

from product import analyze as main
from test_product import CASE, go, rows
from test_product_g3_behavior import PKG, control


class PreparationRoutingTests(unittest.TestCase):
    def summary(self, kind='G3-preparation', *, formal=True, forged_schema=False):
        evidence = control()
        case = copy.deepcopy(CASE)
        case['g3_behavior'] = dict(schema='g3-behavior-v1', package=PKG, cases=[evidence.case])
        case['g3_resources'] = {'routing_test': True}
        case['criteria']['behavior'] = ['G3-BEHAVIOR']
        diagnostic = dict(kind=kind, test=evidence.test, backend='sqlite', vector='TV-G3-05',
                          scenario='control_blackhole', stage='preparation', event='blackhole_phase',
                          applied_ns=100, phase_sampled_ns=90, license_remaining_ms=1794)
        if forged_schema:
            diagnostic['schema'] = 'g3-behavior-v1'
        output = []
        for record in [diagnostic, *(evidence.events if formal else []), *evidence.resources]:
            text = 'OBS ' + json.dumps(record) + '\n'
            # Exercise main's real D16 reconstruction and package/test routing.
            for piece in (text[:47], text[47:]):
                row = go('vectors', 'output', evidence.test, piece)
                row['event']['Package'] = PKG
                output.append(row)
        passed = go('vectors', 'pass', evidence.test)
        passed['event']['Package'] = PKG
        output.append(passed)
        # Resource mathematics has independent full-schema tests. Isolate only
        # that component; real behavior validation and main routing both run.
        resource = dict(result='pass', checks=evidence.checks, required_tests=[evidence.test])
        with patch.object(main, '_g3_resource_analysis', return_value=(resource, [])):
            return main.analyze(case, rows(extra=output), 1), diagnostic

    def test_auxiliary_preparation_is_retained_beside_valid_formal_evidence(self):
        summary, diagnostic = self.summary()
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        self.assertEqual(summary['observations']['G3-preparation'], [diagnostic])
        self.assertEqual(summary['g3_behavior']['result'], 'pass')

    def test_old_misclassified_fault_still_fails_strict_schema(self):
        summary, _ = self.summary('G3-fault')
        self.assertEqual(summary['verdict'], 'fail')
        self.assertTrue(any('identity/schema/time mismatch' in p for p in summary['problems']))

    def test_preparation_and_go_pass_cannot_replace_required_behavior(self):
        summary, _ = self.summary(formal=False)
        self.assertEqual(summary['verdict'], 'fail')
        self.assertEqual(summary['g3_behavior']['result'], 'fail')

    def test_preparation_name_cannot_hide_claimed_formal_schema(self):
        summary, _ = self.summary(forged_schema=True)
        self.assertEqual(summary['verdict'], 'fail')
        self.assertTrue(any('identity/schema/time mismatch' in p for p in summary['problems']))


if __name__ == '__main__':
    unittest.main()
