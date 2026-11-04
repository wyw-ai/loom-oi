"""Execution-budget regressions; no Go, product or real clock execution."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock
spec=importlib.util.spec_from_file_location('product_vector_budget',Path(__file__).parent/'product/run.py')
runner=importlib.util.module_from_spec(spec);spec.loader.exec_module(runner)

class VectorBudgetTests(unittest.TestCase):
    def cases(self):
        for name in ('G1-P1-R4','G4-P2-R3'):
            yield name,json.loads((runner.ROOT/'experiments/manifests'/(name+'.json')).read_text(encoding='utf-8'))['case']

    def test_frozen_defaults_and_g2_default_remain_exact(self):
        for name,case in self.cases():
            with self.subTest(manifest=name):
                before=copy.deepcopy(case);work=Path('work');table=runner.step_table(work,case)
                self.assertEqual(table['vectors'][0],['go','test','-json','-count=1','-timeout','40m','./tests/integration'])
                self.assertEqual(table['vectors'][2:],(2700,True))
                self.assertEqual(table['vectors'][1]['OI_IT_CLOCK'],'0')
                self.assertEqual('OI_IT_LOAD' in table['vectors'][1],case.get('load',True))
                g2=runner.step_table(work,dict(case,g2_behavior={}))
                self.assertEqual(g2['vectors'][0][-2:],['90m','./tests/integration'])
                self.assertEqual(g2['vectors'][2],5700);self.assertEqual(case,before)

    def test_explicit_90_changes_only_vectors_execution_allowance(self):
        for name,case in self.cases():
            with self.subTest(manifest=name):
                work=Path('fixed-work');old=runner.step_table(work,case)
                new=runner.step_table(work,dict(case,vectors_timeout_minutes=90))
                self.assertEqual(new['vectors'],(['go','test','-json','-count=1','-timeout','90m','./tests/integration'],old['vectors'][1],5700,True))
                self.assertEqual({k:v for k,v in old.items() if k!='vectors'},{k:v for k,v in new.items() if k!='vectors'})
                self.assertEqual(runner.step_table(work,dict(case,g2_behavior={})),runner.step_table(work,dict(case,g2_behavior={},vectors_timeout_minutes=90)))

    def test_explicit_invalid_types_or_values_are_not_defaults(self):
        case=next(self.cases())[1]
        for value in (True,False,None,'90',90.0,[],{},0,-1,40,89,91,1000000):
            with self.subTest(value=value),self.assertRaisesRegex(ValueError,'explicit integer 90'):
                runner.step_table(Path('.'),dict(case,vectors_timeout_minutes=value))

    def test_invalid_budget_fails_before_source_or_process_execution(self):
        case=next(self.cases())[1];case['vectors_timeout_minutes']=True
        manifest=dict(id='invalid',evidence_level='Implementation',case=case,hypothesis='h',alternatives=['a'],disproof='d',limitations=['l'],issues=[11])
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'invalid.json';path.write_text(json.dumps(manifest),encoding='utf-8')
            with mock.patch.object(sys,'argv',['run.py',str(path)]),mock.patch.object(runner,'git') as git,mock.patch.object(runner,'run_step') as step:
                with self.assertRaisesRegex(ValueError,'explicit integer 90'):runner.main()
                git.assert_not_called();step.assert_not_called()

if __name__=='__main__':unittest.main()
