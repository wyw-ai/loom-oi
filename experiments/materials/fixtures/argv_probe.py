#!/usr/bin/env python3
"""Trusted input recorder for a separately isolated provider; not an OI implementation."""
import base64
import json
import os
import sys

def record(argv):
    return {'argv': argv, 'argv_base64': [base64.b64encode(os.fsencode(x)).decode('ascii') for x in argv],
            'cwd': os.getcwd(), 'fixture_instance': os.environ.get('LAB_FIXTURE_INSTANCE'),
            'scope': '输入记录夹具；instance标签需由外部部署证据核验，不能单独证明OI目标正确'}

if __name__ == '__main__':
    print(json.dumps(record(sys.argv[1:]), ensure_ascii=True))
