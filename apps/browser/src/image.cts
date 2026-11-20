import { Worker, parentPort, workerData } from 'node:worker_threads';
import { PNG } from 'pngjs';
import jpeg from 'jpeg-js';
import { resolve } from 'node:path';
import { Data, Fault, assert, wire } from './contract.cjs';
import { Tab } from './state.cjs';

type Rect = { x: number; y: number; width: number; height: number };
type Job = {
  raw: Uint8Array;
  meta: Data;
  rect: Rect;
  clip: boolean;
  budget: number;
  callId: string;
};
function encode(job: Job) {
  const { width, height } = job.rect;
  function candidate(bytes: Buffer | Uint8Array, mime: string, w: number, h: number) {
    if (4 * Math.ceil(bytes.length / 3) > job.budget) return null;
    const result = {
      content: [
        { type: 'text', text: 'Viewport screenshot.' },
        { type: 'image', mimeType: mime, data: Buffer.from(bytes).toString('base64') },
      ],
      structuredContent: {
        ...job.meta,
        source_rect: job.rect,
        preview_width: w,
        preview_height: h,
        mime_type: mime,
        encoded_bytes: bytes.length,
        resized: w !== width || h !== height,
      },
    };
    return Buffer.byteLength(JSON.stringify(wire(job.callId, 'screenshot', result))) + 1 <=
      job.budget
      ? result
      : null;
  }
  const raw = Buffer.from(job.raw),
    viewport = job.meta.viewport;
  assert(
    raw.length >= 24 &&
      raw.readUInt32BE(16) === viewport.width &&
      raw.readUInt32BE(20) === viewport.height,
    'SCREENSHOT_FAILED',
    'Unexpected capture dimensions.',
  );
  const full =
    job.rect.x === 0 && job.rect.y === 0 && width === viewport.width && height === viewport.height;
  if (full) {
    const original = candidate(raw, 'image/png', width, height);
    if (original) return original;
  }
  const source = PNG.sync.read(raw);
  if (job.clip) {
    if (!full) {
      const cropped = new PNG({ width, height });
      PNG.bitblt(source, cropped, job.rect.x, job.rect.y, width, height, 0, 0);
      const result = candidate(PNG.sync.write(cropped), 'image/png', width, height);
      if (result) return result;
    }
    throw new Fault('IMAGE_PREVIEW_TOO_LARGE', 'Request a smaller clip.');
  }
  const jpegBytes = (rgba: Buffer, w: number, h: number) => {
    const opaque = Buffer.from(rgba);
    for (let k = 0; k < opaque.length; k += 4)
      if (opaque[k + 3] !== 255) {
        const a = opaque[k + 3] / 255;
        for (let c = 0; c < 3; c++) opaque[k + c] = Math.round(opaque[k + c] * a + 255 * (1 - a));
        opaque[k + 3] = 255;
      }
    return jpeg.encode({ width: w, height: h, data: opaque }, 80).data;
  };
  const originalJpeg = candidate(
    jpegBytes(source.data, width, height),
    'image/jpeg',
    width,
    height,
  );
  if (originalJpeg) return originalJpeg;
  const originalEdge = Math.max(width, height),
    floor = Math.min(originalEdge, 768);
  let edge = originalEdge;
  while (edge > floor) {
    edge = Math.max(floor, Math.floor(edge * 0.8));
    const w = Math.max(1, Math.round((width * edge) / originalEdge)),
      h = Math.max(1, Math.round((height * edge) / originalEdge));
    const scaled = new PNG({ width: w, height: h });
    for (let y = 0; y < h; y++)
      for (let x = 0; x < w; x++) {
        const sx = ((x + 0.5) * width) / w - 0.5,
          sy = ((y + 0.5) * height) / h - 0.5;
        const x0 = Math.floor(sx),
          y0 = Math.floor(sy),
          x1 = Math.min(x0 + 1, width - 1),
          y1 = Math.min(y0 + 1, height - 1);
        const fx = sx - x0,
          fy = sy - y0;
        for (let c = 0; c < 4; c++) {
          const sample = (px: number, py: number) => source.data[4 * (py * width + px) + c];
          scaled.data[4 * (y * w + x) + c] = Math.round(
            (1 - fy) * ((1 - fx) * sample(x0, y0) + fx * sample(x1, y0)) +
              fy * ((1 - fx) * sample(x0, y1) + fx * sample(x1, y1)),
          );
        }
      }
    const result =
      candidate(PNG.sync.write(scaled), 'image/png', w, h) ??
      candidate(jpegBytes(scaled.data, w, h), 'image/jpeg', w, h);
    if (result) return result;
  }
  throw new Fault('IMAGE_PREVIEW_TOO_LARGE', 'Request a smaller clip.');
}
if (parentPort) {
  try {
    parentPort.postMessage({ result: encode(workerData) });
  } catch (e) {
    parentPort.postMessage({
      error:
        e instanceof Fault
          ? e.data()
          : { code: 'SCREENSHOT_FAILED', message: 'Image encoding failed.' },
    });
  }
}
export async function screenshot(
  tab: Tab,
  args: Data,
  callId: string,
  ms: number,
  signal: AbortSignal,
) {
  assert(!tab.current && !tab.capturing, 'TAB_BUSY', 'A page operation is in progress.');
  tab.capturing = true;
  let raw: Buffer;
  let document: string;
  const viewport = tab.session.cfg.viewport;
  const rect: Rect = args.clip ?? { x: 0, y: 0, width: viewport.width, height: viewport.height };
  try {
    await tab.sync();
    document = tab.document;
    assert(
      rect.x + rect.width <= viewport.width && rect.y + rect.height <= viewport.height,
      'INVALID_ARGUMENT',
      'Clip extends outside the visible viewport.',
    );
    raw = await tab.page.screenshot({
      type: 'png',
      scale: 'css',
      fullPage: false,
      omitBackground: false,
      timeout: ms,
    });
    await tab.sync();
    assert(document === tab.document, 'STALE_DOCUMENT', 'The document changed during capture.');
  } finally {
    tab.capturing = false;
  }
  assert(!signal.aborted, 'CANCELLED', 'Screenshot cancelled.');
  const worker = new Worker(resolve(__dirname, 'image.cjs'), {
    workerData: {
      raw,
      rect,
      clip: args.clip !== undefined,
      budget: tab.session.cfg.limits.max_result_frame_bytes,
      callId,
      meta: { session_id: tab.session.id, tab_id: tab.id, document_id: document, viewport },
    },
  });
  try {
    return await new Promise<Data>((accept, reject) => {
      const abort = () => reject(new Fault('CANCELLED', 'Screenshot cancelled.'));
      signal.addEventListener('abort', abort, { once: true });
      worker.once('message', (m) =>
        m.error ? reject(new Fault(m.error.code, m.error.message)) : accept(m.result),
      );
      worker.once('error', () => reject(new Fault('SCREENSHOT_FAILED', 'Image worker failed.')));
      worker.once('exit', () => {
        signal.removeEventListener('abort', abort);
        reject(new Fault('SCREENSHOT_FAILED', 'Image worker exited.'));
      });
    });
  } finally {
    await worker.terminate();
  }
}
