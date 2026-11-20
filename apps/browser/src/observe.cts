import { Data, Fault, assert, bound, id, wire } from './contract.cjs';
import { Tab, Snapshot, TargetNode } from './state.cjs';
import type { Frame } from 'playwright-core';

// Runs in the selected frame; no caller-provided page script is accepted.
function describe(node: Element) {
  const labelDeadline = performance.now() + 12;
  const labelText = (root: Element | null) => {
    if (!root) return '';
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ALL);
    let text = '',
      visits = 0;
    while (
      text.length < 1026 &&
      visits++ < 256 &&
      performance.now() < labelDeadline &&
      walker.nextNode()
    )
      if (
        walker.currentNode.nodeType === Node.TEXT_NODE &&
        !walker.currentNode.parentElement?.closest('script,style,template,noscript')
      )
        text += (walker.currentNode as Text).substringData(0, 1026 - text.length);
    return text;
  };
  const tag = node.tagName.toLowerCase(),
    input = node as HTMLInputElement,
    type = input.type;
  const role =
    node.getAttribute('role')?.split(/\s+/)[0] ||
    (tag === 'input'
      ? (
          {
            checkbox: 'checkbox',
            radio: 'radio',
            button: 'button',
            submit: 'button',
            reset: 'button',
            range: 'slider',
            number: 'spinbutton',
            search: 'searchbox',
          } as Record<string, string>
        )[type] || 'textbox'
      : (
          {
            button: 'button',
            a: 'link',
            textarea: 'textbox',
            select: (node as HTMLSelectElement).multiple ? 'listbox' : 'combobox',
            option: 'option',
            img: 'img',
            summary: 'button',
            h1: 'heading',
            h2: 'heading',
            h3: 'heading',
            h4: 'heading',
            h5: 'heading',
            h6: 'heading',
          } as Record<string, string>
        )[tag] || (node.hasAttribute('contenteditable') ? 'textbox' : 'generic'));
  const ids =
    node.getAttribute('aria-labelledby')?.slice(0, 2048).trim().split(/\s+/).slice(0, 16) ?? [];
  const label = Array.prototype.slice
    .call(input.labels ?? [], 0, 16)
    .map((l: Element) => labelText(l))
    .join(' ')
    .trim();
  const name = (
    ids
      .map((key) => labelText(document.getElementById(key)))
      .join(' ')
      .trim() ||
    node.getAttribute('aria-label') ||
    label ||
    node.getAttribute('alt') ||
    (['button', 'link', 'option', 'heading'].includes(role) ? labelText(node) : '') ||
    (tag === 'input' && ['submit', 'reset', 'button'].includes(type) ? input.value : '') ||
    node.getAttribute('title') ||
    ''
  )
    .slice(0, 1026)
    .replace(/\s+/g, ' ')
    .trim();
  const chars = Array.from(name),
    states: Record<string, boolean> = {};
  if ('disabled' in node) states.disabled = !!input.disabled;
  if (tag === 'input' && ['checkbox', 'radio'].includes(type)) states.checked = input.checked;
  if (tag === 'option') states.selected = (node as HTMLOptionElement).selected;
  if (node.hasAttribute('aria-expanded'))
    states.expanded = node.getAttribute('aria-expanded') === 'true';
  if (tag === 'textarea' || tag === 'input') {
    states.readonly = !!input.readOnly;
    states.editable = !input.readOnly && !input.disabled;
  }
  const href = tag === 'a' ? (node as HTMLAnchorElement).href : undefined;
  return {
    role: role.slice(0, 64),
    name: chars.slice(0, 512).join(''),
    name_truncated: chars.length > 512,
    states,
    ...(label ? { label: Array.from(label).slice(0, 512).join('') } : {}),
    ...(href ? { href: href.length <= 8192 ? href : null } : {}),
  };
}
const selector =
  'a[href],button,input:not([type=hidden]),select,textarea,option,img,summary,h1,h2,h3,h4,h5,h6,[role],[contenteditable],[tabindex]';
