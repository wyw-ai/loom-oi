"""Fixed local WebSocket patch evidence. No subprocess, network or mutable cache."""
import copy
import hashlib
import json
from pathlib import Path, PurePosixPath, PureWindowsPath
import re
from urllib.parse import urlsplit

KEY = 'dependency_quality'
MODULE = 'github.com/coder/websocket'
VERSION = 'v1.8.15'
DIRECTORY = 'third_party/websocket'
ROOT_MODULE = 'github.com/wyw-ai/loom-oi'
STATICCHECK = 'honnef.co/go/tools/cmd/staticcheck@v0.8.2'
GOVULNCHECK = 'golang.org/x/vuln/cmd/govulncheck@v1.9.0'
TEST_ENVIRONMENT = {'CGO_ENABLED': '1', 'GODEBUG': 'netdns=cgo+1',
                    'RES_OPTIONS': 'attempts:1 timeout:1'}
CGO_WITNESS = 'go package net: GODEBUG setting forcing use of the cgo resolver'
STEPS = ('dependency-unit', 'dependency-race', 'dependency-vet',
         'dependency-staticcheck', 'dependency-vuln')
PROFILE_FACTS = {'root_unit_profile': ('unit', 'unit-coverage.out'),
                 'dependency_unit_profile': ('dependency-unit', 'dependency-coverage.out')}
