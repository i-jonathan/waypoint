// A client-side copy of Board.ToggleCheck (internal/domain/checklist.go), only
// so a tick can show at once. The server stays the authority: the store
// refreshes after a failure, and replaces the card with the server's answer.

const ITEM = /^([ \t]*[-*][ \t]+\[)([ xX])(\](?:[ \t]+.*)?)$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/;

/**
 * Flips the nth (1-based) checklist item of a card body, ignoring fenced code
 * as the server does. Returns null when there is no such item.
 */
export function toggleItem(body: string, n: number): { body: string; wasChecked: boolean } | null {
  const lines = body.split('\n');
  let fence = ''; // the opening run, or '' outside a fence
  let seen = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    const f = FENCE.exec(line.replace(/\r$/, ''));
    if (fence === '') {
      if (f && !(f[1]![0] === '`' && f[2]!.includes('`'))) {
        fence = f[1]!;
        continue;
      }
    } else {
      if (f && f[1]![0] === fence[0] && f[1]!.length >= fence.length && f[2]!.trim() === '') fence = '';
      continue;
    }
    const m = ITEM.exec(line.replace(/\r$/, ''));
    if (!m) continue;
    seen++;
    if (seen !== n) continue;
    const wasChecked = m[2] !== ' ';
    lines[i] = line.slice(0, m[1]!.length) + (wasChecked ? ' ' : 'x') + line.slice(m[1]!.length + 1);
    return { body: lines.join('\n'), wasChecked };
  }
  return null;
}
