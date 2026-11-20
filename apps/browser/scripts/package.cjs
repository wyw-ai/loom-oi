'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { createReadStream } = require('node:fs');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const version = 'v24.13.0';
async function hash(file) {
  const digest = createHash('sha256');
  for await (const block of createReadStream(file)) digest.update(block);
  return digest.digest('hex');
}
async function files(dir) {
  const out = [];
  for (const e of await fs.readdir(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...(await files(p)));
    else if (e.isFile()) out.push(p);
  }
  return out.sort();
}
async function build() {
  if (
    process.arch !== 'x64' ||
    !['win32', 'linux'].includes(process.platform) ||
    process.version !== version
  )
    throw Error('Use the locked Node.js ' + version + ' on Windows x64 or Linux x64.');
  process.env.PLAYWRIGHT_BROWSERS_PATH = path.join(root, 'browsers');
  const { chromium } = require('playwright-core');
  const executable = chromium.executablePath();
  await fs.access(executable);
  const platform = process.platform === 'win32' ? 'windows' : 'linux',
    name = 'browser-' + platform + '-x64';
  const dist = path.join(root, 'dist');
  await fs.mkdir(dist, { recursive: true });
  const temp = await fs.mkdtemp(path.join(dist, '.package-'));
  const pkg = path.join(temp, name);
  await fs.mkdir(pkg);
  await fs.mkdir(path.join(pkg, 'runtime'));
  await fs.copyFile(
    process.execPath,
    path.join(pkg, 'runtime', platform === 'windows' ? 'node.exe' : 'node'),
  );
  const lock = JSON.parse(await fs.readFile(path.join(root, 'package-lock.json'), 'utf8'));
  const packageJSON = JSON.parse(await fs.readFile(path.join(root, 'package.json'), 'utf8'));
  const deps = {};
  for (const [relative, entry] of Object.entries(lock.packages)) {
    if (!relative || entry.dev) continue;
    if (!relative.startsWith('node_modules/') || relative.includes('..'))
      throw Error('Unexpected dependency location.');
    const out = path.join(pkg, 'worker', relative);
    await fs.mkdir(path.dirname(out), { recursive: true });
    await fs.cp(path.join(root, relative), out, { recursive: true });
    deps[relative] = { version: entry.version, integrity: entry.integrity };
  }
  for (const dir of ['worker', 'contract'])
    await fs.cp(path.join(root, dir), path.join(pkg, dir), { recursive: true });
  await fs.cp(path.join(root, 'browsers'), path.join(pkg, 'browsers'), {
    recursive: true,
    filter: (src) =>
      !['.links', '__dirlock', '.cache', '%SystemDrive%'].includes(path.basename(src)),
  });
  let manifest = await fs.readFile(path.join(root, 'app.yaml'), 'utf8');
  if (platform === 'windows')
    manifest = manifest.replace('command: [./runtime/node,', 'command: [./runtime/node.exe,');
  await fs.writeFile(path.join(pkg, 'app.yaml'), manifest);
  await fs.copyFile(path.join(root, 'README.md'), path.join(pkg, 'README.md'));
  await fs.copyFile(path.join(root, 'package-lock.json'), path.join(pkg, 'package-lock.json'));
  const licenses = path.join(pkg, 'LICENSES');
  await fs.mkdir(licenses);
  // npm packages and Chromium retain their original license and notice files.
  const bin = path.dirname(process.execPath);
  const candidates = process.env.OI_NODE_LICENSE
    ? [process.env.OI_NODE_LICENSE]
    : [
        path.join(bin, 'LICENSE'),
        path.join(bin, '../LICENSE'),
        path.join(bin, '../share/doc/node/LICENSE'),
        path.join(root, 'licenses/Node-LICENSE'),
      ];
  let nodeLicense;
  for (const candidate of candidates) {
    try {
      await fs.access(candidate);
      nodeLicense = candidate;
      break;
    } catch {}
  }
  if (!nodeLicense) throw Error('Provide the official Node.js license via OI_NODE_LICENSE.');
  await fs.copyFile(nodeLicense, path.join(licenses, 'Node-LICENSE'));
  const registry = JSON.parse(
    await fs.readFile(path.join(root, 'node_modules/playwright-core/browsers.json'), 'utf8'),
  );
  const chromiumLock = registry.browsers.find((b) => b.name === 'chromium');
  const git = spawnSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' });
  const sourceDigest = createHash('sha256');
  const inputs = ['package.json', 'package-lock.json', 'tsconfig.json', 'app.yaml', 'README.md'];
  for (const dir of ['src', 'contract', 'scripts', 'licenses', 'test'])
    inputs.push(...(await files(path.join(root, dir))).map((f) => path.relative(root, f)));
  for (const input of inputs.sort())
    sourceDigest.update(
      input.split(path.sep).join('/') + '\0' + (await hash(path.join(root, input))) + '\n',
    );
  const packagedVersion = chromiumLock.browserVersion;
  const probe = await chromium.launch({
    channel: 'chromium',
    headless: true,
    chromiumSandbox: true,
  });
  try {
    if (probe.version() !== packagedVersion)
      throw Error('Unexpected Chromium version: ' + probe.version());
    const legal = await probe.newPage();
    for (const page of ['credits', 'terms']) {
      await legal.goto('chrome://' + page + '/');
      const html = await legal.content();
      if (html.length < 1000) throw Error('Missing Chromium ' + page + '.');
      await fs.writeFile(path.join(licenses, 'Chromium-' + page + '.html'), html);
    }
  } finally {
    await probe.close();
  }
  await fs.writeFile(
    path.join(pkg, 'release-lock.json'),
    JSON.stringify(
      {
        node: process.version,
        node_sha256: await hash(process.execPath),
        platform,
        arch: process.arch,
        source_commit:
          process.env.OI_SOURCE_COMMIT || (git.status === 0 ? git.stdout.trim() : null),
        app_source_sha256: sourceDigest.digest('hex'),
        chromium: chromiumLock,
        command: 'npm run package',
        npm: deps,
        app_version: packageJSON.version,
      },
      null,
      2,
    ) + '\n',
  );
  const checks = [];
  for (const file of await files(pkg))
    checks.push((await hash(file)) + '  ' + path.relative(pkg, file).split(path.sep).join('/'));
  await fs.writeFile(path.join(pkg, 'checksums.txt'), checks.join('\n') + '\n');
  const archive = name + (platform === 'windows' ? '.zip' : '.tar.gz');
  const packed = spawnSync(
    'tar',
    [platform === 'windows' ? '-acf' : '-czf', path.join(temp, archive), '-C', temp, name],
    { stdio: 'inherit' },
  );
  if (packed.status !== 0) throw Error('Archiving failed.');
  const digest = await hash(path.join(temp, archive));
  await fs.rename(path.join(temp, archive), path.join(dist, archive));
  await fs.writeFile(path.join(dist, archive + '.sha256'), digest + '  ' + archive + '\n');
  console.log(path.join(dist, archive), digest);
  await fs.rm(temp, { recursive: true, force: true });
}
build().catch((err) => {
  console.error(err.message);
  process.exitCode = 1;
});
