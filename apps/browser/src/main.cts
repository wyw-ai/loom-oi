import { once } from 'node:events';
import { resolve, parse } from 'node:path';
import { Data, Fault, actions, assert, check, config, failure, wire } from './contract.cjs';
import type { BrowserRuntime } from './runtime.cjs';

function decodeFrame(raw: Buffer): Data {
  const line = new TextDecoder('utf-8', { fatal: true }).decode(raw);
  // JSON.parse validates the grammar. This pass additionally preserves duplicate-key evidence.
  const value = JSON.parse(line);
  const stack: (Set<string> | null)[] = [];
  const tokens = /"(?:\\[\s\S]|[^"\\])*"|[{}[\]:]/g;
  let match: RegExpExecArray | null;
  while ((match = tokens.exec(line))) {
    const t = match[0];
    if (t === '{') stack.push(new Set());
    else if (t === '[') stack.push(null);
    else if (t === '}' || t === ']') stack.pop();
    else if (t.startsWith('"')) {
      const s = JSON.parse(t);
      assert(s.isWellFormed(), 'INVALID_ARGUMENT', 'Invalid Unicode string.');
      let p = tokens.lastIndex;
      while (p < line.length && /\s/.test(line[p])) p++;
      if (line[p] === ':') {
        const keys = stack.at(-1);
        assert(keys && !keys.has(s), 'INVALID_ARGUMENT', 'Duplicate object key.');
        keys.add(s);
      }
    }
  }
  assert(
    value && typeof value === 'object' && !Array.isArray(value),
    'INVALID_ARGUMENT',
    'An object frame is required.',
  );
  return value;
}
async function main() {
  process.env.PLAYWRIGHT_BROWSERS_PATH = resolve(__dirname, '../browsers');
  if (process.platform === 'win32' && !process.env.SystemDrive && process.env.SystemRoot) {
    // OIC passes SystemRoot. Chromium expands %SystemDrive% for Windows system resources.
    process.env.SystemDrive = parse(process.env.SystemRoot).root.replace(/\\$/, '');
  }
  const { BrowserRuntime } = await import('./runtime.cjs');
  let runtime: BrowserRuntime | undefined,
    writing = Promise.resolve();
  const pending = new Map<string, AbortController>();
  const jobs = new Set<Promise<void>>();
  let buffered = Buffer.alloc(0);
  const send = (frame: Data) => {
    const line = JSON.stringify(frame) + '\n';
    assert(
      Buffer.byteLength(line) <= 1048576,
      'RESULT_TOO_LARGE',
      'Protocol frame limit exceeded.',
    );
    writing = writing.then(async () => {
      if (!process.stdout.write(line)) await once(process.stdout, 'drain');
    });
    return writing;
  };
  async function receive(f: Data) {
    if (!runtime) {
      assert(
        f.type === 'start' && f.protocol === 1,
        'INVALID_ARGUMENT',
        'Expected protocol 1 start.',
      );
      assert(
        Array.isArray(f.result_formats) &&
          f.result_formats.includes('json') &&
          f.result_formats.includes('content.v1'),
        'INVALID_ARGUMENT',
        'The Browser manifest requires json and content.v1.',
      );
      runtime = new BrowserRuntime(config(f.config ?? {}));
      await runtime.ready;
      await send({
        type: 'ready',
        protocol: 1,
        handlers: actions,
        result_formats: ['json', 'content.v1'],
      });
      return;
    }
    assert(
      typeof f.call_id === 'string' && f.call_id.length > 0,
      'INVALID_ARGUMENT',
      'Missing call ID.',
    );
    if (f.type === 'cancel') {
      pending.get(f.call_id)?.abort();
      return;
    }
    assert(
      f.type === 'invoke' && !pending.has(f.call_id),
      'INVALID_ARGUMENT',
      'Invalid or duplicate invoke.',
    );
    const ctrl = new AbortController();
    pending.set(f.call_id, ctrl);
    const r = runtime;
    const job = (async () => {
      try {
        const result = await r.invoke(
          f.handler,
          f.input === undefined ? {} : f.input,
          f.context,
          f.call_id,
          ctrl.signal,
        );
        check(f.handler + '_output', result);
        const frame = wire(f.call_id, f.handler, result);
        assert(
          Buffer.byteLength(JSON.stringify(frame)) + 1 <= r.cfg.limits.max_result_frame_bytes,
          'RESULT_TOO_LARGE',
          'Result exceeds the configured frame budget.',
        );
        await send(frame);
      } catch (e) {
        await send({ type: 'error', call_id: f.call_id, error: failure(e).data() });
      } finally {
        pending.delete(f.call_id);
      }
    })();
    jobs.add(job);
    void job.finally(() => jobs.delete(job)).catch(() => {});
  }
  try {
    for await (const chunk of process.stdin) {
      buffered = Buffer.concat([buffered, chunk]);
      let end: number;
      while ((end = buffered.indexOf(10)) >= 0) {
        assert(end + 1 <= 1048576, 'INVALID_ARGUMENT', 'Protocol input too large.');
        const f = decodeFrame(buffered.subarray(0, end));
        buffered = buffered.subarray(end + 1);
        await receive(f);
      }
      assert(buffered.length < 1048576, 'INVALID_ARGUMENT', 'Protocol input too large.');
    }
    assert(!buffered.length, 'INVALID_ARGUMENT', 'Incomplete protocol frame.');
  } finally {
    for (const ctrl of pending.values()) ctrl.abort();
    await runtime?.shutdown();
    await Promise.allSettled(jobs);
    await writing;
  }
}
main().catch(() => {
  process.stderr.write('Browser worker stopped after a protocol or initialization error.\n');
  process.exitCode = 1;
});
