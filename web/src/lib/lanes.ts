import { groupColor } from './colors';
import type { BoardView, CardView, Swimlane } from './types';

export type LaneMode = Exclude<Swimlane, 'none'>;

/** One swimlane row. `key` is the value a card in the lane carries; '' is the lane for cards without one. */
export interface Lane {
  key: string;
  name: string;
  /** A CSS colour for the lane's route line, or null. */
  color: string | null;
  none: boolean;
}

/** The cards of every cell of the board, by zone id, in rank order. */
export interface Layout {
  lanes: Lane[];
  cells: Map<string, readonly CardView[]>;
}

const EMPTY: readonly CardView[] = Object.freeze([]);

/** An id for a drop zone: a column, or a column within a lane. Unique whatever the names contain. */
export function zoneId(column: string, lane?: string): string {
  return lane === undefined ? JSON.stringify([column]) : JSON.stringify([column, lane]);
}

export function laneKeyOf(card: CardView, mode: LaneMode): string {
  return mode === 'group' ? card.group : card.priority;
}

/**
 * The lanes of a board: the groups (or priorities) in settings order, then any
 * name only cards carry, then the lane for cards with none. Every lane shows,
 * empty or not, so a card can always be dropped into it.
 */
export function lanesOf(view: BoardView, mode: LaneMode): Lane[] {
  const lanes: Lane[] = [];
  const seen = new Set<string>();
  if (mode === 'group') {
    for (const g of view.settings.groups) {
      seen.add(g.name);
      lanes.push({ key: g.name, name: g.name, color: groupColor(g.color), none: false });
    }
  } else {
    for (const p of view.settings.priorities) {
      seen.add(p);
      lanes.push({ key: p, name: p, color: null, none: false });
    }
  }
  for (const card of view.cards) {
    const key = laneKeyOf(card, mode);
    if (key === '' || seen.has(key)) continue;
    seen.add(key);
    lanes.push({ key, name: key, color: mode === 'group' ? 'var(--muted)' : null, none: false });
  }
  lanes.push({ key: '', name: mode === 'group' ? 'No group' : 'No priority', color: null, none: true });
  return lanes;
}

function sameCards(a: readonly CardView[], b: readonly CardView[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

/**
 * Makes the function that sorts a board's live cards into cells. It keeps the
 * last answer, and hands back the very same array for a cell whose cards have
 * not changed, so a move touches only the two cells it changed.
 */
export function createLayout() {
  let prev = new Map<string, readonly CardView[]>();
  return function layout(view: BoardView, mode: Swimlane): Layout {
    const lanes = mode === 'none' ? [] : lanesOf(view, mode);
    const buckets = new Map<string, CardView[]>();
    const columns = new Set(view.settings.columns.map((c) => c.name));
    for (const card of view.cards) {
      if (card.archived || !columns.has(card.column)) continue;
      const id = zoneId(card.column, mode === 'none' ? undefined : laneKeyOf(card, mode));
      const list = buckets.get(id);
      if (list) list.push(card);
      else buckets.set(id, [card]);
    }
    const cells = new Map<string, readonly CardView[]>();
    const ids: string[] = [];
    for (const col of view.settings.columns) {
      if (mode === 'none') ids.push(zoneId(col.name));
      else for (const lane of lanes) ids.push(zoneId(col.name, lane.key));
    }
    for (const id of ids) {
      const fresh = buckets.get(id);
      if (!fresh) {
        cells.set(id, EMPTY);
        continue;
      }
      const old = prev.get(id);
      cells.set(id, old && sameCards(old, fresh) ? old : fresh);
    }
    prev = cells;
    return { lanes, cells };
  };
}

// ----- Collapsed lanes, remembered per project -----

const storageKey = (project: string) => `waypoint.lanes.${project}`;

/** The lanes the person collapsed, as keys. Storage that is missing, blocked or damaged reads as none collapsed. */
export function loadCollapsed(project: string, mode: LaneMode): Set<string> {
  try {
    const raw = localStorage.getItem(storageKey(project));
    if (raw === null) return new Set();
    const parsed: unknown = JSON.parse(raw);
    const list = typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, unknown>)[mode] : undefined;
    return new Set(Array.isArray(list) ? list.filter((k): k is string => typeof k === 'string') : []);
  } catch {
    return new Set();
  }
}

export function saveCollapsed(project: string, mode: LaneMode, collapsed: ReadonlySet<string>): void {
  try {
    let all: Record<string, unknown> = {};
    try {
      const parsed: unknown = JSON.parse(localStorage.getItem(storageKey(project)) ?? '{}');
      if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) all = parsed as Record<string, unknown>;
    } catch {
      // damaged: start again
    }
    all[mode] = [...collapsed];
    localStorage.setItem(storageKey(project), JSON.stringify(all));
  } catch {
    // storage is a convenience
  }
}
