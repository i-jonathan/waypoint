import type { Blocker, BoardView, CardView } from './types';

// What the Next up, Blocked and Archive views show, worked out from a board. Pure.

/** How many cards follow the next-up card on the Next up view. */
export const QUEUE_LENGTH = 9;

export interface NextUp {
  /** The card the server marks as next up, if any. */
  next: CardView | null;
  /** The cards after it from the first open column, in rank order. */
  queue: CardView[];
  /** The first column that is not a done column. */
  column: string | null;
  /** Cards of that column that the queue leaves out. */
  more: number;
  /** Live cards on the board. */
  total: number;
}

export function nextUp(view: BoardView): NextUp {
  const live = view.cards.filter((c) => !c.archived);
  const next = live.find((c) => c.next) ?? null;
  const column = view.settings.columns.find((c) => !c.done)?.name ?? null;
  const rest = column === null ? [] : live.filter((c) => c.column === column && c !== next);
  return { next, queue: rest.slice(0, QUEUE_LENGTH), column, more: Math.max(0, rest.length - QUEUE_LENGTH), total: live.length };
}

export interface BlockedGroup {
  /** The first blocker of every card in the group. */
  blocker: Blocker;
  /** The blocker's title and column, when it is a card on the board. */
  title: string | undefined;
  column: string | undefined;
  cards: CardView[];
}

/** Blocked cards grouped by their first blocker, in the order the groups first appear on the board. */
export function blockedGroups(view: BoardView): BlockedGroup[] {
  const byId = new Map(view.cards.map((c) => [c.id, c]));
  const groups = new Map<string, BlockedGroup>();
  for (const c of view.cards) {
    const first = c.blockers[0];
    if (c.archived || first === undefined) continue;
    let g = groups.get(first.id);
    if (!g) {
      const found = first.missing ? undefined : byId.get(first.id);
      g = { blocker: first, title: found?.title, column: found?.column, cards: [] };
      groups.set(first.id, g);
    }
    g.cards.push(c);
  }
  return [...groups.values()];
}

/** Live cards that stand in a done column, which is what Archive done cards would take. */
export function doneCount(view: BoardView): number {
  const done = new Set(view.settings.columns.filter((c) => c.done).map((c) => c.name));
  return view.cards.filter((c) => !c.archived && done.has(c.column)).length;
}

export interface Month {
  /** YYYY-MM, or '' for cards with no readable date. */
  key: string;
  /** The heading: OCTOBER 2026. */
  label: string;
  cards: CardView[];
}

const MONTHS = ['JANUARY', 'FEBRUARY', 'MARCH', 'APRIL', 'MAY', 'JUNE', 'JULY', 'AUGUST', 'SEPTEMBER', 'OCTOBER', 'NOVEMBER', 'DECEMBER'];
const DATE_ONLY = /^(\d{4})-(\d{2})-\d{2}$/;

/** The UTC year and month (1 to 12) of a stored `done` value, or null. A date-only value is read as written. */
function monthOf(value: string | null): { y: number; m: number } | null {
  if (value === null) return null;
  const plain = DATE_ONLY.exec(value);
  if (plain) {
    const m = Number(plain[2]);
    return m >= 1 && m <= 12 ? { y: Number(plain[1]), m } : null;
  }
  const t = new Date(value);
  if (Number.isNaN(t.getTime())) return null;
  return { y: t.getUTCFullYear(), m: t.getUTCMonth() + 1 };
}

/**
 * The archive grouped by the UTC month of `done`, as the archive file does:
 * newest month first, the file's order kept inside a month, and cards with no
 * readable date last.
 */
export function archiveMonths(cards: readonly CardView[]): Month[] {
  const byKey = new Map<string, Month>();
  for (const c of cards) {
    const mo = monthOf(c.done);
    const key = mo ? `${String(mo.y).padStart(4, '0')}-${String(mo.m).padStart(2, '0')}` : '';
    let month = byKey.get(key);
    if (!month) {
      month = { key, label: mo ? `${MONTHS[mo.m - 1]} ${mo.y}` : 'NO DATE', cards: [] };
      byKey.set(key, month);
    }
    month.cards.push(c);
  }
  return [...byKey.values()].sort((a, b) => (a.key === '' ? 1 : b.key === '' ? -1 : a.key < b.key ? 1 : a.key > b.key ? -1 : 0));
}
