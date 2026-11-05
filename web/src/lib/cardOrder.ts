import type { CardView } from './types';

/**
 * Where a card sits in its column: the column, the cards on either side, and
 * its index. Neighbours are remembered by id so the spot can be found again
 * after other cards have moved around it.
 */
export interface Place {
  column: string;
  /** The card above it in the column, or null when it was first. */
  after: string | null;
  /** The card below it in the column, or null when it was last. */
  before: string | null;
  index: number;
}

/** Where a card is now, or null when the board has no such card. */
export function placeOf(cards: readonly CardView[], id: string): Place | null {
  const card = cards.find((c) => c.id === id);
  if (!card) return null;
  const col = cards.filter((c) => c.column === card.column);
  const i = col.findIndex((c) => c.id === id);
  return { column: card.column, after: col[i - 1]?.id ?? null, before: col[i + 1]?.id ?? null, index: i };
}

/**
 * Puts the card at a place and returns the new list. The list is in file
 * order, so only the card's order among its column's cards matters.
 *
 * The neighbours are tried first (`after`, then `before`), because they stay
 * right when other cards have come and gone. `index` is the fallback, and like
 * the server it counts the column without the card itself; below zero or past
 * the end means the bottom. A board without the card is returned as it is.
 */
export function placeCard(cards: readonly CardView[], id: string, to: Place): CardView[] {
  const card = cards.find((c) => c.id === id);
  if (!card) return cards.slice();
  const rest = cards.filter((c) => c.id !== id);
  const inColumn: number[] = [];
  rest.forEach((c, i) => {
    if (c.column === to.column) inColumn.push(i);
  });

  const afterAt = to.after === null ? -1 : inColumn.find((i) => rest[i]!.id === to.after) ?? -1;
  const beforeAt = to.before === null ? -1 : inColumn.find((i) => rest[i]!.id === to.before) ?? -1;
  let at: number;
  if (afterAt >= 0) at = afterAt + 1;
  else if (beforeAt >= 0) at = beforeAt;
  else if (to.index >= 0 && to.index < inColumn.length) at = inColumn[to.index]!;
  else at = inColumn.length > 0 ? inColumn[inColumn.length - 1]! + 1 : rest.length;

  return [...rest.slice(0, at), { ...card, column: to.column }, ...rest.slice(at)];
}

/**
 * Where a card goes in `cards`, a list that may show only some of the board,
 * given an index in `whole`, the board with every card. The index counts the
 * column of `whole` without the card, as the server does. The place is
 * described by the nearest cards that `cards` shows, so a filtered view puts
 * the card where the whole board does, not at the same index among fewer cards.
 */
export function anchorsIn(cards: readonly CardView[], whole: readonly CardView[], id: string, column: string, index: number): Place {
  const shown = new Set<string>();
  for (const c of cards) if (c.column === column && c.id !== id) shown.add(c.id);
  const order = whole.filter((c) => !c.archived && c.column === column && c.id !== id).map((c) => c.id);
  const at = index < 0 ? order.length : Math.min(index, order.length);
  let after: string | null = null;
  let seen = 0;
  for (let i = 0; i < at; i++) {
    if (shown.has(order[i]!)) {
      after = order[i]!;
      seen++;
    }
  }
  let before: string | null = null;
  for (let i = at; i < order.length; i++) {
    if (shown.has(order[i]!)) {
      before = order[i]!;
      break;
    }
  }
  return { column, after, before, index: seen };
}
