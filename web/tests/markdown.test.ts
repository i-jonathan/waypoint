// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { checklistLines } from '../src/lib/checklist';
import { renderBody } from '../src/lib/markdown';

function html(body: string): string {
  const div = document.createElement('div');
  div.append(renderBody(body));
  return div.innerHTML;
}

function boxes(body: string): Array<{ n: number; checked: boolean; label: string }> {
  const div = document.createElement('div');
  div.append(renderBody(body));
  return [...div.querySelectorAll<HTMLInputElement>('input[type=checkbox]')].map((i) => ({
    n: Number(i.dataset.n),
    checked: i.checked,
    label: i.getAttribute('aria-label') ?? '',
  }));
}

describe('hostile bodies', () => {
  const bodies: Record<string, string> = {
    'img onerror': '<img src=x onerror="window.__pwn=1">',
    'svg onload': '<svg onload="window.__pwn=1"><circle/></svg>',
    'anchor javascript': '<a href="javascript:window.__pwn=1">go</a>',
    'markdown javascript link': '[x](javascript:window.__pwn=1)',
    'markdown javascript link, entity': '[x](&#106;avascript:window.__pwn=1)',
    'data link': '[x](data:text/html;base64,PHNjcmlwdD4=)',
    'comment trick': '<!--><img src=x onerror="window.__pwn=1">-->',
    'comment trick 2': '<!-- --!><img src=x onerror="window.__pwn=1">',
    script: 'before <script>window.__pwn=1</script> after',
    style: '<style>body{display:none}</style>text',
    iframe: '<iframe src="https://example.com"></iframe>',
    object: '<object data="x"></object><embed src="x">',
    form: '<form action="https://evil.example"><input name=a><button>go</button></form>',
    'event on allowed tag': '<p onclick="window.__pwn=1" style="color:red" class="skip" id="x">hi</p>',
    'math/mXSS': '<math><mtext><table><mglyph><style><img src=x onerror="window.__pwn=1">',
  };

  for (const [name, body] of Object.entries(bodies)) {
    it(`neutralises ${name}`, () => {
      const out = html(body);
      expect(out).not.toMatch(/<script|<style|<iframe|<object|<embed|<form|<img|<svg|<input|<math/i);
      expect(out).not.toMatch(/\son\w+\s*=/i);
      expect(out).not.toMatch(/javascript:/i);
      expect(out).not.toMatch(/href=["']?data:/i);
      expect(out).not.toMatch(/\s(style|class|id)=/i);
      expect((window as unknown as { __pwn?: number }).__pwn).toBeUndefined();
    });
  }

  it('keeps the text around a stripped script', () => {
    expect(html('before <script>alert(1)</script> after')).toContain('before');
  });
});

describe('links and images', () => {
  it('opens links in a new tab without leaking the opener', () => {
    const div = document.createElement('div');
    div.append(renderBody('[docs](https://example.com/a?b=1) and <https://example.org> and [m](mailto:a@b.co)'));
    const links = [...div.querySelectorAll('a')];
    expect(links).toHaveLength(3);
    for (const a of links) {
      expect(a.getAttribute('target')).toBe('_blank');
      expect(a.getAttribute('rel')).toBe('noopener noreferrer');
    }
  });

  it('leaves a relative link without a destination', () => {
    const div = document.createElement('div');
    div.append(renderBody('[x](docs/a.md) [y](/etc/passwd) [z](#top)'));
    for (const a of div.querySelectorAll('a')) expect(a.hasAttribute('href')).toBe(false);
  });

  it('shows a remote image as a link with its alt text and host, and never as an image', () => {
    const div = document.createElement('div');
    div.append(renderBody('![the chart](https://cdn.example.com/x.png)'));
    expect(div.querySelector('img')).toBeNull();
    const a = div.querySelector('a')!;
    expect(a.textContent).toBe('the chart (cdn.example.com)');
    expect(a.getAttribute('href')).toBe('https://cdn.example.com/x.png');
    expect(a.getAttribute('target')).toBe('_blank');
  });

  it('shows a protocol-relative image by its host too', () => {
    const div = document.createElement('div');
    div.append(renderBody('![](//cdn.example.com/x.png)'));
    expect(div.querySelector('img')).toBeNull();
    expect(div.textContent).toContain('cdn.example.com');
  });

  it('shows a relative or data image as its alt text', () => {
    const div = document.createElement('div');
    div.append(renderBody('![local pic](pics/a.png) ![inline](data:image/png;base64,AAAA)'));
    expect(div.querySelector('img')).toBeNull();
    expect(div.querySelector('a')).toBeNull();
    expect(div.textContent).toContain('local pic');
    expect(div.textContent).toContain('inline');
  });

  it('escapes an alt text that looks like HTML', () => {
    const div = document.createElement('div');
    div.append(renderBody('![<b onmouseover=1>x</b>](https://h.example/x.png)'));
    expect(div.querySelector('b')).toBeNull();
  });
});

describe('rendering', () => {
  it('renders the usual markdown', () => {
    const out = html('# Title\n\nsome **bold** and `code`\n\n- a\n- b\n\n| h |\n|---|\n| c |\n\n```\nfence\n```');
    expect(out).toContain('<h1>');
    expect(out).toContain('<strong>bold</strong>');
    expect(out).toContain('<code>code</code>');
    expect(out).toContain('<table>');
    expect(out).toContain('<pre>');
  });

  it('renders an empty body as nothing', () => {
    expect(html('')).toBe('');
  });

  it('strips the private marks a body might carry to forge a checkbox', () => {
    expect(boxes('7:x hi')).toEqual([]);
    expect(html('7:x hi')).not.toContain('');
  });
});

describe('checkboxes', () => {
  it('numbers them in order and reads the marks', () => {
    expect(boxes('- [ ] one\n- [x] two\n- [X] three')).toEqual([
      { n: 1, checked: false, label: 'one' },
      { n: 2, checked: true, label: 'two' },
      { n: 3, checked: true, label: 'three' },
    ]);
  });

  it('counts the star form and nested items', () => {
    const body = '* [ ] a\n  - [x] nested\n    * [ ] deeper\n- [ ] b';
    expect(boxes(body).map((b) => b.n)).toEqual([1, 2, 3, 4]);
    expect(boxes(body).map((b) => b.checked)).toEqual([false, true, false, false]);
  });

  it('does not count items in fenced code, so later ones keep the server numbers', () => {
    const body = '- [ ] a\n\n```\n- [ ] not an item\n```\n\n~~~\n- [x] nor this\n~~~\n\n- [x] b';
    expect(boxes(body).map((b) => [b.n, b.label])).toEqual([[1, 'a'], [2, 'b']]);
    expect(html(body)).toContain('- [ ] not an item');
  });

  it('keeps the server numbering when a list sits in a blockquote', () => {
    // Go does not count a quoted line, and neither does the page.
    const body = '> - [ ] quoted\n\n- [ ] first\n- [ ] second';
    expect(boxes(body).map((b) => [b.n, b.label])).toEqual([[1, 'first'], [2, 'second']]);
  });

  it('keeps the server numbering where marked would see a task but Go does not', () => {
    const body = '1. [ ] ordered\n+ [ ] plus\n- [ ] real one\n- [ ] real two';
    const out = boxes(body);
    expect(out.map((b) => [b.n, b.label])).toEqual([[1, 'real one'], [2, 'real two']]);
  });

  it('numbers an item that marked shows as code, by the server, and leaves its text alone', () => {
    // Four spaces make code for marked, yet Go counts the line: n stays 1 for it, the next is 2.
    const body = 'text\n\n    - [ ] indented code\n\n- [ ] after';
    expect(boxes(body).map((b) => [b.n, b.label])).toEqual([[2, 'after']]);
    expect(html(body)).toContain('- [ ] indented code');
  });

  it('numbers items in a loose list', () => {
    expect(boxes('- [ ] a\n\n- [x] b').map((b) => b.n)).toEqual([1, 2]);
  });

  it('matches the server rules: bare brackets with no text still count, and no space after the bracket does not', () => {
    const body = '- [ ]\n- [x]text\n- [ ] ok';
    expect(checklistLines(body).map((l) => l.line)).toEqual([0, 2]);
    expect(boxes(body).map((b) => b.n)).toEqual([1, 2]);
  });

  it('renders them enabled, or disabled when asked', () => {
    const div = document.createElement('div');
    div.append(renderBody('- [ ] a', { disabled: true }));
    expect(div.querySelector<HTMLInputElement>('input')!.disabled).toBe(true);
    const on = document.createElement('div');
    on.append(renderBody('- [ ] a'));
    expect(on.querySelector<HTMLInputElement>('input')!.disabled).toBe(false);
  });

  it('treats CRLF like LF', () => {
    expect(boxes('- [ ] a\r\n- [x] b\r\n').map((b) => [b.n, b.checked])).toEqual([[1, false], [2, true]]);
  });
});
