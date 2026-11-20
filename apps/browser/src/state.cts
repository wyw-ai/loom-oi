import type { BrowserContext, Dialog, ElementHandle, Frame, JSHandle, Page } from 'playwright-core';
import { Config, Data, Fault, assert, bound, id, now } from './contract.cjs';

export type TargetNode = ElementHandle<HTMLElement | SVGElement>;
export type Snapshot = {
  until: number;
  refs: Map<string, { node: TargetNode; frame: string; document: string }>;
};
type FrameRecord = { id: string; handle: JSHandle<Document> };
type DialogRecord = { driver: Dialog; info: Data; timer: NodeJS.Timeout };
export type Op = {
  data: Data;
  until: number;
  control: boolean;
  wake: () => void;
  continuationEnd?: number;
};
export class Session {
  readonly id = id();
  readonly created = now();
  state = 'starting';
  active = Date.now();
  context?: BrowserContext;
  fault: Data | null = null;
  tabs = new Map<string, Tab>();
  pages = new Map<Page, Tab>();
  closedTabs = new Map<string, number>();
  operations = new Map<string, Op>();
  openingTabs = 0;
  seq = 0;
  private ring: Data[] = [];
  constructor(
    public owner: string,
    public profile: string | null,
    public cfg: Config,
  ) {}
  emit(type: string, tab: string | null, resource: string | null, operation: string | null = null) {
    this.ring.push({
      sequence: ++this.seq,
      type,
      tab_id: tab,
      resource_id: resource,
      operation_id: operation,
    });
    if (this.ring.length > this.cfg.limits.max_events_per_session) this.ring.shift();
  }
  events(after: number = this.seq) {
    const items = this.ring.filter((e) => e.sequence > after).slice(0, 20);
    const cursor = items.at(-1)?.sequence ?? Math.min(this.seq, after);
    return {
      cursor,
      items,
      has_more: cursor < this.seq,
      truncated: !!this.ring.length && after < this.ring[0].sequence - 1,
    };
  }
  prune() {
    const time = Date.now(),
      retention = this.cfg.limits.operation_retention_ms;
    for (const [key, op] of this.operations)
      if (op.until && op.until <= time) this.operations.delete(key);
    for (const [key, t] of this.closedTabs) if (t + retention <= time) this.closedTabs.delete(key);
    for (const t of this.tabs.values()) t.prune();
  }
  record() {
    return {
      session_id: this.id,
      state: this.state,
      profile: this.profile,
      persistent: !!this.profile,
      created_at: this.created,
      tab_count: this.tabs.size,
      fault: this.fault,
    };
  }
  attach(page: Page): Tab {
    const known = this.pages.get(page);
    if (known) return known;
    assert(
      this.tabs.size < this.cfg.limits.max_tabs_per_session,
      'CAPACITY_EXCEEDED',
      'Tab limit reached.',
    );
    const tab = new Tab(this, page);
    this.pages.set(page, tab);
    this.tabs.set(tab.id, tab);
    this.emit('tab_opened', tab.id, tab.id);
    return tab;
  }
}
export class Tab {
  readonly id = id();
  document = id();
  title = '';
  state = 'ready';
  current: Op | null = null;
  capturing = false;
  frames = new Map<Frame, FrameRecord>();
  dialogs = new Map<string, DialogRecord>();
  snapshots = new Map<string, Snapshot>();
  initial: Promise<void>;
  private updates = new Map<Frame, Promise<void>>();
  constructor(
    public session: Session,
    public page: Page,
  ) {
    page.on('dialog', (dialog) => this.onDialog(dialog));
    page.on('download', (download) => {
      void download.cancel().catch(() => {});
    });
    page.on('filechooser', () => {
      /* No files are assigned to the originating control. */
    });
    page.on('frameattached', (frame) => {
      void this.refresh(frame);
    });
    page.on('framenavigated', (frame) => {
      void this.refresh(frame, true);
    });
    page.on('framedetached', (frame) => {
      this.dropFrame(frame);
    });
    page.on('domcontentloaded', () => {
      void page
        .title()
        .then((v) => {
          this.title = bound(v, 256);
        })
        .catch(() => {});
    });
    page.on('close', () => {
      this.state = 'closed';
      session.tabs.delete(this.id);
      session.pages.delete(page);
      session.closedTabs.set(this.id, Date.now());
      session.emit('tab_closed', this.id, this.id);
      for (const d of this.dialogs.values()) clearTimeout(d.timer);
      this.dialogs.clear();
      for (const key of [...this.snapshots.keys()]) this.dropSnapshot(key);
      for (const frame of [...this.frames.keys()]) this.dropFrame(frame);
    });
    this.initial = this.refresh(page.mainFrame());
  }
  private dropFrame(frame: Frame) {
    const rec = this.frames.get(frame);
    this.frames.delete(frame);
    if (rec) void rec.handle.dispose().catch(() => {});
  }
  refresh(frame: Frame, navigation = false) {
    const old = this.updates.get(frame) ?? Promise.resolve();
    const task = old
      .then(async () => {
        const prev = this.frames.get(frame);
        if (prev && (await prev.handle.evaluate((doc) => doc === document).catch(() => false)))
          return;
        if (frame.isDetached() || this.state === 'closed') return;
        const handle = await frame.evaluateHandle(() => document);
        if (frame.isDetached() || this.state === 'closed') {
          await handle.dispose();
          return;
        }
        this.dropFrame(frame);
        this.frames.set(frame, { id: id(), handle });
        if (frame === this.page.mainFrame() && prev) this.document = id();
        if (navigation) this.session.emit('navigation_committed', this.id, this.document);
      })
      .catch(() => {});
    this.updates.set(frame, task);
    void task.finally(() => {
      if (this.updates.get(frame) === task) this.updates.delete(frame);
    });
    return task;
  }
  async sync() {
    await this.refresh(this.page.mainFrame());
  }
  onDialog(dialog: Dialog) {
    const key = id(),
      type = dialog.type(),
      message = bound(dialog.message(), 512),
      def = bound(dialog.defaultValue(), 512);
    const end = Date.now() + this.session.cfg.limits.dialog_timeout_ms;
    const info = {
      dialog_id: key,
      type,
      message,
      message_truncated: message !== dialog.message(),
      responses: ['accept', 'dismiss'],
      default_value: type === 'prompt' ? def : null,
      default_value_truncated: type === 'prompt' && def !== dialog.defaultValue(),
      deadline_at: new Date(end).toISOString(),
    };
    const timer = setTimeout(() => {
      void this.respond(key, 'dismiss', undefined, true).catch(() => {});
    }, end - Date.now());
    this.dialogs.set(key, { driver: dialog, info, timer });
    if (this.current?.continuationEnd) {
      Object.assign(this.current.data, {
        state: 'blocked',
        blocked_by: [...this.dialogs.keys()].map((id) => ({ kind: 'dialog', id })),
        deadline_at: new Date(Math.min(this.current.continuationEnd, end)).toISOString(),
      });
    }
    this.session.emit('dialog_opened', this.id, key, this.current?.data.operation_id ?? null);
    this.current?.wake();
  }
  async respond(key: string, action: string, text?: string, expired = false) {
    const d = this.dialogs.get(key);
    assert(d, 'DIALOG_NOT_PENDING', 'Dialog is no longer pending.');
    assert(
      text === undefined || (action === 'accept' && d.info.type === 'prompt'),
      'INVALID_ARGUMENT',
      'Prompt text only applies to accepting a prompt.',
    );
    this.dialogs.delete(key);
    clearTimeout(d.timer);
    await (action === 'accept'
      ? d.driver.accept(d.info.type === 'prompt' ? (text ?? '') : undefined)
      : d.driver.dismiss());
    const original = this.current;
    if (original?.data.state === 'blocked' && !this.dialogs.size)
      Object.assign(original.data, { state: 'running', blocked_by: [], deadline_at: null });
    this.session.emit(
      expired ? 'dialog_expired' : 'dialog_closed',
      this.id,
      key,
      this.current?.data.operation_id ?? null,
    );
    return { dialog_id: key, type: d.info.type, action, handled: true };
  }
  pageInfo() {
    const url = this.page.url();
    assert(
      Array.from(url).length <= 8192,
      'RESULT_TOO_LARGE',
      'Observed URL exceeds the result budget.',
    );
    return { url, title: this.title };
  }
  pending() {
    return {
      dialogs: [...this.dialogs.values()].map((d) => d.info),
      pending_operation_ids: this.current ? [this.current.data.operation_id] : [],
    };
  }
  frameInfo() {
    return [...this.frames.entries()]
      .filter(([frame]) => !frame.isDetached())
      .slice(0, this.session.cfg.limits.max_frames_per_tab)
      .map(([frame, r]) => ({
        frame_id: r.id,
        parent_frame_id: this.frames.get(frame.parentFrame()!)?.id ?? null,
        url: frame.url(),
        name: bound(frame.name(), 256),
        accessible: true,
        omission: null,
      }));
  }
  record() {
    const url = this.page.url();
    return {
      tab_id: this.id,
      document_id: this.document,
      url: url.length <= 8192 ? url : null,
      title: this.title,
      state: this.state,
      frames: this.frameInfo().filter((f) => f.url.length <= 8192),
      ...this.pending(),
      ...(url.length > 8192 ? { omitted_fields: ['url'] } : {}),
    };
  }
  dropSnapshot(key: string) {
    const snap = this.snapshots.get(key);
    this.snapshots.delete(key);
    if (snap) for (const { node } of snap.refs.values()) void node.dispose().catch(() => {});
  }
  prune() {
    for (const [key, snap] of this.snapshots) if (snap.until <= Date.now()) this.dropSnapshot(key);
  }
  async resolve(target: Data, optional = false): Promise<TargetNode | null> {
    await this.sync();
    if (target.kind === 'ref') {
      this.prune();
      const snap = this.snapshots.get(target.snapshot_id);
      assert(snap, 'SNAPSHOT_EXPIRED', 'Snapshot has expired.');
      const entry = snap.refs.get(target.ref);
      assert(entry, 'STALE_TARGET', 'Unknown snapshot element.');
      assert(entry.document === this.document, 'STALE_DOCUMENT', 'The document has changed.');
      assert(
        [...this.frames.values()].some((r) => r.id === entry.frame),
        'STALE_TARGET',
        'The frame has changed.',
      );
      const live = await entry.node
        .evaluate((e) => e.isConnected && e.ownerDocument === document)
        .catch(() => false);
      if (!live && optional) return null;
      assert(live, 'STALE_TARGET', 'The observed element is no longer attached.');
      return entry.node;
    }
    assert(target.document_id === this.document, 'STALE_DOCUMENT', 'The document has changed.');
    const frame = target.frame_id
      ? [...this.frames.entries()].find(([, r]) => r.id === target.frame_id)?.[0]
      : this.page.mainFrame();
    assert(frame && !frame.isDetached(), 'STALE_TARGET', 'The frame has changed.');
    const locator =
      target.kind === 'role'
        ? frame.getByRole(target.role, { name: target.name, exact: target.exact ?? true })
        : frame.getByLabel(target.text, { exact: target.exact ?? true });
    assert(
      (await locator.count()) <= 1,
      'AMBIGUOUS_TARGET',
      'The target must match exactly one element.',
    );
    const nodes = await locator.elementHandles();
    if (nodes.length !== 1) {
      await Promise.all(nodes.map((n) => n.dispose()));
      if (!nodes.length && optional) return null;
      throw new Fault(
        nodes.length ? 'AMBIGUOUS_TARGET' : 'TARGET_NOT_FOUND',
        'The target must match exactly one element.',
      );
    }
    await this.sync();
    if (
      target.document_id !== this.document ||
      (target.frame_id && this.frames.get(frame)?.id !== target.frame_id)
    ) {
      await nodes[0].dispose().catch(() => {});
      throw new Fault(
        target.document_id !== this.document ? 'STALE_DOCUMENT' : 'STALE_TARGET',
        'The target document changed.',
      );
    }
    return nodes[0] as TargetNode;
  }
}
