// Run with NODE_PATH pointing to a module root containing @viz-js/viz 3.25.0.
import { createRequire } from 'node:module';
import { readFile, writeFile, readdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { createHash } from 'node:crypto';
const require = createRequire(import.meta.url);
const { instance } = require('@viz-js/viz');
const viz = await instance();
const dir = dirname(fileURLToPath(import.meta.url));
const hash = x => createHash('sha256').update(x).digest('hex');
const diagrams = [];
for (const source of (await readdir(dir)).filter(x=>x.endsWith('.dot')).sort()) {
  const input = await readFile(join(dir,source),'utf8');
  const svg = source.replace(/\.dot$/,'.svg');
  const output = viz.renderString(input,{format:'svg',engine:'dot'});
  await writeFile(join(dir,svg),output);
  diagrams.push({source,source_sha256:hash(input),svg,svg_sha256:hash(output)});
}
await writeFile(join(dir,'manifest.json'),JSON.stringify({version:'alpha-v0.7',generator:'Graphviz via @viz-js/viz 3.25.0',graphviz:viz.graphvizVersion,diagrams},null,2)+'\n');
console.log(`Rendered ${diagrams.length} precise SVG diagrams.`);
