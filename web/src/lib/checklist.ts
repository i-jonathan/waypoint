// A client-side copy of the checklist rules of internal/domain/checklist.go.
// It lets a tick show at once and numbers the checkboxes of a rendered body the
// way the server numbers them. The server stays the authority: the store
// refreshes after a failure, and replaces the card with the server's answer.

// Go's `.` matches everything but "\n", where JavaScript's also leaves out "\r", U+2028 and U+2029,
// so these spell it `[^\n]`. A line has every trailing "\r" taken off, as strings.TrimRight does.
const ITEM = /^([ \t]*[-*][ \t]+\[)([ xX])(\](?:[ \t]+[^\n]*)?)$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})([^\n]*)$/;
const TRAILING_CR = /\r+$/;
// strings.TrimSpace: Unicode White_Space. JavaScript's trim() also takes U+FEFF, which Go does not.
const BLANK = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$/;

/** One checklist item of a card body. */
export interface ChecklistLine {
  /** The 0-based index of its line. */
  line: number;
  /** Where the mark (space, x or X) is inside that line. */
  markAt: number;
  checked: boolean;
}

/**
 * The checklist items of a card body in order, ignoring fenced code: item n
 * (1-based) is the n-th entry. Same rules as domain.Checklist.
 */
export function checklistLines(body: string): ChecklistLine[] {
  const lines = body.split('\n');
  const out: ChecklistLine[] = [];
  let fence = ''; // the opening run, or '' outside a fence
  for (let i = 0; i < lines.length; i++) {
    const text = lines[i]!.replace(TRAILING_CR, '');
    const f = FENCE.exec(text);
    if (fence === '') {
      if (f && !(f[1]![0] === '`' && f[2]!.includes('`'))) {
        fence = f[1]!;
        continue;
      }
    } else {
      if (f && f[1]![0] === fence[0] && f[1]!.length >= fence.length && BLANK.test(f[2]!)) fence = '';
      continue;
    }
    const m = ITEM.exec(text);
    if (m) out.push({ line: i, markAt: m[1]!.length, checked: m[2] !== ' ' });
  }
  return out;
}

/**
 * Flips the nth (1-based) checklist item of a card body, ignoring fenced code
 * as the server does. Returns null when there is no such item.
 */
export function toggleItem(body: string, n: number): { body: string; wasChecked: boolean } | null {
  const item = checklistLines(body)[n - 1];
  if (!item) return null;
  const lines = body.split('\n');
  const line = lines[item.line]!;
  lines[item.line] = line.slice(0, item.markAt) + (item.checked ? ' ' : 'x') + line.slice(item.markAt + 1);
  return { body: lines.join('\n'), wasChecked: item.checked };
}
