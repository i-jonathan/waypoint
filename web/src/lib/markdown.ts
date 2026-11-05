import DOMPurify from 'dompurify';
import { Marked } from 'marked';
import { checklistLines } from './checklist';

// Card bodies are written by people and by agents, and the API can create
// files, so this is the one place body text becomes page content. The path is
// fixed: marked, then DOMPurify with an allow list, then a pass that only adds
// elements this file creates. The result is a DocumentFragment that the caller
// appends; it is never turned back into a string and parsed again.

// Private-use marks that stand in for a checklist item's `[ ]` while marked runs.
const OPEN = '';
const CLOSE = '';
const MARK = /(\d+):([ xX])/g;

const TAGS = [
  'a', 'blockquote', 'br', 'code', 'del', 'details', 'div', 'em', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'kbd', 'li', 'mark', 'ol', 'p', 'pre', 's',
  'span', 'strong', 'sub', 'summary', 'sup', 'table', 'tbody', 'td', 'tfoot', 'th', 'thead', 'tr', 'ul',
];
const ATTRS = ['href', 'title', 'align', 'start', 'colspan', 'rowspan', 'open'];
const FORBIDDEN_TAGS = ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select', 'img', 'svg', 'math', 'link', 'meta', 'base'];
// Only these can be followed. Anything else (javascript:, data:, relative paths) has no destination.
const SAFE_URL = /^(?:https?:|mailto:)/i;

const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');

const marked = new Marked({
  gfm: true,
  breaks: false,
  renderer: {
    // Task items are numbered by the server's rules (see prepare), not marked's. One that marked
    // sees anyway (`1. [ ] x`) is not on the server's list, so it stays text.
    checkbox: ({ checked }) => (checked ? '[x] ' : '[ ] '),
    // Nothing is ever loaded from a body: a remote image is a link to it, any other is its alt text.
    image: ({ href, text }) => {
      const alt = escapeHtml(text.trim());
      let host = '';
      if (/^(?:https?:)?\/\//i.test(href)) {
        try {
          host = new URL(href.startsWith('//') ? `https:${href}` : href).host;
        } catch {
          host = '';
        }
      }
      if (host === '') return `<span>${alt === '' ? 'image' : alt}</span>`;
      const to = escapeHtml(href.startsWith('//') ? `https:${href}` : href);
      return `<a href="${to}">${alt === '' ? 'image' : alt} (${escapeHtml(host)})</a>`;
    },
  },
});

let purifier: ReturnType<typeof DOMPurify> | undefined;

function purify() {
  if (purifier) return purifier;
  const p = DOMPurify(window);
  p.addHook('afterSanitizeAttributes', (node) => {
    if (node.nodeName !== 'A') return;
    const href = node.getAttribute('href');
    if (href !== null && !SAFE_URL.test(href.replace(/[\u0000- ]/g, ''))) node.removeAttribute('href');
    if (node.hasAttribute('href')) {
      node.setAttribute('target', '_blank');
      node.setAttribute('rel', 'noopener noreferrer');
    }
  });
  purifier = p;
  return p;
}

/** Marks each checklist line of the server's list, so marked cannot disagree with it about which items exist. */
function prepare(body: string): string {
  const clean = body.replaceAll(OPEN, '�').replaceAll(CLOSE, '�');
  const lines = clean.split('\n');
  checklistLines(clean).forEach((item, i) => {
    const line = lines[item.line]!;
    lines[item.line] = line.slice(0, item.markAt - 1) + OPEN + String(i + 1) + ':' + line[item.markAt] + CLOSE + line.slice(item.markAt + 2);
  });
  return lines.join('\n');
}

/** Whether a mark at the very start of this text node begins a list item. */
function startsItem(text: Text): boolean {
  const parent = text.parentElement;
  if (!parent || text !== parent.firstChild) return false;
  if (parent.tagName === 'LI') return true;
  if (parent.tagName !== 'P') return false;
  const li = parent.parentElement;
  if (!li || li.tagName !== 'LI') return false;
  return [...li.childNodes].find((n) => !(n.nodeType === 3 && n.textContent!.trim() === '')) === parent;
}

/**
 * Turns marks into checkboxes where they begin a list item, and back into the
 * text they replaced everywhere else (code, quotes, HTML blocks).
 */
function placeCheckboxes(root: DocumentFragment, disabled: boolean) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const texts: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) if (n.textContent!.includes(OPEN)) texts.push(n as Text);

  for (const text of texts) {
    const value = text.textContent!;
    const parts: Array<string | HTMLInputElement> = [];
    let last = 0;
    for (const m of value.matchAll(MARK)) {
      const at = m.index!;
      parts.push(value.slice(last, at));
      last = at + m[0].length;
      if (at === 0 && startsItem(text)) {
        const box = document.createElement('input');
        box.type = 'checkbox';
        box.checked = m[2] !== ' ';
        box.disabled = disabled;
        box.dataset.n = m[1]!;
        parts.push(box);
      } else {
        parts.push(`[${m[2]}]`);
      }
    }
    parts.push(value.slice(last));
    text.replaceWith(...parts.filter((p) => p !== ''));
  }

  for (const box of root.querySelectorAll<HTMLInputElement>('input[data-n]')) {
    const label = (box.closest('li')?.textContent ?? '').replace(/\s+/g, ' ').trim();
    box.setAttribute('aria-label', label === '' ? `Item ${box.dataset.n}` : label.slice(0, 120));
  }
}

/**
 * Renders a card body. The nth checkbox in the result is the nth item of the
 * server's checklist (`data-n`), whatever marked made of the lines. Append the
 * fragment; do not serialise it.
 */
export function renderBody(body: string, opts: { disabled?: boolean } = {}): DocumentFragment {
  const html = marked.parse(prepare(body), { async: false });
  const clean = purify().sanitize(html, {
    ALLOWED_TAGS: TAGS,
    ALLOWED_ATTR: ATTRS,
    FORBID_TAGS: FORBIDDEN_TAGS,
    FORBID_ATTR: ['style', 'class', 'id', 'name', 'srcset', 'src'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    ALLOWED_URI_REGEXP: SAFE_URL,
    RETURN_DOM_FRAGMENT: true,
  });
  placeCheckboxes(clean, opts.disabled ?? false);
  return clean;
}
