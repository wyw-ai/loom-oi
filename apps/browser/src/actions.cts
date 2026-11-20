import { Data, Fault, address, assert, failure, id, timeout, utf8 } from './contract.cjs';
import { Tab, Op, TargetNode } from './state.cjs';

type Effect = { result: Data; unchanged?: boolean };
const roles = new Set(
  'alert alertdialog application article banner blockquote button caption cell checkbox code columnheader combobox complementary contentinfo definition deletion dialog directory document emphasis feed figure form generic grid gridcell group heading img insertion link list listbox listitem log main marquee math meter menu menubar menuitem menuitemcheckbox menuitemradio navigation none note option paragraph presentation progressbar radio radiogroup region row rowgroup rowheader scrollbar search searchbox separator slider spinbutton status strong subscript superscript switch tab table tablist tabpanel term textbox time timer toolbar tooltip tree treegrid treeitem'.split(
    ' ',
  ),
);
export function inputChecks(action: string, a: Data) {
  for (const target of [a.target, a.scope, a.condition?.target, a.condition?.scope].filter(
    Boolean,
  )) {
    if (target.kind === 'role')
      assert(
        roles.has(target.role) && utf8(target.name, 512),
        'INVALID_ARGUMENT',
        'Invalid role or accessible name.',
      );
    if (target.kind === 'label')
      assert(utf8(target.text, 512), 'INVALID_ARGUMENT', 'Label exceeds its byte budget.');
  }
  if (a.url) address(a.url);
  if (action === 'fill')
    assert(utf8(a.text, 16384), 'INVALID_ARGUMENT', 'Text exceeds its byte budget.');
  if (a.condition?.kind === 'text')
    assert(
      utf8(a.condition.text, 2048),
      'INVALID_ARGUMENT',
      'Text condition exceeds its byte budget.',
    );
  if (action === 'select_option')
    assert(
      a.values.reduce((n: number, s: string) => n + Buffer.byteLength(s), 0) <= 8192,
      'INVALID_ARGUMENT',
      'Option values exceed their byte budget.',
    );
  if (a.prompt_text !== undefined)
    assert(utf8(a.prompt_text, 8192), 'INVALID_ARGUMENT', 'Prompt exceeds its byte budget.');
  if (a.key) assert(utf8(a.key, 128), 'UNSUPPORTED_KEY', 'Key exceeds its byte budget.');
  if (a.target?.document_id && a.document_id)
    assert(a.target.document_id === a.document_id, 'INVALID_ARGUMENT', 'Conflicting document IDs.');
}
async function navigate(tab: Tab, a: Data, ms: number): Promise<Effect> {
  const page = tab.page,
    mode = a.mode ?? 'goto',
    waitUntil = a.wait_until ?? 'domcontentloaded';
  const opts = { timeout: ms, waitUntil };
  const oldDoc = tab.document,
    oldUrl = page.url();
  const r =
    mode === 'goto'
      ? await page.goto(address(a.url ?? 'about:blank'), opts)
      : mode === 'reload'
        ? await page.reload(opts)
        : mode === 'back'
          ? await page.goBack(opts)
          : await page.goForward(opts);
  await tab.sync();
  const noHistory =
    ['back', 'forward'].includes(mode) && !r && tab.document === oldDoc && oldUrl === page.url();
  return {
    unchanged: noHistory,
    result: {
      mode,
      requested_url: mode === 'goto' ? (a.url ?? 'about:blank') : null,
      actual_url: address(page.url()),
      http_status: r?.status() ?? null,
      phase: noHistory ? 'not_started' : waitUntil === 'commit' ? 'committed' : waitUntil,
      committed: !noHistory,
      reason: noHistory ? 'no_history' : null,
    },
  };
}
async function actionEffect(tab: Tab, action: string, a: Data, ms: number): Promise<Effect> {
  if (action === 'handle_dialog')
    return { result: await tab.respond(a.dialog_id, a.action, a.prompt_text) };
  if (action === 'navigate' || action === 'open_tab') return navigate(tab, a, ms);
  if (a.document_id) {
    await tab.sync();
    assert(a.document_id === tab.document, 'STALE_DOCUMENT', 'The document has changed.');
  }
  let node: TargetNode | null = null;
  try {
    if (a.target) node = await tab.resolve(a.target);
    if (action === 'press_key' && !node) {
      let frame = tab.page.mainFrame();
      for (;;) {
        node = (await frame.evaluateHandle(
          () => document.activeElement ?? document.body,
        )) as TargetNode;
        const child = await node.contentFrame();
        if (!child) break;
        await node.dispose();
        frame = child;
      }
    }
    if (node) {
      const file = await node.evaluate((e) => e.matches('input[type="file"]'));
      assert(!file, 'UNSUPPORTED_TARGET', 'File inputs are outside the Browser contract.');
    }
    if (action === 'click') {
      const doc = tab.document,
        seq = tab.session.seq;
      await node!.click({
        button: a.button ?? 'left',
        clickCount: a.click_count ?? 1,
        modifiers: a.modifiers ?? [],
        timeout: ms,
      });
      await tab.sync();
      const ev = tab.session.events(seq).items;
      return {
        result: {
          button: a.button ?? 'left',
          click_count: a.click_count ?? 1,
          new_tab_ids: ev
            .filter(
              (e) => e.type === 'tab_opened' && e.operation_id === tab.current?.data.operation_id,
            )
            .map((e) => e.tab_id),
          dialog_ids: ev
            .filter(
              (e) =>
                e.type === 'dialog_opened' &&
                e.tab_id === tab.id &&
                e.operation_id === tab.current?.data.operation_id,
            )
            .map((e) => e.resource_id),
          navigation_committed: tab.document !== doc,
        },
      };
    }
    if (action === 'fill') {
      const mode = a.mode ?? 'replace';
      assert(
        await node!.evaluate((e) =>
          e.matches(
            'textarea,input:not([type=file]):not([type=button]):not([type=checkbox]):not([type=radio]):not([type=submit]),[contenteditable="true"],[contenteditable=""]',
          ),
        ),
        'UNSUPPORTED_TARGET',
        'A text-editable field is required.',
      );
      if (mode === 'replace') await node!.fill(a.text, { timeout: ms });
      else {
        await node!.focus();
        await node!.type(a.text, { timeout: ms });
      }
      return {
        result: { mode, requested_characters: Array.from(a.text).length, input_completed: true },
      };
    }
    if (action === 'select_option') {
      const state = await node!.evaluate((e) =>
        e instanceof HTMLSelectElement
          ? {
              multiple: e.multiple,
              options: [...e.options].map((o) => o.value),
              current: [...e.selectedOptions].map((o) => o.value),
            }
          : null,
      );
      assert(state, 'UNSUPPORTED_TARGET', 'A native select is required.');
      assert(
        state.multiple || a.values.length === 1,
        'INVALID_ARGUMENT',
        'Single select requires exactly one value.',
      );
      for (const value of a.values)
        assert(
          state.options.filter((x) => x === value).length === 1,
          'TARGET_NOT_FOUND',
          'Option must be unique and present.',
        );
      if (
        state.current.length === a.values.length &&
        state.current.every((x) => a.values.includes(x))
      )
        return { unchanged: true, result: { selected_values: state.current } };
      return {
        result: {
          selected_values: await node!.selectOption(
            a.values.map((value: string) => ({ value })),
            { timeout: ms },
          ),
        },
      };
    }
    if (action === 'set_checked') {
      const type = await node!.evaluate((e) => (e instanceof HTMLInputElement ? e.type : ''));
      assert(
        ['checkbox', 'radio'].includes(type),
        'UNSUPPORTED_TARGET',
        'A native checkbox or radio is required.',
      );
      assert(
        type !== 'radio' || a.checked,
        'INVALID_ARGUMENT',
        'A radio cannot be unchecked directly.',
      );
      const previous = await node!.isChecked();
      if (previous !== a.checked) await node!.setChecked(a.checked, { timeout: ms });
      const actual = await node!.isChecked();
      assert(actual === a.checked, 'POSTCONDITION_FAILED', 'Checked state could not be verified.');
      return { unchanged: previous === actual, result: { actual_checked: actual } };
    }
    if (action === 'press_key') {
      try {
        if (!a.target) {
          await tab.sync();
          assert(a.document_id === tab.document, 'STALE_DOCUMENT', 'The document has changed.');
          assert(
            await node!
              .evaluate((e) => e.isConnected && e.ownerDocument.activeElement === e)
              .catch(() => false),
            'STALE_TARGET',
            'The previously focused element has changed.',
          );
        }
        await node!.press(a.key, { timeout: ms });
      } catch (e) {
        if (e instanceof Error && /Unknown key|Unknown modifier/.test(e.message))
          throw new Fault('UNSUPPORTED_KEY', 'Unsupported key.');
        throw e;
      }
      return { result: { key: a.key, focus: null } };
    }
    if (action === 'hover') {
      await node!.hover({ timeout: ms });
      return { result: { hovered: true } };
    }
    if (action === 'scroll') {
      const mode = a.mode ?? 'by';
      const positions = () =>
        node
          ? node.evaluate((element, type) => {
              if (type === 'by') return [{ x: element.scrollLeft, y: element.scrollTop }];
              const ancestors = [];
              for (let p = element.parentElement; p; p = p.parentElement) {
                if (p.scrollWidth > p.clientWidth || p.scrollHeight > p.clientHeight)
                  ancestors.push({ x: p.scrollLeft, y: p.scrollTop });
              }
              return [...ancestors, { x: window.scrollX, y: window.scrollY }];
            }, mode)
          : tab.page.evaluate(() => [{ x: window.scrollX, y: window.scrollY }]);
      const initial = await positions();
      if (mode === 'into_view') await node!.scrollIntoViewIfNeeded({ timeout: ms });
      else if (node) {
        assert(
          await node.evaluate(
            (e) => e.scrollWidth > e.clientWidth || e.scrollHeight > e.clientHeight,
          ),
          'UNSUPPORTED_TARGET',
          'Target is not scrollable.',
        );
        await node.evaluate((e, d) => e.scrollBy({ left: d.x, top: d.y, behavior: 'instant' }), {
          x: a.delta_x ?? 0,
          y: a.delta_y ?? 0,
        });
      } else
        await tab.page.evaluate(
          (d) => window.scrollBy({ left: d.x, top: d.y, behavior: 'instant' }),
          { x: a.delta_x ?? 0, y: a.delta_y ?? 0 },
        );
      const final = await positions();
      const index = initial.findIndex(
        (p, i) => final[i] && (p.x !== final[i].x || p.y !== final[i].y),
      );
      const changed = index !== -1,
        before = initial[Math.max(0, index)],
        after = final[Math.max(0, index)];
      return { unchanged: !changed, result: { mode, before, after, changed } };
    }
    throw new Fault('INVALID_ARGUMENT', 'Unknown action.');
  } finally {
    if (node && a.target?.kind !== 'ref') await node.dispose().catch(() => {});
  }
}
export async function mutation(tab: Tab, action: string, a: Data, ms: number, signal: AbortSignal) {
  const s = tab.session,
    cfg = s.cfg.limits,
    control = action === 'handle_dialog';
  if (!control)
    assert(
      tab.state === 'ready' && !tab.current && !tab.capturing && !tab.dialogs.size,
      'TAB_BUSY',
      'The tab has an unfinished operation.',
    );
  s.prune();
  const count = [...s.operations.values()].filter((op) =>
    control ? op.control && op.data.tab_id === tab.id : !op.control,
  ).length;
  assert(
    count + (control ? 0 : s.openingTabs) < (control ? 4 : cfg.max_operation_records_per_session),
    'CAPACITY_EXCEEDED',
    'Operation retention capacity reached.',
  );
  const key = id(),
    cursor = s.seq,
    hard = ms + cfg.blocked_operation_timeout_ms,
    start = Date.now(),
    originalDoc = tab.document;
  let wake!: () => void;
  const blocked = new Promise<void>((r) => (wake = r));
  const op: Op = {
    control,
    until: 0,
    wake,
    data: {
      operation_id: key,
      action,
      tab_id: tab.id,
      state: 'running',
      blocked_by: [],
      deadline_at: null,
      receipt: null,
      error: null,
    },
  };
  s.operations.set(key, op);
  if (!control) {
    tab.current = op;
    tab.state = 'busy';
  }
  const receipt = (status: string, result: Data | null) => ({
    session_id: s.id,
    tab_id: tab.id,
    document_id: tab.document,
    operation_id: key,
    status,
    page: tab.pageInfo(),
    events: s.events(cursor),
    result,
  });
  let settled = false,
    deadline: NodeJS.Timeout;
  const finish = (error: Fault | null, value: Data | null, uncertain: boolean) => {
    if (settled) return;
    settled = true;
    clearTimeout(deadline);
    op.until = Date.now() + cfg.operation_retention_ms;
    Object.assign(op.data, {
      state: uncertain ? 'unknown' : error ? 'failed' : 'completed',
      receipt: value,
      error: error?.data() ?? null,
      blocked_by: [],
      deadline_at: null,
    });
    if (tab.current === op) {
      tab.current = null;
      if (tab.state !== 'closed') tab.state = uncertain ? 'interrupted' : 'ready';
    }
    s.emit('operation_updated', tab.id, key, key);
  };
  const fail = (err: unknown, uncertain = false) => {
    const f = failure(
      err,
      action === 'navigate' || action === 'open_tab'
        ? 'NAVIGATION_FAILED'
        : 'TARGET_NOT_ACTIONABLE',
    );
    const pre = [
      'STALE_TARGET',
      'STALE_DOCUMENT',
      'SNAPSHOT_EXPIRED',
      'TARGET_NOT_FOUND',
      'AMBIGUOUS_TARGET',
      'UNSUPPORTED_TARGET',
      'INVALID_ARGUMENT',
      'UNSUPPORTED_KEY',
      'DIALOG_NOT_PENDING',
    ];
    const effect = pre.includes(f.code)
      ? 'not_applied'
      : action === 'fill' && a.mode === 'type'
        ? 'partially_applied'
        : 'unknown';
    const details: Data = {
      effect_state: effect,
      session_id: s.id,
      tab_id: tab.id,
      document_id: tab.document,
      operation_id: key,
    };
    if (action === 'navigate' || action === 'open_tab') {
      details.committed = tab.document !== originalDoc;
      details.current_url = tab.page.url();
    }
    const error = new Fault(f.code, f.message, details);
    finish(error, null, uncertain);
    return error;
  };
  function stopTab() {
    if (!tab.page.isClosed())
      void timeout(tab.page.close({ runBeforeUnload: false }), 1000).catch(() => {
        if (!tab.page.isClosed()) tab.state = 'interrupted';
      });
  }
  async function expire() {
    if (settled) return;
    const dialogs = [...tab.dialogs.entries()].filter(
      ([, d]) => Date.parse(d.info.deadline_at) <= Date.now(),
    );
    if (dialogs.length) {
      await timeout(
        Promise.allSettled(dialogs.map(([key]) => tab.respond(key, 'dismiss', undefined, true))),
        250,
      ).catch(() => {});
    }
    // The expiry handler may already have sent dismiss on the control channel.
    if (op.continuationEnd) await timeout(job, 200).catch(() => {});
    if (!settled) {
      fail(new Fault('TIMEOUT', 'Backend continuation deadline elapsed.'), true);
      stopTab();
    }
  }
  deadline = setTimeout(() => void expire(), hard);
  const job = actionEffect(tab, action, a, hard).then(
    (effect) => {
      const value = receipt(effect.unchanged ? 'unchanged' : 'completed', effect.result);
      finish(null, value, false);
      return value;
    },
    (e) => {
      throw fail(e);
    },
  );
  try {
    const result = await timeout(
      control ? job : Promise.race([job, blocked.then(() => null)]),
      ms,
      signal,
    );
    if (result) return result;
    if (settled) return await job;
    const blockers = [...tab.dialogs.keys()].map((id) => ({ kind: 'dialog', id }));
    if (!blockers.length) return await timeout(job, Math.max(1, ms - (Date.now() - start)), signal);
    const end = Math.min(
      start + hard,
      Date.now() + cfg.blocked_operation_timeout_ms,
      ...[...tab.dialogs.values()].map((d) => Date.parse(d.info.deadline_at)),
    );
    clearTimeout(deadline);
    op.continuationEnd = end;
    deadline = setTimeout(() => void expire(), Math.max(1, end - Date.now()));
    Object.assign(op.data, {
      state: 'blocked',
      blocked_by: blockers,
      deadline_at: new Date(end).toISOString(),
    });
    return { ...receipt('pending', null), blocked_by: blockers, deadline_at: op.data.deadline_at };
  } catch (e) {
    if (!settled) {
      const error = fail(e, true);
      stopTab();
      throw error;
    }
    throw e;
  }
}
