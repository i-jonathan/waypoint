import type { BoardView } from './types';

export interface GroupCount {
  done: number;
  total: number;
}

export interface BoardStats {
  /** Cards on the board. */
  total: number;
  /** Cards with an unfinished dependency. */
  blocked: number;
  groups: Record<string, GroupCount>;
}

/** The counts the sidebar shows. A card is done when it stands in a done column. */
export function summarize(view: BoardView): BoardStats {
  const doneColumns = new Set(view.settings.columns.filter((c) => c.done).map((c) => c.name));
  const stats: BoardStats = { total: 0, blocked: 0, groups: {} };
  for (const card of view.cards) {
    if (card.archived) continue;
    stats.total++;
    if (card.blockers.length > 0) stats.blocked++;
    if (card.group === '') continue;
    const g = (stats.groups[card.group] ??= { done: 0, total: 0 });
    g.total++;
    if (doneColumns.has(card.column)) g.done++;
  }
  return stats;
}
