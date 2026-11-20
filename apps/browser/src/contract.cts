import { readFileSync, realpathSync, statSync } from 'node:fs';
import { resolve, isAbsolute, relative, sep } from 'node:path';
import { randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import Ajv2020 from 'ajv/dist/2020';
import formats from 'ajv-formats';

// The JSON contract owns the shape of caller data and action-specific receipts.
export type Data = Record<string, any>;
const schema = JSON.parse(readFileSync(resolve(__dirname, '../contract/schemas.json'), 'utf8'));
const ajv = new Ajv2020({ strict: false, validateFormats: true });
formats(ajv);
ajv.addSchema(schema);
export const actions: string[] = Object.keys(schema.$defs)
  .filter((name) => name.endsWith('_input'))
  .map((name) => name.slice(0, -6));
export class Fault extends Error {
  constructor(
    public code: string,
    message: string,
    public details?: Data,
  ) {
    super(message);
  }
  data() {
    return {
      code: this.code,
      message: this.message,
      ...(this.details ? { details: this.details } : {}),
    };
  }
}
export function check(name: string, value: unknown) {
  const validator = ajv.getSchema(schema.$id + '#/$defs/' + name);
  if (!validator || !validator(value)) {
    const detail = validator?.errors?.[0];
    const code =
      name === 'config'
        ? 'INVALID_CONFIG'
        : name.endsWith('_input')
          ? 'INVALID_ARGUMENT'
          : 'INTERNAL_ERROR';
    throw new Fault(
      code,
      'Invalid ' +
        name +
        (detail ? ' at ' + detail.instancePath + ' (' + detail.keyword + ')' : ''),
    );
  }
}
export const id = () => 'r_' + randomUUID();
export const now = () => new Date().toISOString();
export function assert(ok: unknown, code: string, message: string): asserts ok {
  if (!ok) throw new Fault(code, message);
}
export function utf8(s: string, max: number) {
  return Buffer.byteLength(s) <= max && s.isWellFormed();
}
export function bound(s: string, n: number) {
  return Array.from(s).slice(0, n).join('');
}
function ownedDir(value: string) {
  assert(isAbsolute(value), 'INVALID_CONFIG', 'An absolute private directory is required.');
  // Match fs.promises.realpath in session cleanup, including Windows drive casing and 8.3 aliases.
  const path = realpathSync.native(value);
  const st = statSync(path);
  assert(st.isDirectory(), 'INVALID_CONFIG', 'Configured directory is unavailable.');
  if (process.getuid) {
    assert(
      st.uid === process.getuid() && (st.mode & 0o077) === 0,
      'INVALID_CONFIG',
      'A private directory owned by the worker is required.',
    );
  }
  return path;
}
function windowsPrivateDirs(dirs: string[]) {
  if (process.platform !== 'win32') return;
  const shell = resolve(
    process.env.SystemRoot ?? 'C:\\Windows',
    'System32/WindowsPowerShell/v1.0/powershell.exe',
  );
  const script = [
    "$ErrorActionPreference='Stop'",
    '$sid=[System.Security.Principal.SecurityIdentifier]',
    '$me=[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value',
    "$trusted=@($me,'S-1-5-18','S-1-5-32-544')",
    'foreach($dir in (ConvertFrom-Json -InputObject $env:OI_BROWSER_CHECK_DIRS)){',
    "  if($dir.StartsWith('\\\\') -or ([IO.DriveInfo]::new([IO.Path]::GetPathRoot($dir))).DriveType -ne 'Fixed'){exit 2}",
    '  $acl=[IO.Directory]::GetAccessControl($dir)',
    '  $descriptor=[System.Security.AccessControl.RawSecurityDescriptor]::new($acl.GetSecurityDescriptorBinaryForm(),0)',
    '  if($null -eq $descriptor.DiscretionaryAcl){exit 5}',
    '  if($trusted -notcontains $acl.GetOwner($sid).Value){exit 3}',
    '  foreach($r in $acl.GetAccessRules($true,$true,$sid)){',
    "    if($r.AccessControlType -eq 'Allow' -and $trusted -notcontains $r.IdentityReference.Value){",
    "      if($r.IdentityReference.Value -eq 'S-1-3-0' -and ($r.PropagationFlags -band 2)){continue}",
    '      exit 4',
    '    }',
    '  }',
    '}',
  ].join('\n');
  const test = spawnSync(shell, ['-NoProfile', '-NonInteractive', '-Command', script], {
    windowsHide: true,
    stdio: 'ignore',
    timeout: 5000,
    env: { ...process.env, OI_BROWSER_CHECK_DIRS: JSON.stringify(dirs) },
  });
  assert(
    test.status === 0,
    'INVALID_CONFIG',
    'Windows state and profile directories must have private ownership and access rules.',
  );
}
export function config(input: Data) {
  check('config', input);
  const limits: Record<string, number> = {};
  for (const [key, val] of Object.entries(schema.$defs.config.properties.limits.properties))
    limits[key] = input.limits?.[key] ?? (val as Data).default;
  const viewport = { width: 1280, height: 720, device_scale_factor: 1, ...input.viewport };
  const state_dir = ownedDir(input.state_dir);
  const dirs = [state_dir],
    aliases = new Set<string>();
  const profiles: Data[] = (input.profiles ?? []).map((profile: Data) => {
    const path = ownedDir(profile.path);
    const contains = (a: string, b: string) => {
      const rel = relative(a, b);
      return !isAbsolute(rel) && rel !== '..' && !rel.startsWith('..' + sep);
    };
    assert(
      !aliases.has(profile.alias) && dirs.every((d) => !contains(d, path) && !contains(path, d)),
      'INVALID_CONFIG',
      'Profile aliases and directories must be distinct.',
    );
    aliases.add(profile.alias);
    dirs.push(path);
    return { ...profile, path };
  });
  windowsPrivateDirs(dirs);
  return { state_dir, profiles, viewport, limits };
}
export type Config = ReturnType<typeof config>;
export function address(url: string) {
  if (url === 'about:blank') return url;
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    throw new Fault('UNSUPPORTED_URL', 'Invalid URL.');
  }
  assert(
    ['http:', 'https:'].includes(parsed.protocol) && utf8(url, 8192),
    'UNSUPPORTED_URL',
    'Only HTTP, HTTPS and about:blank are accepted.',
  );
  return url;
}
export function failure(err: unknown, fallback = 'INTERNAL_ERROR'): Fault {
  if (err instanceof Fault) return err;
  if (err instanceof Error && err.name === 'TimeoutError')
    return new Fault('TIMEOUT', 'Browser operation timed out.');
  // Driver error messages may contain URLs, form text and Playwright call logs.
  return new Fault(fallback, 'Browser operation could not be completed.');
}
export function timeout<T>(promise: Promise<T>, ms: number, signal?: AbortSignal): Promise<T> {
  return new Promise((accept, reject) => {
    const abort = () => reject(new Fault('CANCELLED', 'Call cancelled.'));
    const timer = setTimeout(
      () => reject(new Fault('TIMEOUT', 'Call deadline elapsed.')),
      Math.max(1, ms),
    );
    if (signal?.aborted) abort();
    else signal?.addEventListener('abort', abort, { once: true });
    promise.then(accept, reject).finally(() => {
      clearTimeout(timer);
      signal?.removeEventListener('abort', abort);
    });
  });
}
export function wire(callId: string, action: string, result: Data) {
  return {
    type: 'result',
    call_id: callId,
    ...(action === 'screenshot' ? { result_format: 'content.v1' } : {}),
    result,
  };
}
