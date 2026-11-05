// The board's keyboard: what a key means, and where it leads. Pure, so the
// rules can be tested without a page. Board.svelte does the focusing and moving.

export type Dir = 'up' | 'down' | 'left' | 'right';

export type KeyAction =
  | { kind: 'focus'; dir: Dir }
  | { kind: 'move'; dir: Dir }
  | { kind: 'menu' }
  | { kind: 'open' }
  | { kind: 'escape' };

type KeyLike = Pick<KeyboardEvent, 'key' | 'shiftKey' | 'ctrlKey' | 'metaKey' | 'altKey'>;

const ARROWS: Record<string, Dir> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' };

/**
 * What a key does on a focused card. Arrows move focus, Shift+arrow moves the
 * card, m opens the move menu, Enter or Space opens the card, Esc closes
 * things. `n` and `/` belong to the top bar, which owns quick add and the filter.
 */
export function readKey(e: KeyLike): KeyAction | null {
  if (e.ctrlKey || e.metaKey || e.altKey) return null;
  const dir = ARROWS[e.key];
  if (dir) return e.shiftKey ? { kind: 'move', dir } : { kind: 'focus', dir };
  if (e.shiftKey) return null;
  if (e.key === 'm' || e.key === 'M') return { kind: 'menu' };
  if (e.key === 'Enter' || e.key === ' ') return { kind: 'open' };
  if (e.key === 'Escape') return { kind: 'escape' };
  return null;
}

/** A card as the keyboard sees the board: its id, and the swimlane it is in ('' outside swimlanes). */
export interface NavItem {
  id: string;
  lane: string;
}

/** The visible cards of each column, in the order they appear from top to bottom. */
export type NavGrid = readonly (readonly NavItem[])[];

function find(grid: NavGrid, id: string): { col: number; row: number } | null {
  for (let col = 0; col < grid.length; col++) {
    const row = grid[col]!.findIndex((i) => i.id === id);
    if (row >= 0) return { col, row };
  }
  return null;
}

/**
 * The card an arrow key moves focus to, or null when there is none that way.
 * Up and down stay in the column. Left and right go to the nearest column that
 * has cards, to the card at the same height in the same lane when there is
 * one, else the nearest in that column.
 */
export function focusTarget(grid: NavGrid, id: string, dir: Dir): string | null {
  const at = find(grid, id);
  if (!at) return null;
  const column = grid[at.col]!;
  if (dir === 'up') return column[at.row - 1]?.id ?? null;
  if (dir === 'down') return column[at.row + 1]?.id ?? null;

  const step = dir === 'left' ? -1 : 1;
  const me = column[at.row]!;
  const rowInLane = column.slice(0, at.row).filter((i) => i.lane === me.lane).length;
  for (let c = at.col + step; c >= 0 && c < grid.length; c += step) {
    const there = grid[c]!;
    if (there.length === 0) continue;
    const sameLane = there.filter((i) => i.lane === me.lane);
    if (sameLane.length > 0) return sameLane[Math.min(rowInLane, sameLane.length - 1)]!.id;
    return there[Math.min(at.row, there.length - 1)]!.id;
  }
  return null;
}

/**
 * Where Shift+Up or Shift+Down puts a card: between the visible cards of its
 * own lane, as the neighbours it will have after the move. Null at the edge.
 */
export function stepTarget(grid: NavGrid, id: string, dir: 'up' | 'down'): { after: string | null; before: string | null } | null {
  const at = find(grid, id);
  if (!at) return null;
  const me = grid[at.col]![at.row]!;
  const cell = grid[at.col]!.filter((i) => i.lane === me.lane);
  const i = cell.findIndex((c) => c.id === id);
  if (dir === 'up') {
    if (i <= 0) return null;
    return { after: cell[i - 2]?.id ?? null, before: cell[i - 1]!.id };
  }
  if (i < 0 || i >= cell.length - 1) return null;
  return { after: cell[i + 1]!.id, before: cell[i + 2]?.id ?? null };
}

/**
 * The column Shift+Left or Shift+Right sends a card to: the next one in that
 * direction that takes it, or null. `refusal` says why a column does not.
 */
export function adjacentColumn(columns: readonly string[], from: string, dir: 'left' | 'right', refusal: (column: string) => string | null): string | null {
  const at = columns.indexOf(from);
  if (at < 0) return null;
  const step = dir === 'left' ? -1 : 1;
  for (let c = at + step; c >= 0 && c < columns.length; c += step) {
    if (refusal(columns[c]!) === null) return columns[c]!;
  }
  return null;
}
