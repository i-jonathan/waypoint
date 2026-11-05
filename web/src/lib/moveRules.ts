import type { CardView, Settings } from './types';

// The rules of a move: pure, and shared by the pointer, the keyboard and the menu.

/**
 * Why a card cannot go to a column, or null when it can. It checks what the
 * client knows (moves_to and the WIP limit, counted on the whole board);
 * the server stays the authority and refuses what this lets through.
 * Moving within a column only reorders, and is always allowed.
 */
export function canDrop(settings: Pick<Settings, 'columns'>, cards: readonly CardView[], cardId: string, to: string): string | null {
  const card = cards.find((c) => c.id === cardId);
  if (!card) return 'That card is no longer on the board';
  if (card.column === to) return null;
  const dest = settings.columns.find((c) => c.name === to);
  if (!dest) return `There is no column called ${to}`;
  const src = settings.columns.find((c) => c.name === card.column);
  if (!src || !src.moves_to.includes(to)) return `${card.column} cannot move to ${to}`;
  if (dest.wip > 0) {
    let n = 0;
    for (const c of cards) if (!c.archived && c.column === to) n++;
    if (n >= dest.wip) return `${to} is at its WIP limit (${n}/${dest.wip})`;
  }
  return null;
}

/** The ids of a column's live cards, in rank order. */
export function columnIds(cards: readonly CardView[], column: string): string[] {
  const ids: string[] = [];
  for (const c of cards) if (!c.archived && c.column === column) ids.push(c.id);
  return ids;
}

/**
 * The index to send for a drop, in the column's FULL rank order, whatever the
 * board is filtered to. `fullIds` is the whole column, `after` is the visible
 * card just above the drop and `before` the visible card just below.
 *
 * The card lands right after `after` in the full order; with nothing above it,
 * right before `before`; with neither, at the bottom. Like the server's, the
 * index counts the column without the moving card.
 */
export function dropIndex(fullIds: readonly string[], movingId: string, after: string | null, before: string | null): number {
  const rest = fullIds.filter((id) => id !== movingId);
  if (after !== null) {
    const at = rest.indexOf(after);
    if (at >= 0) return at + 1;
  }
  if (before !== null) {
    const at = rest.indexOf(before);
    if (at >= 0) return at;
  }
  return rest.length;
}
