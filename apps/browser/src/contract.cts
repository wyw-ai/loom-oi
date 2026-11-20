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
const windowsAclFailures: Record<string, string> = {
  2: 'Windows state and profile directories must be on a local fixed drive.',
  3: 'Windows directory owner is outside the trusted owner set.',
  4: 'Windows directory ACL grants access to an untrusted principal.',
  5: 'Windows directory has a null discretionary ACL.',
  6: 'Access denied while reading a Windows directory security descriptor.',
  7: 'Could not read a Windows directory security descriptor.',
  8: 'Windows directory ACL check received an incomplete path list.',
  ETIMEDOUT: 'Windows directory ACL check timed out (ETIMEDOUT).',
  EACCES: 'Access denied while starting the Windows directory ACL check.',
  SPAWN: 'Could not start the Windows directory ACL check.',
  UNKNOWN: 'Windows directory ACL check did not complete.',
};
class WindowsAclError extends Fault {
  constructor(readonly category: string) {
    super('INVALID_CONFIG', windowsAclFailures[category] ?? windowsAclFailures.UNKNOWN);
  }
}
export function startupDiagnostic(err: unknown) {
  if (err instanceof WindowsAclError)
    return 'INVALID_CONFIG: ' + (windowsAclFailures[err.category] ?? windowsAclFailures.UNKNOWN);
  if (err instanceof Fault && ['INVALID_CONFIG', 'INVALID_ARGUMENT'].includes(err.code))
    return err.code;
  return 'protocol or initialization error';
}
function windowsPrivateDirs(dirs: string[]) {
  if (process.platform !== 'win32') return;
  const shell = resolve(
    process.env.SystemRoot ?? 'C:\\Windows',
    'System32/WindowsPowerShell/v1.0/powershell.exe',
  );
  // The ACL probe uses only .NET, including in an OIC worker with a filtered environment.
  const script = String.raw`
$ErrorActionPreference = 'Stop'
$PSModuleAutoLoadingPreference = 'None'
try {
    $sid = [System.Security.Principal.SecurityIdentifier]
    $me = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $trusted = @($me, 'S-1-5-18', 'S-1-5-32-544')
    $reader = [IO.StreamReader]::new([Console]::OpenStandardInput(), [Text.Encoding]::UTF8)
    $checked = 0
    $complete = $false
    while ($null -ne ($line = $reader.ReadLine())) {
        if ($line -ceq '!') { $complete = $true; break }
        $dir = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($line))
        if ($dir.StartsWith('\\') -or ([IO.DriveInfo]::new([IO.Path]::GetPathRoot($dir))).DriveType -ne 'Fixed') { exit 2 }
        $acl = [IO.Directory]::GetAccessControl($dir)
        $descriptor = [System.Security.AccessControl.RawSecurityDescriptor]::new($acl.GetSecurityDescriptorBinaryForm(), 0)
        if ($null -eq $descriptor.DiscretionaryAcl) { exit 5 }
        if ($trusted -notcontains $acl.GetOwner($sid).Value) { exit 3 }
        foreach ($rule in $acl.GetAccessRules($true, $true, $sid)) {
            if ($rule.AccessControlType -eq 'Allow' -and $trusted -notcontains $rule.IdentityReference.Value) {
                if ($rule.IdentityReference.Value -eq 'S-1-3-0' -and ($rule.PropagationFlags -band 2)) { continue }
                exit 4
            }
        }
        $checked++
    }
    if (-not $complete -or $checked -ne ${dirs.length}) { exit 8 }
} catch {
    if ($_.Exception.GetBaseException() -is [UnauthorizedAccessException]) { exit 6 }
    exit 7
}
`;
  // One UTF-8 path per Base64 line. A missing path or end marker fails closed.
  const input = dirs
    .map((dir) => Buffer.from(dir, 'utf8').toString('base64'))
    .concat('!', '')
    .join('\n');
  const test = spawnSync(shell, ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', script], {
    windowsHide: true,
    input,
    stdio: ['pipe', 'ignore', 'ignore'],
    // OIC waits ten seconds for the entire handshake, including starting Node and validating the config.
    timeout: 7000,
  });
  if (test.status === 0) return;
  const code = (test.error as NodeJS.ErrnoException | undefined)?.code;
  if (code === 'EACCES' || code === 'EPERM') throw new WindowsAclError('EACCES');
  if (code) throw new WindowsAclError(code === 'ETIMEDOUT' ? 'ETIMEDOUT' : 'SPAWN');
  throw new WindowsAclError(String(test.status));
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
