'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { spawn, execFileSync } = require('node:child_process');
const { once } = require('node:events');
const { createInterface } = require('node:readline');
const { createServer } = require('node:http');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const { PNG } = require('pngjs');
const { actions, check, config, Fault, startupDiagnostic } = require('../worker/contract.cjs');
const profileOwner = { access_identity: 'fixture', vmcp_id: 'workbench' };
async function fixture(t, options = {}, beforeStart = async () => {}) {
  const base = await fs.mkdtemp(path.join(os.tmpdir(), 'oi-browser-test-'));
  if (process.platform === 'win32') {
    const identity = JSON.parse(
      execFileSync('whoami.exe', ['/user', '/fo', 'csv', '/nh'], { encoding: 'utf8' })
        .trim()
        .split(',')
        .at(-1),
    );
    execFileSync(
      'icacls.exe',
      [
        base,
        '/inheritance:r',
        '/grant:r',
        '*' + identity + ':(OI)(CI)F',
        '*S-1-5-18:(OI)(CI)F',
        '*S-1-5-32-544:(OI)(CI)F',
      ],
      { stdio: 'ignore' },
    );
  }
  const state = path.join(base, 'state'),
    profile = path.join(base, 'profile');
  await fs.mkdir(state, { mode: 0o700 });
  await fs.mkdir(profile, { mode: 0o700 });
  await beforeStart(state);
  const archive = process.env.OI_BROWSER_TEST_ROOT;
  const app = archive ? path.resolve(archive) : path.resolve(__dirname, '..');
  const executable = archive
    ? path.join(app, 'runtime', process.platform === 'win32' ? 'node.exe' : 'node')
    : process.execPath;
  const child = spawn(executable, [path.join(app, 'worker/main.cjs')], {
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
    cwd: app,
  });
  const lines = createInterface({ input: child.stdout });
  const handlers = new Map();
  let serial = 0;
  let readyResolve, readyReject;
  const ready = new Promise((res, rej) => {
    readyResolve = res;
    readyReject = rej;
  });
  const closed = once(child, 'exit');
  const calls = new Set();
  lines.on('line', (line) => {
    const f = JSON.parse(line);
    if (f.type === 'ready') {
      readyResolve(f);
      return;
    }
    const pair = handlers.get(f.call_id);
    if (!pair) return;
    handlers.delete(f.call_id);
    if (f.type === 'error') pair.reject(Object.assign(new Error(f.error.message), f.error));
    else {
      try {
        check(pair.action + '_output', f.result);
        pair.resolve(f);
      } catch (e) {
        pair.reject(e);
      }
    }
  });
  child.on('exit', () => {
    const err = Error('Worker exited.');
    readyReject(err);
    for (const p of handlers.values()) p.reject(err);
  });
  function send(msg) {
    child.stdin.write(JSON.stringify(msg) + '\n');
  }
  t.after(async () => {
    child.stdin.end();
    let timer;
    try {
      await Promise.race([
        closed,
        new Promise((_, reject) => {
          timer = setTimeout(() => {
            child.kill();
            reject(Error('Worker did not exit.'));
          }, 10000);
        }),
      ]);
    } finally {
      clearTimeout(timer);
      await fs.rm(base, { recursive: true, force: true, maxRetries: 6, retryDelay: 200 });
    }
  });
  send({
    type: 'start',
    protocol: 1,
    result_formats: ['json', 'content.v1'],
    config: {
      // Use an alternate spelling of the same Windows path, even when TEMP has no 8.3 alias.
      state_dir:
        process.platform === 'win32'
          ? state.replace(/^[A-Z]:/i, (drive) => drive.toLowerCase())
          : state,
      profiles: [{ alias: 'reports', path: profile, allowed_callers: [profileOwner] }],
      ...options,
    },
  });
  const first = await ready;
  assert.equal(first.handlers.length, 18);
  assert.deepEqual(first.result_formats, ['json', 'content.v1']);
  function call(action, input, context = profileOwner) {
    if (process.env.OI_BROWSER_TEST_TRACE) console.error('fixture call', action);
    calls.add(action);
    const call_id = 't' + ++serial;
    const wait = new Promise((resolve, reject) =>
      handlers.set(call_id, { resolve, reject, action }),
    );
    send({ type: 'invoke', call_id, handler: action, input, context });
    return wait.then((f) => {
      assert.equal(f.result_format, action === 'screenshot' ? 'content.v1' : undefined);
      return f.result;
    });
  }
  return { call, calls, state };
}
test(
  'record admission, bounded DOM scans, stale focus and expired input preserve page state',
  { timeout: 30000 },
  async (t) => {
    let hits = 0;
    const local = createServer((req, res) => {
      if (req.url === '/hit') {
        hits++;
        res.end('ok');
        return;
      }
      res.setHeader('Content-Type', 'text/html');
      res.end(
        '<button disabled onclick="fetch(\'/hit\')">Slow</button><input aria-label="Name">' +
          '<script>const list=document.createDocumentFragment();' +
          'for(let i=0;i<30000;i++)list.append(document.createElement("i"));document.body.appendChild(list);' +
          'setTimeout(()=>document.querySelector("button").disabled=false,1800);</script>',
      );
    });
    await new Promise((r) => local.listen(0, '127.0.0.1', r));
    t.after(() => {
      local.closeAllConnections();
      return new Promise((r) => local.close(r));
    });
    const url = 'http://127.0.0.1:' + local.address().port;
    const { call } = await fixture(t, {
      limits: { max_operation_records_per_session: 1, operation_retention_ms: 1000 },
    });
    const made = await call('create_session', {}),
      session = { session_id: made.session_id },
      p = { ...session, tab_id: made.tab_id };
    const nav = await call('navigate', { ...p, url });
    await assert.rejects(call('open_tab', session), { code: 'CAPACITY_EXCEEDED' });
    assert.equal((await call('list_tabs', session)).tabs.length, 1);
    const snap = await call('snapshot', { ...p, max_nodes: 8, max_chars: 60 });
    assert.equal(snap.truncated, true);
    assert.ok(snap.truncation_reasons.some((r) => ['node_limit', 'timeout'].includes(r)));
    await new Promise((r) => setTimeout(r, 1050));
    await assert.rejects(call('press_key', { ...p, key: 'Enter', document_id: made.document_id }), {
      code: 'STALE_DOCUMENT',
    });
    await new Promise((r) => setTimeout(r, 1050));
    // A fresh document starts its disabled period again.
    const reload = await call('navigate', { ...p, url, mode: 'goto' });
    assert.notEqual(nav.document_id, reload.document_id);
    await new Promise((r) => setTimeout(r, 1050));
    await assert.rejects(
      call('click', {
        ...p,
        target: { kind: 'role', document_id: reload.document_id, role: 'button', name: 'Slow' },
        timeout_ms: 100,
      }),
      { code: 'TIMEOUT' },
    );
    await new Promise((r) => setTimeout(r, 1000));
    assert.equal(hits, 0, 'expired input must not arrive when the button becomes enabled');
    assert.equal((await call('list_tabs', session)).tabs.length, 0);
    await call('close_session', session);
  },
);
test('stdio enforces the registered contract and trusted caller', async (t) => {
  const { call, state } = await fixture(t);
  assert.equal(startupDiagnostic(new Fault('INVALID_CONFIG', 'private input')), 'INVALID_CONFIG');
  assert.equal(startupDiagnostic(new Error('private path')), 'protocol or initialization error');
  if (process.platform === 'win32') {
    const denied = path.join(state, 'private-name-not-for-logs');
    await fs.mkdir(denied);
    execFileSync('icacls.exe', [denied, '/grant', '*S-1-1-0:R'], { stdio: 'ignore' });
    assert.throws(
      () => config({ state_dir: denied }),
      (err) =>
        startupDiagnostic(err) ===
        'INVALID_CONFIG: Windows directory ACL grants access to an untrusted principal.',
    );
  }
  assert.deepEqual(await call('list_sessions', {}), { sessions: [] });
  await assert.rejects(
    call(
      'create_session',
      { profile: 'reports' },
      { access_identity: 'other', vmcp_id: 'workbench' },
    ),
    { code: 'PROFILE_NOT_ALLOWED' },
  );
  await assert.rejects(
    call('screenshot', {
      session_id: 's',
      tab_id: 't',
      clip: { x: -1, y: 2, width: 8, height: 8 },
    }),
    { code: 'INVALID_ARGUMENT' },
  );
  await assert.rejects(
    call('navigate', { session_id: 's', tab_id: 't', mode: 'reload', url: 'https://example.test' }),
    { code: 'INVALID_ARGUMENT' },
  );
  await assert.rejects(call('list_sessions', {}, {}), { code: 'RESOURCE_NOT_FOUND' });
});
test('reclaims only marked temporary profiles after both recorded processes have exited', async (t) => {
  const { state } = await fixture(t, {}, async (dir) => {
    const gone = spawn(process.execPath, ['-e', ''], { stdio: 'ignore', windowsHide: true });
    const oldPid = gone.pid;
    await once(gone, 'exit');
    for (const [entry, pid] of [
      ['session-aaaaaa', oldPid],
      ['session-bbbbbb', process.pid],
      ['unmanaged', oldPid],
    ]) {
      const sub = path.join(dir, entry);
      await fs.mkdir(sub, { mode: 0o700 });
      await fs.writeFile(
        path.join(sub, '.oi-browser-owner.json'),
        JSON.stringify({ app: 'browser', directory: entry, worker_pid: pid, browser_pid: pid }),
      );
    }
  });
  assert.deepEqual((await fs.readdir(state)).sort(), ['session-bbbbbb', 'unmanaged']);
});
test(
  'private Chromium runs all 18 actions, isolates callers and resumes a confirm',
  { timeout: 120000 },
  async (t) => {
    const local = createServer((req, res) => {
      res.setHeader('Content-Type', 'text/html; charset=UTF-8');
      res.end(
        '<title>Controlled form</title><body style="height:2100px"><label>Name<input id="name"></label>' +
          '<label>Flag<input type="checkbox" id="flag"></label><label for="choice">Choice</label><select id="choice"><option value="a">A</option><option value="b">B</option></select>' +
          "<button onclick=\"if(confirm('Continue?'))document.querySelector('output').innerText=++window.count\">Submit</button>" +
          '<button onclick="this.replaceWith(this.cloneNode(true))">Replace</button><output></output><input type="file" aria-label="File">' +
          '<div style="position:absolute;top:80px;left:32px;width:64px;height:32px;background:rgb(12,34,56)"></div>' +
          '<script>window.count=0;localStorage.setItem("seen",String(1+Number(localStorage.getItem("seen")||0)));' +
          'document.body.insertAdjacentHTML("beforeend","<p>visits "+localStorage.getItem("seen")+"</p>");</script></body>',
      );
    });
    await new Promise((r) => local.listen(0, '127.0.0.1', r));
    t.after(() => {
      local.closeAllConnections();
      return new Promise((r) => local.close(r));
    });
    const url = 'http://127.0.0.1:' + local.address().port;
    const { call, calls, state } = await fixture(t, {
      viewport: { width: 800, height: 600, device_scale_factor: 2 },
      limits: { max_inflight_calls: 1 },
    });
    const initial = await call('create_session', { profile: 'reports' });
    const p = { session_id: initial.session_id, tab_id: initial.tab_id },
      s = { session_id: p.session_id };
    assert.equal(initial.persistent, true);
    await assert.rejects(call('create_session', { profile: 'reports' }), { code: 'PROFILE_BUSY' });
    await assert.rejects(call('list_tabs', s, { access_identity: 'other', vmcp_id: 'workbench' }), {
      code: 'RESOURCE_NOT_FOUND',
    });
    assert.equal((await call('list_sessions', {})).sessions[0].session_id, p.session_id);
    const nav = await call('navigate', { ...p, url });
    assert.equal(nav.result.http_status, 200);
    const snap = await call('snapshot', p);
    assert.match(snap.text, /visits 1/);
    const find = (name) => {
      const e = snap.elements.find((e) => e.name === name);
      assert.ok(e, 'Missing ' + name);
      return { kind: 'ref', snapshot_id: snap.snapshot_id, ref: e.ref };
    };
    const label = (text) => ({ kind: 'label', document_id: snap.document_id, text });
    await call('fill', { ...p, target: label('Name'), text: 'Ada' });
    await call('press_key', { ...p, target: label('Name'), key: 'End' });
    await call('press_key', { ...p, document_id: snap.document_id, key: 'Home' });
    assert.equal(
      (await call('set_checked', { ...p, target: label('Flag'), checked: true })).result
        .actual_checked,
      true,
    );
    assert.equal(
      (await call('set_checked', { ...p, target: label('Flag'), checked: true })).status,
      'unchanged',
    );
    assert.deepEqual(
      (await call('select_option', { ...p, target: label('Choice'), values: ['b'] })).result
        .selected_values,
      ['b'],
    );
    await call('hover', { ...p, target: find('Submit') });
    await assert.rejects(call('click', { ...p, target: label('File') }), {
      code: 'UNSUPPORTED_TARGET',
    });
    const stale = find('Replace');
    await call('click', { ...p, target: stale });
    await assert.rejects(call('click', { ...p, target: stale }), { code: 'STALE_TARGET' });
    const click = await call('click', { ...p, target: find('Submit'), timeout_ms: 250 });
    assert.equal(click.status, 'pending');
    await assert.rejects(call('screenshot', p), { code: 'TAB_BUSY' });
    await new Promise((r) => setTimeout(r, 350));
    const listing = await call('list_tabs', { ...s, operation_id: click.operation_id });
    assert.equal(listing.operation.state, 'blocked');
    const dialog = listing.tabs[0].dialogs[0];
    await call('handle_dialog', { ...p, dialog_id: dialog.dialog_id, action: 'accept' });
    let original;
    for (let i = 0; i < 50; i++) {
      original = (await call('list_tabs', { ...s, operation_id: click.operation_id })).operation;
      if (original.state === 'completed') break;
      await new Promise((r) => setTimeout(r, 20));
    }
    assert.equal(original.state, 'completed');
    assert.equal(original.receipt.operation_id, click.operation_id);
    assert.equal(
      (await call('wait_for', { ...p, condition: { kind: 'text', text: '1' } })).status,
      'matched',
    );
    assert.equal(
      (
        await call('wait_for', {
          ...p,
          condition: { kind: 'text', text: 'never occurs' },
          timeout_ms: 150,
        })
      ).status,
      'timed_out',
    );
    const region = { x: 40, y: 84, width: 8, height: 8 },
      pic = await call('screenshot', { ...p, clip: region });
    const png = PNG.sync.read(Buffer.from(pic.content[1].data, 'base64'));
    assert.deepEqual([png.width, png.height, ...png.data.subarray(0, 4)], [8, 8, 12, 34, 56, 255]);
    assert.deepEqual(pic.structuredContent.source_rect, region);
    await assert.rejects(
      call('screenshot', { ...p, clip: { x: 799, y: 599, width: 2, height: 2 } }),
      { code: 'INVALID_ARGUMENT' },
    );
    assert.equal((await call('scroll', { ...p, delta_y: 200 })).result.changed, true);
    const tab = await call('open_tab', s);
    await call('close_tab', { ...s, tab_id: tab.tab_id });
    await call('close_session', s);
    assert.equal((await call('close_session', s)).status, 'already_closed');
    const reused = await call('create_session', { profile: 'reports' });
    const rp = { session_id: reused.session_id, tab_id: reused.tab_id };
    await call('navigate', { ...rp, url });
    assert.match((await call('snapshot', rp)).text, /visits 2/);
    await call('close_session', { session_id: rp.session_id });
    const tmp = await call('create_session', {});
    await call('close_session', { session_id: tmp.session_id });
    assert.deepEqual(await fs.readdir(state), []);
    assert.deepEqual([...calls].sort(), [...actions].sort());
  },
);
test(
  'one CSS viewport chooses original PNG, original JPEG or a resized overview under the frame cap',
  { timeout: 90000 },
  async (t) => {
    const local = createServer((req, res) => {
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      const filled = Number(new URL(req.url, 'http://fixture.test').searchParams.get('pixels'));
      res.end(
        '<style>*{margin:0;padding:0}</style><canvas width="1280" height="720"></canvas><script>' +
          'const ctx=document.querySelector("canvas").getContext("2d"),img=ctx.createImageData(1280,720);' +
          'let seed=0x32577631;for(let i=0;i<img.data.length/4;i++){seed^=seed<<13;seed^=seed>>>17;seed^=seed<<5;' +
          'img.data[i*4]=i<' +
          filled +
          '?seed&255:255;img.data[i*4+1]=i<' +
          filled +
          '?(seed>>>8)&255:255;' +
          'img.data[i*4+2]=i<' +
          filled +
          '?(seed>>>16)&255:255;img.data[i*4+3]=255}ctx.putImageData(img,0,0);</script>',
      );
    });
    await new Promise((r) => local.listen(0, '127.0.0.1', r));
    t.after(() => {
      local.closeAllConnections();
      return new Promise((r) => local.close(r));
    });
    const { call } = await fixture(t);
    const made = await call('create_session', {}),
      p = { session_id: made.session_id, tab_id: made.tab_id };
    const url = 'http://127.0.0.1:' + local.address().port;
    for (const [pixels, mime, resized] of [
      [145000, 'image/png', false],
      [340000, 'image/jpeg', false],
      [921600, 'image/jpeg', true],
    ]) {
      await call('navigate', { ...p, url: url + '/?pixels=' + pixels, wait_until: 'load' });
      const shot = await call('screenshot', p),
        meta = shot.structuredContent;
      assert.equal(meta.mime_type, mime);
      assert.equal(meta.resized, resized);
      const bytes = Buffer.from(shot.content[1].data, 'base64');
      assert.equal(bytes.length, meta.encoded_bytes);
      const decoded =
        mime === 'image/png' ? PNG.sync.read(bytes) : require('jpeg-js').decode(bytes);
      assert.deepEqual([decoded.width, decoded.height], [meta.preview_width, meta.preview_height]);
      if (pixels === 145000) assert.ok(bytes.length > 384 * 1024);
      if (!resized) assert.equal(meta.preview_width, 1280);
      else assert.ok(meta.preview_width >= 768 && meta.preview_width < 1280);
    }
    await assert.rejects(
      call('screenshot', { ...p, clip: { x: 0, y: 0, width: 1280, height: 720 } }),
      { code: 'IMAGE_PREVIEW_TOO_LARGE' },
    );
    await call('close_session', { session_id: p.session_id });
  },
);