SOURCE_FACTS = ('dependency_source', 'dependency_source_after')
DB_FACTS = ('dependency_vulndb_before', 'dependency_vulndb_after')
HASH = re.compile(r'[0-9a-f]{64}\Z')
# Fixed bytes and inventory from the separately reviewed v1.8.15 import.
FIXED_FILES = {'.github/dependabot.yml': '6a22d413bfbf68cc1fca0162fb7dfd6584395b04ca68cbf9906dec185002665d',
 '.github/workflows/ci.yml': '6ea95b1c34561f188e0eec9084b34dea54a1d21f13f2684672c8c3253d5c72a0',
 '.github/workflows/daily.yml': '9ef3959aa1d7f8f22c8f0b34a100a07df3d5220d98463484b8be36100765e539',
 '.github/workflows/static.yml': '7b002a3c91f7d311da51b7ad126661adcb82ee3ad4311ea4d010016d0a05c020',
 '.gitignore': 'ed6cfbce214958ad4dfe1f387ebb95caa900597410363397763d5179b1736e6c',
 'AGENTS.md': '4f18e644c5ea375e6df74def90e0b0a5c3d53af651363dee21cc92c78263e82d',
 'LICENSE.txt': 'cc0975a5f6305145bdd7b41ce9479632fdac3870e6ac4281f28017f18c767c4e',
 'Makefile': '258058f087ba86c2b191d48c7fff25e6c8c3d5e9320c15f0a2d614fd7052d157',
 'PATCHES.md': '4a7a5c1e1ea3aae97d706ebfc216a4e1e1878e37641bfe04c7b5b94f54eec610',
 'README.md': '5fd5393d4d3095741658019bf7e86ebbe4a5272cfcffeae6c7e9032e3fc4c97f',
 'UPSTREAM.json': '6d14828a8176ea687cf50b4c07a77058d1d8ca9d7efdc6558e8cf540bfc19a4c',
 'UPSTREAM.md': '6c79c78d7b17c037fb8d0047a9a51e447385bc97954b5bd33868b6496f4ab48e',
 'accept.go': '3578bc97e032fdee9ff5fbcecf4e02f2f9d0928184b92466702858de76d8b6af',
 'accept_test.go': '163a3dd06a860546d6491777c5f271529c38b3518b3d60c379e8f2709a98516a',
 'autobahn_test.go': '6cb87e08b223f10a9be5542051e65c0deae37705a5d4588d93e481c8969fcbb6',
 'ci/bench.sh': '44e086a6a16afb88520ebd9de1e60318f272513943e606cf6b3db44cb0d2f069',
 'ci/fmt.sh': 'ed5c17772f467421f83aff9602bc2f024cd8f2f3a5c440126ebf05addfc16878',
 'ci/lint.sh': '827966c0b23a1137816dae50ddeefaa04d2d9bc8871224e449f7af9efa7acd7f',
 'ci/out/.gitignore': 'cdbcae15105d6b781e620813c79c7e868740d4e9cc53ce6f5fcbbc12387adf4b',
 'ci/test.sh': '2109853d69a1899441911849058bcdb168c016c10eae3c225ea196325bad192b',
 'close.go': '2cee606147172e5ac31731ae1846bdceae2d1e8a25dc9990cb46de75dd41b948',
 'close_test.go': 'a4b01be58aec9aa47670393ed51e1fa503ae57f6f48ce948a3a41f08bdd413cb',
 'compress.go': 'dfca27c32fd9109279ea248193c2203546a28dbbc647a5c2b4c207f8ae08bf54',
 'compress_test.go': '451b7f97a8684f16bbc83445e71504531009887a6bf7c3f876e4cb39c267b3a9',
 'conn.go': 'f5aabddca411a9ccbf2938f976d9152f373e52bb1de138c305110807e1c7b0e4',
 'conn_test.go': '708b70d91ccee0aeb4398f9b063bdf9a98200a4bd26ace59a4fae1f710eabb52',
 'control_write.go': '5f0d79a30668ca30ab692383fb06c0366c43f6f3f636b0469b81b61c0933a116',
 'control_write_js.go': '70e9ccea0d3e0b7582a0cefd735e97675b414400f32c06ebad5b5a8a8069bca1',
 'control_write_js_test.go': 'ae11d73c6775d99ad79a65a9d1429597303d93e584b9e8d967ce91d13e16547b',
 'control_write_notjs.go': '9e5f7aeabecb01c97e4ab396b0868decdaaeaf49f2c27817ada814c300523d83',
 'control_write_test.go': 'cdac46cfb9731db7b03f8c4b809f5944024301c7c75e106579bedff63b60b8c7',
 'dial.go': '613ea3bb251a13de7c0f47839947a7e5f867acb9555bad1499d1a514b9a772d7',
 'dial_test.go': '10f4cbf5f42d802089708fc785c724f12ebb4816cd1e86aab99f790d8d3822e0',
 'doc.go': '58ee6f20e5baed220eaae3999651d97dc5aa97499ab69b81ded421af94b70e90',
 'errors.go': '7664ab90db0687f0834fd25561e950cc0ba4ae3c4d616fde6ae186bb7bcef230',
 'example_test.go': '7f9b32c01472b9d1c197ef74dee2d473f018702cee66358aa1c502dfe3bdb2fa',
 'export_test.go': '81b9cad42d23ecc5ded734a97f28639638a632e143cc8ffb87d42dc207fde191',
 'frame.go': 'ee8a805dd226f4e3b461fc5699c58a00e5a45cc35d0e56542ca96773c2d61d8a',
 'frame_test.go': '43afb9948c8cd264efb993f4f243c6dd206348aa2e2d9e6df610cecdc5c8a2df',
 'go.mod': '0cab443e389cef98e63828bd7ce82224bf192732a18795581314766eb14ea275',
 'go.sum': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 'hijack.go': '971691793739eae342309998ed96f22194c37439a868c9d48fd434245f8ee8b2',
 'hijack_go120_test.go': 'a5e6caddac8c35af2c0c1d065324578e347d3134c27f58443e93c347eb574614',
 'internal/bpool/bpool.go': '0806d6ed46c48d3b2b3af68138e4f5d80f466eb8e9ce85d9786f7bc86a849c7f',
 'internal/errd/wrap.go': '0e70ac99d7bb75692509b1eeb4c921c281f18c24a3bdcc86e7e57dcbfc3fdab5',
 'internal/test/assert/assert.go': '4001c6955ea8d4840ca69f7c7af182cd21208643b3d15c1eb6f27249d227807b',
 'internal/test/doc.go': '8b14b86cc00a403259fd1edf2047077937cc9a4b4a58f659d22f5738fda677a3',
 'internal/test/wstest/echo.go': '813b0198c42e26eb5c2b2da23c2daeffa8e477e5eed20209a6dc250f0dc0273f',
 'internal/test/wstest/pipe.go': 'fe21d4201d21e73c9845ae0f0a935be6da31381566415b1faaaca5e0f2ad26d9',
 'internal/test/xrand/xrand.go': 'c86ba361a6ec5267d36941d2bff476accc9253eeb7c236954e0f1403be31afad',
 'internal/util/util.go': '384c5982c8854cd041a930586580bff71478be939a4c9c0f6d4870263de199bc',
 'internal/wsjs/wsjs_js.go': 'd477d40a1e50a66e59224009343740b1e8f5d82648d368788e29b98ff68f0de1',
 'internal/xsync/go.go': 'cfb8a5bfe684e9a6ccdc0f05f0b307c2db05069e319cf562843730c12e5a6358',
 'internal/xsync/go_test.go': 'd3b7768607e4f45ad5b3a5d149b18d87e66c20f40acc71da2b70de15cf500ef1',
 'main_test.go': '01739cd54a52cd00460a40b68549168de272b52d095a187936f9aedc9b69963f',
 'mask.go': '4b910cc40e42d5268682ce14395027f5936f3b5b4ad3f8b0c511b87f176702b4',
 'mask_amd64.s': '3ff45361b7376aacf750c3d75aee213ab2a39c6ba96f4e298ebe83ea64696733',
 'mask_arm64.s': '8a9703306ce9ced3056b9a41e63d10c4c266731259c51b8a57f94e8e1d043e50',
 'mask_asm.go': '1858a7bb3ffd4b643fb1e683a6b1f30f28ac904f970611c4e3f865df6e3a4924',
 'mask_asm_test.go': '60081e6d9b4ecd2f15b2a0ea20e88ff9e5d1f8f10a65867092f04003a2512c96',
 'mask_go.go': '90b7f1a3969fe810d1e598ce4a5f909c392855f2eb95862d7e3e4a7e5c57caee',
 'mask_test.go': 'b795cb760f3f6b28847a2ba0d81fd552e50787f0103b284b0f9bfba547356218',
 'netconn.go': '3ffff33c5c0caeb5b5be8b272edc20649f834514bf99d256e4a5dc163f4f06c3',
 'netconn_js.go': '3d24e9796a6d87a49917adf4b08f1e7cdb3617f2ce6f522addc14676e32be520',
 'netconn_notjs.go': '25053a79f57498a87c6f610214b0ee71d225d7f3af1d2719607eeba5a5266ec7',
 'read.go': 'bc8fe2d1003a6acdd078da05e490edc5f0b5a5df69b28ae6f5004c7c526cfeb6',
 'stringer.go': '7c1120b8b7f24a5a9ef9e779add7c482dbf2d48908de7501227fcb405ec685fe',
 'write.go': 'c867336502e341f18d5c8a8c484da1115dd84659f40ee8aed64fcba1d412aa21',
 'ws_js.go': 'afde6fe4b98c1d28a9cb2f0e27203e8d8fa2c14cc779947698040bf9bc7decb7',
 'ws_js_test.go': '57bef885638ac5f4a8bab0d5c752efbc39e2991533d6d7e1b8253ffc269c90ba',
 'wsjson/wsjson.go': 'b16795bc9f03144a3eaca0cf0c76ea466367f9613ff5cb8b22808c79429ad0f9',
 'wsjson/wsjson_test.go': '178d17b08452b71776d8d09032bb3c7428c80ca8fbc7cfe4715063cbd3a66b00'}