export async function snapshot(tab: Tab, a: Data, callId: string, signal: AbortSignal) {
  assert(!tab.dialogs.size, 'TAB_BUSY', 'A browser dialog is blocking this page.');
  await tab.sync();
  const cfg = tab.session.cfg.limits,
    doc = tab.document;
  const maxChars = Math.min(a.max_chars ?? 12000, cfg.max_snapshot_chars);
  const maxNodes = Math.min(a.max_nodes ?? 400, cfg.max_snapshot_nodes);
  const deadline =
    Date.now() +
    Math.max(1, Math.min(a.timeout_ms ?? cfg.operation_timeout_ms, cfg.operation_timeout_ms) - 30);
  const reasons = new Set<string>(),
    elements: Data[] = [];
  let text = '';
  const snap: Snapshot = { until: Date.now() + cfg.snapshot_ttl_ms, refs: new Map() };
  const snapshotId = id();
  tab.snapshots.set(snapshotId, snap);
  while (tab.snapshots.size > cfg.max_snapshots_per_tab)
    tab.dropSnapshot(tab.snapshots.keys().next().value!);
  const scope = a.scope ? await tab.resolve(a.scope) : null;
  try {
    const frames = scope
      ? [await scope.ownerFrame()].filter((f): f is Frame => !!f)
      : tab.page.frames();
    if (frames.length > cfg.max_frames_per_tab) reasons.add('frame_limit');
    for (const frame of frames.slice(0, cfg.max_frames_per_tab)) {
      if (Date.now() >= deadline) {
        reasons.add('timeout');
        break;
      }
      if (signal.aborted) throw new Fault('CANCELLED', 'Snapshot cancelled.');
      await tab.refresh(frame);
      const record = tab.frames.get(frame);
      if (!record) {
        reasons.add('inaccessible_frame');
        continue;
      }
      let root = scope;
      if (!root) root = await frame.$('body').catch(() => null);
      if (!root) {
        reasons.add('inaccessible_frame');
        continue;
      }
      try {
        const remain = maxChars - Array.from(text).length;
        const limit = maxNodes - elements.length;
        const scan = await root.evaluateHandle(
          (e, p) => {
            const items: Element[] = [];
            let current: Node | null = e,
              visited = 0,
              body = '',
              chars = 0;
            const flags = new Set<string>();
            const end = performance.now() + p.millis;
            // Visit all node types explicitly so even a subtree with zero matches has bounded cost.
            while (current) {
              if (++visited > p.maxVisits) {
                flags.add('node_limit');
                break;
              }
              if (performance.now() >= end) {
                flags.add('timeout');
                break;
              }
              let children = true;
              if (current instanceof Element) {
                const visible =
                  !current.matches('script,style,noscript,template,[hidden]') &&
                  getComputedStyle(current).display !== 'none';
                children = visible;
                if (
                  visible &&
                  current.matches(p.selector) &&
                  current.getClientRects().length &&
                  getComputedStyle(current).visibility !== 'hidden'
                ) {
                  if (items.length < p.limit) items.push(current);
                  else flags.add('node_limit');
                }
              } else if (
                current instanceof Text &&
                current.parentElement &&
                getComputedStyle(current.parentElement).visibility !== 'hidden'
              ) {
                if (chars < p.remain) {
                  const remaining = p.remain - chars,
                    raw = current.substringData(0, remaining * 2 + 2);
                  const normalized = raw.replace(/\s+/g, ' ').trim();
                  const charsHere = Array.from((body && normalized ? ' ' : '') + normalized);
                  if (charsHere.length > remaining || raw.length < current.length)
                    flags.add('text_limit');
                  body += charsHere.slice(0, remaining).join('');
                  chars += Math.min(remaining, charsHere.length);
                } else if (current.length) flags.add('text_limit');
              }
              if (children && current.firstChild) {
                current = current.firstChild;
                continue;
              }
              while (current !== e && !current.nextSibling) current = current.parentNode!;
              current = current === e ? null : current.nextSibling;
            }
            return { items, text: body, reasons: [...flags] };
          },
          {
            selector,
            limit,
            remain,
            maxVisits: Math.min(32000, Math.max(512, maxNodes * 24)),
            millis: Math.max(1, Math.min(100, deadline - Date.now())),
          },
        );
        const summary = await scan.evaluate((scan) => ({ text: scan.text, reasons: scan.reasons }));
        for (const reason of summary.reasons) reasons.add(reason);
        const joined = (text && summary.text ? '\n' : '') + summary.text;
        if (Array.from(joined).length > remain) reasons.add('text_limit');
        text += bound(joined, remain);
        const array = await scan.getProperty('items');
        await scan.dispose();
        const handles = await array.getProperties();
        await array.dispose();
        const remainingHandles = [...handles.values()];
        for (const [index, h] of remainingHandles.entries()) {
          if (signal.aborted || Date.now() >= deadline) {
            reasons.add('timeout');
            void Promise.allSettled(
              remainingHandles.slice(index).map((handle) => handle.dispose()),
            );
            break;
          }
          const node = h.asElement() as TargetNode | null;
          if (!node || elements.length >= maxNodes || signal.aborted || Date.now() >= deadline) {
            if (Date.now() >= deadline) reasons.add('timeout');
            await h.dispose();
            if (elements.length >= maxNodes) reasons.add('node_limit');
            continue;
          }
          try {
            const description = await node.evaluate(describe);
            const ref = id();
            snap.refs.set(ref, { node, frame: record.id, document: doc });
            elements.push({ ref, frame_id: record.id, ...description });
          } catch {
            await node.dispose().catch(() => {});
            reasons.add('inaccessible_frame');
          }
        }
      } catch {
        reasons.add('inaccessible_frame');
      } finally {
        if (root !== scope) await root.dispose().catch(() => {});
      }
    }
    await tab.sync();
    assert(doc === tab.document, 'STALE_DOCUMENT', 'The document changed during observation.');
    assert(!signal.aborted, 'CANCELLED', 'Snapshot cancelled.');
    const result: Data = {
      session_id: tab.session.id,
      tab_id: tab.id,
      document_id: doc,
      snapshot_id: snapshotId,
      page: tab.pageInfo(),
      text,
      elements,
      frames: tab.frameInfo(),
      pending: tab.pending(),
      truncated: reasons.size > 0,
      truncation_reasons: [...reasons],
    };
    while (
      Buffer.byteLength(JSON.stringify(wire(callId, 'snapshot', result))) + 1 >
      cfg.max_result_frame_bytes
    ) {
      reasons.add('output_limit');
      result.truncated = true;
      result.truncation_reasons = [...reasons];
      if (elements.length) {
        for (const item of elements.splice(Math.floor(elements.length * 0.75))) {
          await snap.refs
            .get(item.ref)
            ?.node.dispose()
            .catch(() => {});
          snap.refs.delete(item.ref);
        }
      } else if (result.text.length)
        result.text = bound(result.text, Math.floor(Array.from(result.text).length * 0.75));
      else throw new Fault('RESULT_TOO_LARGE', 'Snapshot cannot fit the frame budget.');
    }
    return result;
  } catch (err) {
    tab.dropSnapshot(snapshotId);
    throw err;
  } finally {
    if (scope && a.scope.kind !== 'ref') await scope.dispose().catch(() => {});
  }
}
export async function waitFor(tab: Tab, c: Data, ms: number, signal: AbortSignal) {
  const start = Date.now(),
    cursor = tab.session.seq;
  const base = () => ({
    session_id: tab.session.id,
    tab_id: tab.id,
    document_id: tab.document,
    elapsed_ms: Date.now() - start,
    events: tab.session.events(cursor),
    pending: tab.pending(),
  });
  while (!signal.aborted) {
    if (tab.dialogs.size)
      return {
        ...base(),
        status: 'blocked',
        observation: null,
        blocked_by: [...tab.dialogs.keys()].map((id) => ({ kind: 'dialog', id })),
      };
    if (Date.now() - start >= ms) return { ...base(), status: 'timed_out', observation: null };
    await tab.sync();
    let matched = false;
    const obs: Data = { kind: c.kind };
    if (c.kind === 'url') {
      const url = tab.page.url();
      matched = c.match === 'prefix' ? url.startsWith(c.url) : url === c.url;
      obs.url = url;
    } else if (c.kind === 'load') {
      const state = c.state ?? 'domcontentloaded',
        ready = await tab.page.evaluate(() => document.readyState);
      matched = ready === 'complete' || (state === 'domcontentloaded' && ready === 'interactive');
      obs.load_state = state;
    } else if (c.kind === 'text') {
      const root = c.scope ? await tab.resolve(c.scope, true) : null;
      try {
        const value = root
          ? await root.innerText()
          : c.scope
            ? ''
            : await tab.page.evaluate(() => document.body?.innerText ?? '');
        const normal = (s: string) => s.replace(/\s+/g, ' ').trim();
        matched = normal(value).includes(normal(c.text)) !== (c.state === 'absent');
        obs.text = c.text;
      } finally {
        if (root && c.scope.kind !== 'ref') await root.dispose();
      }
    } else {
      const node = await tab.resolve(c.target, true),
        state = c.state ?? 'visible';
      try {
        const visible = node && (await node.isVisible());
        matched =
          state === 'attached'
            ? !!node
            : state === 'detached'
              ? !node
              : state === 'visible'
                ? !!visible
                : !visible;
        obs.element_state = state;
      } finally {
        if (node && c.target.kind !== 'ref') await node.dispose();
      }
    }
    if (matched) return { ...base(), status: 'matched', observation: obs };
    const remaining = ms - (Date.now() - start);
    if (remaining <= 0) return { ...base(), status: 'timed_out', observation: null };
    await new Promise((resolve) => setTimeout(resolve, Math.min(50, remaining)));
  }
  throw new Fault('CANCELLED', 'Wait cancelled.');
}
