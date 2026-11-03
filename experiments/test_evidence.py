import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from verify import verify
from analyze import analyze

ROOT=Path(__file__).resolve().parent
class IntegrityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source=sorted((ROOT/"runs").glob("LAB-001-*"))[-1]
    def copy(self, path):
        shutil.copytree(self.source,path)
    def rehash(self,path):
        (path/"SHA256SUMS").write_text("".join(hashlib.sha256(p.read_bytes()).hexdigest()+"  "+p.name+"\n"
            for p in sorted(path.iterdir()) if p.is_file() and p.name!="SHA256SUMS"))
    def test_original(self):
        self.assertEqual(verify(self.source,True)["integrity"],"verified")
    def test_raw_corruption_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            p=Path(t)/"copy";self.copy(p)
            with (p/"raw.jsonl").open("a") as f:f.write("{}\n")
            with self.assertRaises(AssertionError):verify(p)
    def test_forged_summary_even_with_updated_hashes_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            p=Path(t)/"copy";self.copy(p)
            s=json.loads((p/"summary.json").read_text(encoding="utf-8"));s["tcp_success_without_timely_application_success"]=0
            (p/"summary.json").write_text(json.dumps(s));self.rehash(p)
            with self.assertRaises(AssertionError):verify(p)
    def test_duplicate_trial_rejected(self):
        m=json.loads((self.source/"manifest.json").read_text(encoding="utf-8"))
        rows=[json.loads(x) for x in (self.source/"raw.jsonl").read_text(encoding="utf-8").splitlines()]
        for x in rows:
            if x["kind"]=="probe" and x["repeat"]==1:x["repeat"]=0
        self.assertEqual(analyze(m["case"],rows,m["repeats"])["status"],"inconclusive")
    def test_attempt_count_inconsistent_rejected(self):
        p=sorted((ROOT/"runs").glob("LAB-002-*"))[-1]
        m=json.loads((p/"manifest.json").read_text(encoding="utf-8"))
        rows=[json.loads(x) for x in (p/"raw.jsonl").read_text(encoding="utf-8").splitlines()]
        rows[0]["attempts"]=99
        self.assertEqual(analyze(m["case"],rows,m["repeats"])["status"],"inconclusive")
    def test_failed_run_cannot_claim_observed(self):
        with tempfile.TemporaryDirectory() as t:
            p=Path(t)/"copy";self.copy(p)
            s=json.loads((p/"execution.json").read_text(encoding="utf-8"));s["exit_code"]=9
            (p/"execution.json").write_text(json.dumps(s));self.rehash(p)
            with self.assertRaises(AssertionError):verify(p)
if __name__=="__main__":unittest.main()
