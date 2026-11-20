'use strict';
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const cli = path.join(path.dirname(require.resolve('playwright-core/package.json')), 'cli.js');
const run = spawnSync(process.execPath, [cli, 'install', 'chromium', '--no-shell'], {
  cwd: root,
  stdio: 'inherit',
  env: { ...process.env, PLAYWRIGHT_BROWSERS_PATH: path.join(root, 'browsers') },
});
if (run.error) throw run.error;
process.exitCode = run.status ?? 1;
