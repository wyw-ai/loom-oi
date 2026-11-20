import {
  mkdtemp,
  rm,
  realpath,
  readdir,
  lstat,
  readFile,
  writeFile,
  rename,
} from 'node:fs/promises';
import { join, dirname, basename } from 'node:path';
import { chromium } from 'playwright-core';
import {
  Config,
  Data,
  Fault,
  actions,
  assert,
  check,
  failure,
  timeout,
  wire,
} from './contract.cjs';
import { Session } from './state.cjs';
import { inputChecks, mutation } from './actions.cjs';
import { snapshot, waitFor } from './observe.cjs';
import { screenshot } from './image.cjs';

export class BrowserRuntime {
  private sessions = new Map<string, Session>();
  private removed = new Map<string, { owner: string; until: number }>();
  private temporary = new Map<string, string>();
  private locks = new Map<string, string>();
  private starting = new Map<string, Promise<void>>();
  private closing = new Map<string, Promise<string[]>>();
  private ordinary = 0;
  private controls = 0;
  private dialogCalls = 0;
  private images = 0;
  private timer: NodeJS.Timeout;
  private reaping = false;
  private nextReap = 0;
  readonly ready: Promise<void>;
  constructor(public cfg: Config) {
    this.timer = setInterval(() => {
      const time = Date.now();
      if (time >= this.nextReap) {
        this.nextReap = time + 30000;
        void this.reapOrphans().catch(() => {});
      }
      for (const [key, r] of this.removed) if (r.until <= time) this.removed.delete(key);
      for (const s of this.sessions.values()) {
        s.prune();
        if (s.active + cfg.limits.session_idle_timeout_ms <= time)
          void this.close(s).catch(() => {});
      }
    }, 1000);
    this.timer.unref();
    this.ready = this.reapOrphans();
  }
  private async reapOrphans() {
    if (this.reaping) return;
    this.reaping = true;
    const dead = (pid: unknown) => {
      if (typeof pid !== 'number' || !Number.isSafeInteger(pid) || pid <= 0) return false;
      try {
        process.kill(pid, 0);
        return false;
      } catch (err) {
        return (err as NodeJS.ErrnoException).code === 'ESRCH';
      }
    };
    try {
      for (const entry of await readdir(this.cfg.state_dir, { withFileTypes: true })) {
        if (!entry.isDirectory() || !/^session-[A-Za-z0-9]{6}$/.test(entry.name)) continue;
        const dir = join(this.cfg.state_dir, entry.name);
        try {
          if ((await realpath(dir)) !== dir) continue;
          const file = join(dir, '.oi-browser-owner.json'),
            stat = await lstat(file);
          if (!stat.isFile() || stat.size > 1024) continue;
          const marker = JSON.parse(await readFile(file, 'utf8'));
          if (
            marker.app !== 'browser' ||
            marker.directory !== entry.name ||
            !dead(marker.worker_pid) ||
            !dead(marker.browser_pid)
          )
            continue;
          await rm(dir, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
        } catch {
          /* A live instance may have just removed or locked its directory. */
        }
      }
    } finally {
      this.reaping = false;
    }
  }
  async shutdown() {
    clearInterval(this.timer);
    await Promise.allSettled([...this.sessions.values()].map((s) => this.close(s)));
  }
  private async create(owner: string, context: Data, a: Data, ms: number, signal: AbortSignal) {
    assert(
      this.sessions.size < this.cfg.limits.max_sessions,
      'CAPACITY_EXCEEDED',
      'Session limit reached.',
    );
    const profile = this.cfg.profiles.find((p) => p.alias === a.profile);
    if (a.profile) {
      assert(
        profile?.allowed_callers.some(
          (p: Data) =>
            p.access_identity === context.access_identity && p.vmcp_id === context.vmcp_id,
        ),
        'PROFILE_NOT_ALLOWED',
        'The requested profile is unavailable.',
      );
      assert(!this.locks.has(profile!.path), 'PROFILE_BUSY', 'The profile is in use.');
    }
    const s = new Session(owner, a.profile ?? null, this.cfg);
    this.sessions.set(s.id, s);
    if (profile) this.locks.set(profile.path, s.id);
    const launch = async () => {
      const dir = profile?.path ?? (await mkdtemp(join(this.cfg.state_dir, 'session-')));
      if (!profile) this.temporary.set(s.id, dir);
      const vp = this.cfg.viewport;
      const ctx = await chromium.launchPersistentContext(dir, {
        channel: 'chromium',
        chromiumSandbox: true,
        headless: true,
        acceptDownloads: false,
        viewport: { width: vp.width, height: vp.height },
        deviceScaleFactor: vp.device_scale_factor,
        timeout: ms,
        handleSIGINT: false,
        handleSIGTERM: false,
        handleSIGHUP: false,
      });
      s.context = ctx;
      if (!profile) {
        const cdp = await ctx.browser()!.newBrowserCDPSession();
        try {
          const info = await cdp.send('SystemInfo.getProcessInfo');
          const pid = info.processInfo.find((p) => p.type === 'browser')?.id;
          assert(pid, 'BROWSER_START_FAILED', 'Could not identify the managed browser process.');
          const mark = {
            app: 'browser',
            directory: basename(dir),
            worker_pid: process.pid,
            browser_pid: pid,
          };
          const file = join(dir, '.oi-browser-owner.json');
          await writeFile(file + '.new', JSON.stringify(mark), { flag: 'wx', mode: 0o600 });
          await rename(file + '.new', file);
        } finally {
          await cdp.detach().catch(() => {});
        }
      }
      ctx.on('close', () => {
        if (s.state !== 'closing') {
          s.state = 'interrupted';
          s.fault = { code: 'BROWSER_DISCONNECTED', message: 'Browser disconnected.' };
        }
      });
      ctx.on('page', (page) => {
        try {
          s.attach(page);
        } catch {
          void page.close({ runBeforeUnload: false }).catch(() => {});
        }
      });
      const page = ctx.pages()[0] ?? (await ctx.newPage());
      const tab = s.attach(page);
      await tab.initial;
      // Persistent profiles may remember pages. Only the new blank tab belongs to this session.
      if (page.url() !== 'about:blank') await page.goto('about:blank', { timeout: ms });
      for (const other of ctx.pages())
        if (other !== page) await other.close({ runBeforeUnload: false });
      await tab.sync();
      if (s.state === 'starting') s.state = 'ready';
    };
    const job = launch();
    this.starting.set(s.id, job);
    void job.finally(() => this.starting.delete(s.id)).catch(() => {});
    try {
      await timeout(job, ms, signal);
      assert(s.state === 'ready', 'BROWSER_START_FAILED', 'Browser did not become ready.');
      const t = [...s.tabs.values()][0];
      return {
        session_id: s.id,
        state: 'ready',
        profile: s.profile,
        persistent: !!s.profile,
        tab_id: t.id,
        document_id: t.document,
        viewport: this.cfg.viewport,
        capabilities: ['semantic_targets', 'snapshot_refs', 'frames', 'dialogs', 'screenshots'],
      };
    } catch (e) {
      const err = failure(e, 'BROWSER_START_FAILED');
      s.fault = err.data();
      s.state = 'failed';
      void this.close(s).catch(() => {});
      throw new Fault(err.code, err.message, { session_id: s.id, state: 'failed' });
    }
  }
  private close(s: Session): Promise<string[]> {
    const other = this.closing.get(s.id);
    if (other) return other;
    const job = (async () => {
      s.state = 'closing';
      await this.starting.get(s.id)?.catch(() => {});
      const tabs = [...s.tabs.keys()];
      await s.context?.close();
      const dir = this.temporary.get(s.id);
      if (dir) {
        const real = await realpath(dir);
        assert(
          dirname(real) === this.cfg.state_dir && real === dir,
          'INTERNAL_ERROR',
          'Temporary directory changed identity.',
        );
        await rm(real, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
        this.temporary.delete(s.id);
      }
      for (const [path, owner] of this.locks) if (owner === s.id) this.locks.delete(path);
      this.sessions.delete(s.id);
      this.removed.set(s.id, {
        owner: s.owner,
        until: Date.now() + this.cfg.limits.operation_retention_ms,
      });
      return tabs;
    })().catch((e) => {
      s.state = 'failed';
      s.fault = failure(e).data();
      throw e;
    });
    this.closing.set(s.id, job);
    void job.finally(() => this.closing.delete(s.id)).catch(() => {});
    return job;
  }
  async invoke(
    action: string,
    a: Data,
    context: Data,
    callId: string,
    signal: AbortSignal,
  ): Promise<Data> {
    assert(actions.includes(action), 'INVALID_ARGUMENT', 'Unknown handler.');
    check(action + '_input', a);
    inputChecks(action, a);
    assert(
      typeof context?.access_identity === 'string' &&
        !!context.access_identity &&
        typeof context?.vmcp_id === 'string' &&
        !!context.vmcp_id,
      'RESOURCE_NOT_FOUND',
      'Trusted caller identity is required.',
    );
    const owner = JSON.stringify([context.access_identity, context.vmcp_id]);
    const cfg = this.cfg.limits,
      ms =
        action === 'create_session'
          ? Math.min(a.timeout_ms ?? 30000, 30000)
          : Math.min(a.timeout_ms ?? cfg.operation_timeout_ms, cfg.operation_timeout_ms);
    const control = ['list_sessions', 'list_tabs', 'close_session', 'close_tab'].includes(action);
    if (control) {
      assert(this.controls < 2, 'CAPACITY_EXCEEDED', 'Control capacity reached.');
      this.controls++;
    } else if (action === 'handle_dialog') {
      assert(this.dialogCalls < 2, 'CAPACITY_EXCEEDED', 'Dialog control capacity reached.');
      this.dialogCalls++;
    } else {
      assert(this.ordinary < cfg.max_inflight_calls, 'CAPACITY_EXCEEDED', 'Call capacity reached.');
      this.ordinary++;
    }
    try {
      if (action === 'create_session') return await this.create(owner, context, a, ms, signal);
      if (action === 'list_sessions')
        return {
          sessions: [...this.sessions.values()]
            .filter((s) => s.owner === owner)
            .map((s) => s.record()),
        };
      if (action === 'close_session' && this.removed.get(a.session_id)?.owner === owner)
        return {
          session_id: a.session_id,
          status: 'already_closed',
          closed_tab_ids: [],
          warnings: [],
        };
      const s = this.sessions.get(a.session_id);
      assert(s && s.owner === owner, 'RESOURCE_NOT_FOUND', 'Resource is unavailable.');
      s.active = Date.now();
      s.prune();
      if (action === 'close_session')
        return {
          session_id: s.id,
          status: 'closed',
          closed_tab_ids: await timeout(this.close(s), ms, signal),
          warnings: [],
        };
      if (action === 'list_tabs') {
        const op = a.operation_id ? s.operations.get(a.operation_id) : null;
        assert(
          !a.operation_id || op,
          'OPERATION_RESULT_EXPIRED',
          'No retained result for that operation.',
        );
        const result: Data = {
          session_id: s.id,
          tabs: [...s.tabs.values()].map((t) => t.record()),
          events: s.events(a.after_sequence),
          operation: op?.data ?? null,
        };
        if (
          Buffer.byteLength(JSON.stringify(wire(callId, action, result))) + 1 >
          cfg.max_result_frame_bytes
        ) {
          for (const t of result.tabs) {
            t.url = null;
            t.title = null;
            t.frames = [];
            t.omitted_fields = ['url', 'title', 'frames'];
          }
        }
        return result;
      }
      if (action === 'close_tab' && s.closedTabs.has(a.tab_id))
        return { session_id: s.id, tab_id: a.tab_id, status: 'already_closed', warnings: [] };
      assert(
        s.state === 'ready' || action === 'close_tab',
        'BROWSER_DISCONNECTED',
        'Session is unavailable for browser actions.',
      );
      if (action === 'open_tab') {
        assert(
          s.tabs.size + s.openingTabs < cfg.max_tabs_per_session,
          'CAPACITY_EXCEEDED',
          'Tab limit reached.',
        );
        const ordinary = [...s.operations.values()].filter((op) => !op.control).length;
        assert(
          ordinary + s.openingTabs < cfg.max_operation_records_per_session,
          'CAPACITY_EXCEEDED',
          'Operation retention capacity reached.',
        );
        s.openingTabs++;
        let abandoned = false;
        const start = Date.now();
        const opening = (async () => {
          const page = await s.context!.newPage();
          try {
            const tab = s.attach(page);
            await tab.initial;
            assert(!abandoned, 'CANCELLED', 'Tab creation was cancelled.');
            return tab;
          } catch (e) {
            await page.close({ runBeforeUnload: false }).catch(() => {});
            throw e;
          }
        })();
        let tab;
        try {
          tab = await timeout(opening, ms, signal);
        } catch (e) {
          abandoned = true;
          void opening.finally(() => s.openingTabs--).catch(() => {});
          throw e;
        }
        // Registration runs synchronously until the first driver await, consuming our reserved slot.
        s.openingTabs--;
        return await mutation(tab, action, a, Math.max(1, ms - (Date.now() - start)), signal);
      }
      const t = s.tabs.get(a.tab_id);
      assert(t, 'RESOURCE_NOT_FOUND', 'Tab is unavailable.');
      if (action === 'close_tab') {
        await timeout(t.page.close({ runBeforeUnload: false }), ms, signal);
        return { session_id: s.id, tab_id: t.id, status: 'closed', warnings: [] };
      }
      if (action === 'wait_for') {
        const controller = new AbortController(),
          cursor = s.seq,
          start = Date.now();
        try {
          return await timeout(
            waitFor(t, a.condition, ms, AbortSignal.any([signal, controller.signal])),
            ms,
            signal,
          );
        } catch (e) {
          if (failure(e).code !== 'TIMEOUT') throw e;
          const blockers = [...t.dialogs.keys()].map((id) => ({ kind: 'dialog', id }));
          return {
            session_id: s.id,
            tab_id: t.id,
            document_id: t.document,
            status: blockers.length ? 'blocked' : 'timed_out',
            elapsed_ms: Date.now() - start,
            observation: null,
            events: s.events(cursor),
            pending: t.pending(),
            ...(blockers.length ? { blocked_by: blockers } : {}),
          };
        } finally {
          controller.abort();
        }
      }
      if (action === 'snapshot' || action === 'screenshot') {
        if (action === 'screenshot') {
          assert(this.images === 0, 'CAPACITY_EXCEEDED', 'Image processor is busy.');
          this.images++;
        }
        const controller = new AbortController();
        const combined = AbortSignal.any([signal, controller.signal]);
        const task =
          action === 'snapshot'
            ? snapshot(t, a, callId, combined)
            : screenshot(t, a, callId, ms, combined);
        try {
          return await timeout(task, ms, signal);
        } finally {
          controller.abort();
          if (action === 'screenshot') void task.finally(() => this.images--).catch(() => {});
        }
      }
      return await mutation(t, action, a, ms, signal);
    } finally {
      if (control) this.controls--;
      else if (action === 'handle_dialog') this.dialogCalls--;
      else this.ordinary--;
    }
  }
}