PACKAGES = ['github.com/coder/websocket', 'github.com/coder/websocket/internal/bpool', 'github.com/coder/websocket/internal/errd', 'github.com/coder/websocket/internal/test', 'github.com/coder/websocket/internal/test/assert', 'github.com/coder/websocket/internal/test/wstest', 'github.com/coder/websocket/internal/test/xrand', 'github.com/coder/websocket/internal/util', 'github.com/coder/websocket/internal/xsync', 'github.com/coder/websocket/wsjson']

REQUIRED_ROOTS = [{'package': 'github.com/coder/websocket', 'test': 'TestAccept'},
 {'package': 'github.com/coder/websocket', 'test': 'TestAutobahn'},
 {'package': 'github.com/coder/websocket', 'test': 'TestBadDials'},
 {'package': 'github.com/coder/websocket', 'test': 'TestCloseError'},
 {'package': 'github.com/coder/websocket', 'test': 'TestCloseStatus'},
 {'package': 'github.com/coder/websocket', 'test': 'TestCompressionDictionaryPreserved'},
 {'package': 'github.com/coder/websocket', 'test': 'TestConcurrentClosePing'},
 {'package': 'github.com/coder/websocket', 'test': 'TestConn'},
 {'package': 'github.com/coder/websocket', 'test': 'TestConnClosePropagation'},
 {'package': 'github.com/coder/websocket', 'test': 'TestDialRedirect'},
 {'package': 'github.com/coder/websocket', 'test': 'TestDialViaProxy'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlCancellation'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlKeepsCallerDeadline'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlWriteError'},
 {'package': 'github.com/coder/websocket', 'test': 'TestHeader'},
 {'package': 'github.com/coder/websocket', 'test': 'TestMask'},
 {'package': 'github.com/coder/websocket', 'test': 'TestMaskASM'},
 {'package': 'github.com/coder/websocket', 'test': 'TestWasm'},
 {'package': 'github.com/coder/websocket', 'test': 'TestWriteSingleFrameCompressed'},
 {'package': 'github.com/coder/websocket', 'test': 'TestWriteThenWriterContextTakeover'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_authenticateOrigin'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_hijackerHTTPResponseControllerCompatibility'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_mask'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_parseClosePayload'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_selectDeflate'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_selectSubprotocol'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_slidingWindow'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_validWireCloseCode'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_verifyClientHandshake'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_verifyHostOverride'},
 {'package': 'github.com/coder/websocket', 'test': 'Test_verifyServerHandshake'},
 {'package': 'github.com/coder/websocket/internal/xsync', 'test': 'TestGoRecover'}]

OWNED_TESTS = [{'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlCancellation'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPing_control_write_cancel'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPing_control_write_close'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPing_write_lock_cancel'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlCancellation/opPing_write_lock_close'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPong_control_write_cancel'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPong_control_write_close'},
 {'package': 'github.com/coder/websocket',
  'test': 'TestExplicitControlCancellation/opPong_write_lock_cancel'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlCancellation/opPong_write_lock_close'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPing_0'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPing_125'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPing_8'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPong_0'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPong_125'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/client_opPong_8'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPing_0'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPing_125'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPing_8'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPong_0'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPong_125'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlFrames/server_opPong_8'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlKeepsCallerDeadline'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/binary'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/close'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/continuation'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/ping_126'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/pong_126'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/reserved'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlValidation/text'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlWriteError'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlWriteError/opPing'},
 {'package': 'github.com/coder/websocket', 'test': 'TestExplicitControlWriteError/opPong'}]

NATIVE_BLOCKS = {'github.com/coder/websocket/control_write.go:13.2,14.1': 1,
 'github.com/coder/websocket/control_write.go:21.2,22.1': 1,
 'github.com/coder/websocket/control_write.go:25.2,25.34': 1,
 'github.com/coder/websocket/control_write.go:26.3,27.1': 1,
 'github.com/coder/websocket/control_write.go:28.2,28.24': 1,
 'github.com/coder/websocket/control_write.go:29.3,30.1': 1,
 'github.com/coder/websocket/control_write.go:31.2,31.48': 1,
 'github.com/coder/websocket/control_write_notjs.go:11.2,12.16': 2,
 'github.com/coder/websocket/control_write_notjs.go:13.3,14.1': 1,
 'github.com/coder/websocket/control_write_notjs.go:15.2,15.12': 1}

ALLOWED_SKIPS = {(MODULE, "TestAutobahn"), (MODULE, "TestWasm")}

NO_TEST_PACKAGES = [MODULE+'/'+suffix for suffix in (
    'internal/bpool', 'internal/errd', 'internal/test', 'internal/test/assert',
    'internal/test/wstest', 'internal/test/xrand', 'internal/util')]
NO_TEST_SKIPS = {'dependency-unit': [MODULE+'/internal/test'],
                 'dependency-race': NO_TEST_PACKAGES}



def require(condition, message):
    if not condition:
        raise ValueError('dependency quality: ' + message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False)


def expected_spec():
    return copy.deepcopy(dict(
        schema='websocket-control-v1', module=MODULE, version=VERSION, directory=DIRECTORY,
        steps=list(STEPS), packages=PACKAGES, no_test_packages=NO_TEST_PACKAGES,
        no_test_package_skips=NO_TEST_SKIPS, test_environment=TEST_ENVIRONMENT,
        allowed_test_skips=[dict(package=MODULE, test='TestAutobahn'),
                            dict(package=MODULE, test='TestWasm')],
        required_roots=REQUIRED_ROOTS, required_owned_tests=OWNED_TESTS,
        coverage=dict(root='cmd/internal', owned_statements=12, native_statements=11,
                      js_uncovered_statements=1, minimum_percent=95),
        upstream_manifest_sha256=FIXED_FILES['UPSTREAM.json'],
        files_sha256=FIXED_FILES,
        tools=dict(staticcheck=STATICCHECK, govulncheck=GOVULNCHECK)))


def validate_case(case):
    require(isinstance(case, dict), 'case must be an object')
    if KEY not in case:
        require(not any(x in case.get('steps', []) for x in STEPS), 'undeclared dependency step')
        return False
    try:
        declared = canonical(case[KEY])
    except (TypeError, ValueError) as error:
        raise ValueError('dependency quality: malformed preregistration') from error
    require(declared == canonical(expected_spec()), 'unknown or incomplete preregistration')
    for name in ('steps', 'go_steps'):
        value = case.get(name)
        require(isinstance(value, list) and all(isinstance(x, str) for x in value)
                and len(value) == len(set(value)), name+' missing or duplicated')
    require(set(STEPS) | {'unit', 'race'} <= set(case['steps']), 'required steps missing')
    require({'unit', 'race', 'dependency-unit', 'dependency-race'} <= set(case['go_steps']),
            'required Go steps missing')
    require(not set(STEPS[2:]) & set(case['go_steps']), 'non-Go dependency step declared as Go')
    require(type(case.get('quality_package_parallelism')) is int
            and case['quality_package_parallelism'] == 1, 'explicit package parallelism must be 1')
    require(type(case.get('coverage_min_pct')) in (int, float) and case['coverage_min_pct'] == 95,
            'coverage threshold differs')
    require(case.get('execution_profile') == 'local-isolated-vm-v1'
            and isinstance(case.get('vulndb_snapshot'), dict), 'registered local database required')
    return True


def commands(work, case, db_uri):
    if not validate_case(case):
        return {}
    prefix = ['go', '-C', DIRECTORY]
    query = ['go', 'run', GOVULNCHECK]
    if db_uri is not None:
        _db_uri(db_uri)
        query += ['-db', db_uri]
    query += ['-json', '-mode=query', MODULE+'@'+VERSION]
    return {
        'dependency-unit': (prefix+['test', '-json', '-p=1', '-count=1', '-covermode=atomic',
                                   '-coverprofile='+str(work/'dependency.out'), './...'],
                            dict(TEST_ENVIRONMENT), 300, True),
        'dependency-race': (prefix+['test', '-json', '-p=1', '-race', '-count=1', './...'],
                            dict(TEST_ENVIRONMENT), 600, True),
        'dependency-vet': (prefix+['vet', './...'], {}, 120, False),
        'dependency-staticcheck': (prefix+['run', STATICCHECK, './...'], {}, 300, False),
        'dependency-vuln': (query, {}, 120, False),
    }


def _root_mod(text):
    require(isinstance(text, str), 'root module text absent')
    # Exact module/version and a single local replacement, including block syntax.
    lines = [x.split('//', 1)[0].strip() for x in text.splitlines()]
    require(lines.count('module '+ROOT_MODULE) == 1, 'wrong root module')
    mentions = [x for x in lines if MODULE in x]
    require(len(mentions) == 2 and sum(x == MODULE+' '+VERSION for x in mentions) == 1
            and sum(x in ('replace '+MODULE+' => ./'+DIRECTORY, MODULE+' => ./'+DIRECTORY)
                    for x in mentions) == 1, 'upstream requirement/local replace differs')


def source_fact(root):
    root = Path(root)
    directory = root/DIRECTORY
    require(directory.is_dir() and not directory.is_symlink(), 'dependency directory unavailable')
    actual = {}
    for path in directory.rglob('*'):
        require(not path.is_symlink(), 'symlink in dependency source')
        if path.is_file():
            actual[path.relative_to(directory).as_posix()] = sha(path.read_bytes())
    require(actual == FIXED_FILES, 'dependency source differs from fixed 72-file import')
    text = (root/'go.mod').read_bytes().decode('utf-8')
    _root_mod(text)
    files = {DIRECTORY+'/'+name: value for name, value in actual.items()}
    files['go.mod'] = sha(text.encode('utf-8'))
    return dict(schema='websocket-dependency-source-v1', files=files, root_go_mod=text)


def _source(value):
    require(isinstance(value, dict) and set(value) == {'schema', 'files', 'root_go_mod'}
            and value['schema'] == 'websocket-dependency-source-v1', 'invalid dependency source fact')
    _root_mod(value['root_go_mod'])
    expected = {DIRECTORY+'/'+name: digest for name, digest in FIXED_FILES.items()}
    expected['go.mod'] = sha(value['root_go_mod'].encode('utf-8'))
    require(value['files'] == expected, 'source fact hashes differ')
    return value


def _profile(text, prefix):
    require(isinstance(text, str) and text.endswith('\n'), 'profile missing or unterminated')
    lines = text.splitlines()
    require(lines and lines[0] == 'mode: atomic' and len(lines) > 1, 'profile not atomic or empty')
    blocks = {}
    pattern = re.compile(r'([^\s:]+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)\Z')
    for line in lines[1:]:
        m = pattern.fullmatch(line)
        require(m is not None, 'invalid profile block')
        file = m[1]
        require(file.startswith(prefix) and '..' not in file.split('/') and '\\' not in file,
                'profile file outside declared module')
        sl, sc, el, ec, statements, count = map(int, m.groups()[1:])
        require(min(sl, sc, el, ec) > 0 and (sl, sc) <= (el, ec), 'invalid coverage coordinates')
        key = line.rsplit(' ', 2)[0]
        require(key not in blocks, 'duplicate coverage block')
        blocks[key] = (statements, count)
    return blocks


def _counts(blocks):
    return dict(covered=sum(n for n, count in blocks.values() if count > 0),
                total=sum(n for n, _ in blocks.values()))


def _percentage(counts):
    require(counts['total'] > 0, 'empty statement denominator')
    return dict(counts, pct=100*counts['covered']/counts['total'])


def summarize_coverage(root_text, dependency_text):
    root = _profile(root_text, ROOT_MODULE+'/')
    require(all(key.startswith((ROOT_MODULE+'/cmd/', ROOT_MODULE+'/internal/')) for key in root),
            'root coverage includes a different scope')
    dependency = _profile(dependency_text, MODULE+'/')
    for key in dependency:
        rel = key.split(':', 1)[0][len(MODULE)+1:]
        require(rel in FIXED_FILES and rel.endswith('.go') and not rel.endswith('_test.go'),
                'unknown dependency profile source')
        require(rel != 'control_write_js.go', 'native profile claims JS unsupported branch')
    owned = {key: value for key, value in dependency.items()
             if key.split(':', 1)[0] in (MODULE+'/control_write.go', MODULE+'/control_write_notjs.go')}
    require({key: value[0] for key, value in owned.items()} == NATIVE_BLOCKS,
            'native owned coverage blocks missing, added or changed')
    rc = _counts(root)
    oc = _counts(owned)
    require(oc['total'] == 11, 'native denominator differs')
    oc['total'] += 1  # No automatic JS coverage; owned unsupported path stays uncovered.
    combined = dict(covered=rc['covered']+oc['covered'], total=rc['total']+oc['total'])
    return dict(root=_percentage(rc), owned=_percentage(oc), combined=_percentage(combined),
                js_uncovered_statements=1, meets_threshold=combined['covered']*20 >= combined['total']*19)


def _pairs(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, 'duplicate JSON key')
        value[key] = item
    return value


def _db_uri(uri):
    require(isinstance(uri, str), 'database URI absent')
    parsed = urlsplit(uri)
    require(uri.startswith('file:///') and parsed.scheme == 'file' and not parsed.netloc
            and not parsed.query and not parsed.fragment, 'database must be the local fixed snapshot')


def validate_query(text, expected_db=None):
    require(isinstance(text, str) and text.endswith('\n'), 'query stream missing or truncated')
    decoder = json.JSONDecoder(object_pairs_hook=_pairs, parse_constant=lambda x: require(False, 'nonfinite JSON'))
    values, position = [], 0
    while position < len(text):
        while position < len(text) and text[position].isspace():
            position += 1
        if position == len(text):
            break
        try:
            value, position = decoder.raw_decode(text, position)
        except (ValueError, TypeError) as e:
            raise ValueError('dependency quality: invalid query JSON stream') from e
        require(isinstance(value, dict) and len(value) == 1, 'invalid query message')
        require('osv' not in value and 'finding' not in value, 'upstream vulnerability requires analysis')
        values.append(value)
    require(len(values) == 2 and set(values[0]) == {'config'} and set(values[1]) == {'progress'},
            'query must contain one configuration and one exact query')
    config, progress = values[0]['config'], values[1]['progress']
    require(isinstance(config, dict) and set(config) <= {'protocol_version', 'scanner_name', 'scanner_version',
            'db', 'db_last_modified', 'go_version', 'scan_level', 'scan_mode'}, 'unknown query configuration')
    require(config.get('protocol_version') == 'v1.0.0' and config.get('scanner_name') == 'govulncheck'
            and config.get('scanner_version') == 'v1.9.0' and config.get('scan_mode') == 'query',
            'query tool/protocol/mode differs')
    # CI's default upstream scan is explicitly the official service. Formal
    # runs always pass the registered, immutable local snapshot URI below.
    if expected_db is None:
        require(config.get('db') == 'https://vuln.go.dev', 'CI query must use the fixed official database')
    else:
        _db_uri(expected_db)
        require(config.get('db') == expected_db, 'query used a different local database')
    require(isinstance(progress, dict) and set(progress) <= {'time', 'message'}
            and progress.get('message') == 'Looking up vulnerabilities in '+MODULE+' at '+VERSION+'...',
            'upstream query differs')
    return dict(module=MODULE, version=VERSION, tool=GOVULNCHECK, db=config['db'], osv_count=0)


def _fact(rows, name, step):
    selected = [(i, r) for i, r in enumerate(rows) if r.get('kind') == 'fact' and r.get('name') == name]
    require(len(selected) == 1 and selected[0][1].get('step') == step, 'missing/duplicate/misplaced '+name)
    return selected[0][0], selected[0][1].get('value')


def _step(rows, name):
    selected = [(i, r) for i, r in enumerate(rows) if r.get('step') == name]
    starts = [(i, r) for i, r in selected if r.get('kind') == 'start']
    ends = [(i, r) for i, r in selected if r.get('kind') == 'end']
    require(len(starts) == len(ends) == 1 and starts[0][0] < ends[0][0], 'step start/end differs: '+name)
    si, start = starts[0]
    ei, end = ends[0]
    require(type(end.get('exit_code')) is int and end['exit_code'] == 0
            and end.get('timed_out') is False, 'step failed/timed out: '+name)
    require(all(si < i < ei for i, r in selected if r.get('kind') in ('go', 'line')), 'output outside step: '+name)
    return si, ei, start


def _go_tests(rows, step):
    events = [r.get('event') for r in rows if r.get('step') == step and r.get('kind') == 'go']
    require(events and not any(r.get('step') == step and r.get('kind') == 'line' for r in rows),
            'missing or malformed Go JSON: '+step)
    packages, opened, terminal, tests, started_tests, outputs = {}, set(), {}, {}, set(), {}
    resolver_pending, resolver_seen = {}, False
    for event in events:
        require(isinstance(event, dict), 'Go event is not an object')
        require('WARNING: DATA RACE' not in str(event.get('Output', '')), 'race diagnostic in dependency output')
        pkg, test, action = event.get('Package'), event.get('Test'), event.get('Action')
        require(isinstance(pkg, str) and isinstance(action, str) and pkg in PACKAGES
                and action in {'start', 'run', 'pause', 'cont', 'output', 'pass', 'fail', 'skip'},
                'unknown dependency package/action')
        require('Test' not in event or isinstance(test, str) and bool(test), 'invalid Go test identity')
        if action == 'output':
            require(isinstance(event.get('Output'), str), 'invalid Go output')
            if pkg == MODULE:
                # test2json may split Output. Only a complete line from the same
                # package/test can witness this test process's resolver choice.
                identity = (pkg, test)
                parts = (resolver_pending.get(identity, '')+event['Output']).split('\n')
                resolver_seen |= CGO_WITNESS in parts[:-1]
                resolver_pending[identity] = parts[-1]
        if not test:
            if action == 'start':
                require(pkg not in opened, 'duplicate package start')
                opened.add(pkg)
                packages[pkg] = action
                continue
            require(pkg in opened and pkg not in terminal, 'package output outside its lifetime')
            if action == 'output':
                require(isinstance(event.get('Output'), str), 'invalid Go output')
                outputs[pkg] = outputs.get(pkg, '')+event['Output']
            elif action in ('pass', 'fail', 'skip'):
                terminal[pkg] = action
            else:
                require(False, 'invalid package action')
            continue
        require(isinstance(test, str) and pkg in opened and pkg not in terminal, 'test outside package lifetime')
        key = (pkg, test)
        if action == 'run':
            require(key not in started_tests, 'duplicate test start')
            started_tests.add(key)
        elif action in ('pass', 'skip', 'fail'):
            require(key in started_tests and key not in tests, 'missing/duplicate test execution')
            require(action == 'pass' or action == 'skip' and key in ALLOWED_SKIPS, 'dependency test failed/skipped')
            tests[key] = action
        elif action in ('pause', 'cont'):
            require(key in started_tests and key not in tests, 'test scheduling outside lifetime')
        else:
            # test2json emits the textual PASS line before the terminal event.
            require(action == 'output' and key in started_tests and isinstance(event.get('Output'), str),
                    'invalid named test output')
    require(set(terminal) == set(PACKAGES), 'missing package termination')
    for pkg, action in terminal.items():
        if pkg in NO_TEST_PACKAGES:
            require(not any(p == pkg for p, _ in started_tests), 'test hidden in no-test package')
        if pkg in NO_TEST_SKIPS[step]:
            require(action == 'skip' and not any(p == pkg for p, _ in started_tests)
                    and outputs.get(pkg, '').strip() == '?   \t'+pkg+'\t[no test files]', 'invalid no-test package')
        else:
            require(action == 'pass', 'dependency package did not pass')
    require(started_tests == set(tests), 'unterminated dependency test')
    for item in REQUIRED_ROOTS+OWNED_TESTS:
        key = (item['package'], item['test'])
        require(key in tests and (tests[key] == 'pass' or key in ALLOWED_SKIPS), 'required test absent: '+str(key))
    require(resolver_seen, 'actual forced-cgo resolver witness absent: '+step)
    return dict(packages=terminal, passed=sum(v == 'pass' for v in tests.values()),
                skipped=[dict(package=p, test=t) for (p, t), v in sorted(tests.items()) if v == 'skip'])


def _work(start):
    argv = start.get('command')
    require(isinstance(argv, list) and all(isinstance(x, str) for x in argv), 'invalid root unit command')
    paths = [x[len('-coverprofile='):] for x in argv if x.startswith('-coverprofile=')]
    require(len(paths) == 1, 'root profile argument absent/duplicated')
    path = PurePosixPath(paths[0]) if paths[0].startswith('/') else PureWindowsPath(paths[0])
    require(path.is_absolute() and path.name == 'unit.out', 'root profile path differs')
    return path.parent


def validate_rows(case, rows):
    enabled = validate_case(case)
    names = set(PROFILE_FACTS) | set(SOURCE_FACTS) | set(DB_FACTS)
    relevant = [r for r in rows if r.get('step') in STEPS or r.get('kind') == 'fact' and r.get('name') in names]
    if not enabled:
        require(not relevant, 'undeclared dependency evidence')
        return None
    require(not any(isinstance(r.get('step'), str) and r['step'].startswith('dependency-')
                    and r['step'] not in STEPS for r in rows), 'unknown dependency step output')
    require(not any(r.get('kind') == 'fact' and isinstance(r.get('name'), str)
                    and r['name'].startswith('dependency_') and r['name'] not in names for r in rows),
            'unknown dependency fact')
    before_i, before = _fact(rows, SOURCE_FACTS[0], 'env')
    after_i, after = _fact(rows, SOURCE_FACTS[1], 'env')
    _source(before)
    require(before == after, 'dependency source changed during execution')
    all_starts = [i for i, r in enumerate(rows) if r.get('kind') == 'start']
    all_ends = [i for i, r in enumerate(rows) if r.get('kind') == 'end']
    require(all_starts and all_ends and before_i < min(all_starts) and after_i > max(all_ends),
            'source checks do not enclose all steps')
    unit = _step(rows, 'unit')
    race = _step(rows, 'race')
    work = _work(unit[2])
    require(unit[2].get('command') == ['go', 'test', '-json', '-p=1', '-count=1', '-covermode=atomic',
            '-coverprofile='+str(work/'unit.out'), './cmd/...', './internal/...']
            and unit[2].get('env') == {}, 'root unit scope filtered/changed')
    require(race[2].get('command') == ['go', 'test', '-json', '-p=1', '-race', '-count=1', './cmd/...', './internal/...']
            and race[2].get('env') == {'CGO_ENABLED': '1'}, 'root race scope filtered/changed')
    _, db = _fact(rows, 'vulndb_snapshot', 'env')
    require(isinstance(db, dict) and set(db) == {'schema', 'snapshot', 'uri', 'tool', 'members_sha256'}
            and db.get('schema') == 'go-vulndb-input-v1' and db.get('snapshot') == case['vulndb_snapshot']
            and db.get('tool') == GOVULNCHECK and isinstance(db.get('members_sha256'), str)
            and HASH.fullmatch(db['members_sha256']), 'database registration differs')
    _db_uri(db.get('uri'))
    bdi, bd = _fact(rows, DB_FACTS[0], 'dependency-vuln')
    adi, ad = _fact(rows, DB_FACTS[1], 'dependency-vuln')
    require(bd == db == ad, 'query database changed')
    table = commands(work, case, db['uri'])
    actual = {name: _step(rows, name) for name in STEPS}
    for name, (_, _, start) in actual.items():
        argv, env, _, _ = table[name]
        require(start.get('command') == argv and start.get('env') == env, 'fixed command/env differs: '+name)
    require(bdi < actual['dependency-vuln'][0] < actual['dependency-vuln'][1] < adi,
            'query database checks do not enclose execution')
    texts = {}
    for name, (step, _) in PROFILE_FACTS.items():
        index, value = _fact(rows, name, step)
        end = unit[1] if step == 'unit' else actual[step][1]
        require(end < index < after_i, 'profile fact outside completed step/source checks')
        texts[name] = value
    coverage = summarize_coverage(texts['root_unit_profile'], texts['dependency_unit_profile'])
    require(coverage['meets_threshold'], 'combined exact coverage is below 95%')
    query_lines = [r for r in rows if r.get('step') == 'dependency-vuln' and r.get('kind') == 'line']
    require(query_lines and all(isinstance(r.get('text'), str) and r.get('terminated') is True for r in query_lines),
            'query output has an incomplete line')
    require(not any(r.get('kind') == 'go' and r.get('step') == 'dependency-vuln' for r in rows),
            'query JSON misclassified as Go output')
    query = validate_query('\n'.join(r['text'] for r in query_lines)+'\n', expected_db=db['uri'])
    require(query['db'] == db['uri'], 'query used a different database')
    return dict(schema='websocket-control-result-v1', coverage=coverage, query=query,
                tests={name: _go_tests(rows, name) for name in ('dependency-unit', 'dependency-race')})


def verify_materials(run, case, rows, source_manifest):
    if not validate_case(case):
        require(not any((Path(run)/name).exists() for _, name in PROFILE_FACTS.values()),
                'undeclared dependency profile artifact')
        return
    _, before = _fact(rows, SOURCE_FACTS[0], 'env')
    _source(before)
    # A real runner failure may prevent the final readback. Keep its originals
    # verifiable; validate_rows still requires that readback for quality PASS.
    if any(r.get('kind') == 'fact' and r.get('name') == SOURCE_FACTS[1] for r in rows):
        _, after = _fact(rows, SOURCE_FACTS[1], 'env')
        require(before == after, 'archived source checks differ')
    require(isinstance(source_manifest, dict), 'source manifest absent')
    actual = {k: v for k, v in source_manifest.items() if k == 'go.mod' or k.startswith(DIRECTORY+'/')}
    require(actual == before['files'], 'recorded source manifest differs from imported patch')
    for name, (step, artifact) in PROFILE_FACTS.items():
        selected = [r for r in rows if r.get('kind') == 'fact' and r.get('name') == name]
        path = Path(run)/artifact
        # A genuine failed compiler may produce no profile. Preserve that run;
        # validate_rows still rejects it as quality evidence.
        if not selected:
            require(not path.exists(), 'profile artifact has no raw fact')
            continue
        _, text = _fact(rows, name, step)
        require(isinstance(text, str) and path.is_file() and not path.is_symlink()
                and path.read_bytes() == text.encode('utf-8'), 'profile raw/artifact bytes differ')
