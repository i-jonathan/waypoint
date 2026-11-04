// Waypoint runs entirely on localhost: no request may leave the machine. This
// scans the built UI for absolute http(s) URLs and fails on any that is not an
// XML/SVG namespace identifier or a URL inside a library comment.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../../internal/http/dist', import.meta.url));
const textExt = /\.(html|js|mjs|css|svg|json|webmanifest|txt)$/;
const urlRe = /https?:\/\/[^\s"'`)<>\\]+/g;

// Never fetched: XML namespace identifiers, and the documentation links Svelte
// puts inside its runtime error messages (text for a human, not a request).
const allowed = [
  'https://svelte.dev/e/',
  'http://www.w3.org/2000/svg',
  'http://www.w3.org/1999/xlink',
  'http://www.w3.org/1999/xhtml',
  'http://www.w3.org/1998/Math/MathML',
  'http://www.w3.org/XML/1998/namespace',
  'http://www.w3.org/2000/xmlns/',
];

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else if (textExt.test(name)) yield p;
  }
}

// True when the match sits inside a /* ... */ or // comment on its line.
function inComment(text, index) {
  const lineStart = text.lastIndexOf('\n', index) + 1;
  const before = text.slice(lineStart, index);
  if (/\/\*(?!.*\*\/)/.test(before) || /(^|\s)\/\/(?!.*:\/\/$)/.test(before)) return true;
  const open = text.lastIndexOf('/*', index);
  return open !== -1 && text.lastIndexOf('*/', index) < open;
}

const protocolRelative = /(?:url\(|src=|href=)\s*["']?\/\/[^\s"')]+/g;

const found = [];
for (const file of walk(dist)) {
  const text = readFileSync(file, 'utf8');
  for (const m of text.matchAll(urlRe)) {
    const url = m[0].replace(/[.,;]+$/, '');
    if (allowed.some((ns) => url.startsWith(ns))) continue;
    if (inComment(text, m.index)) continue;
    found.push(`${relative(dist, file)}: ${url}`);
  }
  for (const m of text.matchAll(protocolRelative)) found.push(`${relative(dist, file)}: ${m[0]}`);
}

if (found.length > 0) {
  console.error('External URLs in the built UI (Waypoint must stay 100% local):');
  for (const f of found) console.error('  ' + f);
  process.exit(1);
}
console.log('check-build: no external URLs in the built UI');
