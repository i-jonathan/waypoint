// Waypoint runs entirely on localhost: no request may leave the machine. This
// scans the built UI and fails on any absolute http(s) URL that is not an
// XML/SVG namespace identifier, and on any protocol-relative host such as
// fetch("//host/x").
//
// The built JS is a few huge lines, and a "/*" or " // " inside a string would
// hide everything after it from a line-based comment check. So JS is never
// given a comment exemption: a URL in a JS comment fails too. Only CSS and HTML
// comments are exempt, and those are cut out with a real comment scan.
//
// Usage: node scripts/check-build.mjs [dir]   (default: the embedded dist)
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = process.argv[2]
  ? resolve(process.argv[2])
  : fileURLToPath(new URL('../../internal/http/dist', import.meta.url));
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

// A protocol-relative host: after a loading call, or anywhere as a string, or in CSS.
const host = String.raw`[a-z0-9-]+(?:\.[a-z0-9-]+)*(?::\d+)?`;
const protocolRelative = [
  new RegExp(String.raw`(?:fetch|import|new\s+URL|new\s+Request|new\s+EventSource|new\s+WebSocket|new\s+Worker|new\s+SharedWorker)\s*\(\s*["'\x60]\/\/[^\s"'\x60)]*`, 'gi'),
  new RegExp(String.raw`["'\x60]\/\/${host}(?=[\/?#"'\x60])[^\s"'\x60)]*`, 'gi'),
  /(?:url\(|src=|href=)\s*["']?\/\/[^\s"')]+/gi,
];

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else if (textExt.test(name)) yield p;
  }
}

// Blanks out the comments of CSS and HTML, scanning left to right so that a
// comment marker inside a CSS string is not taken for a comment.
function withoutComments(file, text) {
  if (/\.css$/.test(file)) return cutCss(text);
  if (/\.(html|svg)$/.test(file)) return text.replace(/<!--[\s\S]*?-->/g, (c) => ' '.repeat(c.length));
  return text; // JS and the rest: no exemption
}

function cutCss(text) {
  let out = '';
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < text.length && text[j] !== c) j += text[j] === '\\' ? 2 : 1;
      out += text.slice(i, j + 1);
      i = j + 1;
    } else if (c === '/' && text[i + 1] === '*') {
      const end = text.indexOf('*/', i + 2);
      const stop = end === -1 ? text.length : end + 2;
      out += ' '.repeat(stop - i);
      i = stop;
    } else {
      out += c;
      i++;
    }
  }
  return out;
}

const found = [];
for (const file of walk(dist)) {
  const text = withoutComments(file, readFileSync(file, 'utf8'));
  const name = relative(dist, file);
  for (const m of text.matchAll(urlRe)) {
    const url = m[0].replace(/[.,;]+$/, '');
    if (allowed.some((ns) => url.startsWith(ns))) continue;
    found.push(`${name}: ${url}`);
  }
  for (const re of protocolRelative) for (const m of text.matchAll(re)) found.push(`${name}: ${m[0]}`);
}

if (found.length > 0) {
  console.error('External URLs in the built UI (Waypoint must stay 100% local):');
  for (const f of new Set(found)) console.error('  ' + f);
  process.exit(1);
}
console.log('check-build: no external URLs in the built UI');
