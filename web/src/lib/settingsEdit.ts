import { PRESETS } from './accent';
import type { Appearance, ArchiveMode, CardView, Settings } from './types';

// The project settings form, as data. The form edits rows that remember the name
// they started with (`origin`); the renames the server needs fall out of that,
// so a swap of two names is two renames and a reorder is none.

export interface ColumnRow {
  /** Stable while editing, whatever the row is named. Not sent anywhere. */
  key: string;
  /** The name it had when loaded, or null for a column added in this edit. */
  origin: string | null;
  name: string;
  /** What the WIP field holds: empty, or digits. */
  wip: string;
  /** The keys of the columns a card may move to from here. */
  moves: string[];
  done: boolean;
}

export interface GroupRow {
  key: string;
  origin: string | null;
  name: string;
  color: string;
}

export interface PriorityRow {
  key: string;
  origin: string | null;
  name: string;
}

export interface Draft {
  columns: ColumnRow[];
  groups: GroupRow[];
  priorities: PriorityRow[];
  archive: { mode: ArchiveMode; days: string };
  appearance: Appearance;
}

/** What goes into the PUT, less the hash the store adds. */
export interface SettingsBody {
  settings: Settings;
  column_renames: Record<string, string>;
  group_renames: Record<string, string>;
  priority_renames: Record<string, string>;
}

const HEX = /^#[0-9a-f]{6}$/i;
const DIGITS = /^\d+$/;

let seq = 0;
export const newKey = (): string => `r${++seq}`;

export function toDraft(s: Settings): Draft {
  const columns = s.columns.map<ColumnRow>((c) => ({
    key: newKey(),
    origin: c.name,
    name: c.name,
    wip: c.wip > 0 ? String(c.wip) : '',
    moves: [],
    done: c.done,
  }));
  const keyOf = new Map(columns.map((r) => [r.origin, r.key]));
  s.columns.forEach((c, i) => {
    columns[i]!.moves = c.moves_to.map((n) => keyOf.get(n)).filter((k): k is string => k !== undefined);
  });
  return {
    columns,
    groups: s.groups.map((g) => ({ key: newKey(), origin: g.name, name: g.name, color: g.color })),
    priorities: s.priorities.map((p) => ({ key: newKey(), origin: p, name: p })),
    archive: { mode: s.archive.mode, days: String(s.archive.after_days) },
    appearance: { ...s.appearance },
  };
}

function renamesOf(rows: Array<{ origin: string | null; name: string }>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) {
    const name = r.name.trim();
    if (r.origin !== null && r.origin !== name) out[r.origin] = name;
  }
  return out;
}

export function buildChange(base: Settings, d: Draft): SettingsBody {
  const names = new Map(d.columns.map((r) => [r.key, r.name.trim()]));
  const archive =
    d.archive.mode === 'auto' && DIGITS.test(d.archive.days) ? Number(d.archive.days) : base.archive.after_days;
  return {
    settings: {
      ...base,
      columns: d.columns.map((r) => ({
        name: r.name.trim(),
        wip: DIGITS.test(r.wip) ? Number(r.wip) : 0,
        // In the new column order, whatever order the boxes were ticked in.
        moves_to: d.columns.filter((o) => o.key !== r.key && r.moves.includes(o.key)).map((o) => names.get(o.key)!),
        done: r.done,
      })),
      groups: d.groups.map((g) => ({ name: g.name.trim(), color: g.color.trim() })),
      priorities: d.priorities.map((p) => p.name.trim()),
      archive: { mode: d.archive.mode, after_days: archive },
      appearance: { ...d.appearance },
    },
    column_renames: renamesOf(d.columns),
    group_renames: renamesOf(d.groups),
    priority_renames: renamesOf(d.priorities),
  };
}

/** True when saving would change anything. */
export function isDirty(base: Settings, d: Draft): boolean {
  // What was typed counts too, even where it would not be sent: a WIP of "1.5" is sent as none.
  const typed = (x: Draft) => [x.columns.map((r) => r.wip), x.archive.days];
  const was = toDraft(base);
  return (
    JSON.stringify(buildChange(base, d)) !== JSON.stringify(buildChange(base, was)) ||
    JSON.stringify(typed(d)) !== JSON.stringify(typed(was))
  );
}

/** A copy of the list with the item at `from` moved to `to`; out of range changes nothing. */
export function moveRow<T>(rows: readonly T[], from: number, to: number): T[] {
  const out = [...rows];
  if (from < 0 || from >= out.length || to < 0 || to >= out.length || from === to) return out;
  const [item] = out.splice(from, 1);
  out.splice(to, 0, item!);
  return out;
}

/** The rows with a new column at the end. It has no moves_to: nothing can leave it until the owner says so. */
export function addColumn(rows: readonly ColumnRow[]): ColumnRow[] {
  const taken = new Set(rows.map((r) => r.name.trim()));
  let name = 'New column';
  for (let n = 2; taken.has(name); n++) name = `New column ${n}`;
  return [...rows, { key: newKey(), origin: null, name, wip: '', moves: [], done: false }];
}

const PRESET_NAMES = Object.keys(PRESETS);

/** The next preset colour not used by a group yet, so a new group is not the same colour as the last. */
export function nextGroupColor(rows: readonly GroupRow[]): string {
  const used = new Set(rows.map((g) => g.color));
  return PRESET_NAMES.find((n) => !used.has(n)) ?? PRESET_NAMES[rows.length % PRESET_NAMES.length]!;
}

export function addGroup(rows: readonly GroupRow[]): GroupRow[] {
  const taken = new Set(rows.map((r) => r.name.trim().toLowerCase()));
  let name = 'New group';
  for (let n = 2; taken.has(name.toLowerCase()); n++) name = `New group ${n}`;
  return [...rows, { key: newKey(), origin: null, name, color: nextGroupColor(rows) }];
}

export function addPriority(rows: readonly PriorityRow[]): PriorityRow[] {
  const taken = new Set(rows.map((r) => r.name.trim().toLowerCase()));
  let name = 'New priority';
  for (let n = 2; taken.has(name.toLowerCase()); n++) name = `New priority ${n}`;
  return [...rows, { key: newKey(), origin: null, name }];
}

// ----- Errors: keyed by the id of the field, so the form can focus the first one. -----

export type Errors = Record<string, string>;

export function validateDraft(d: Draft): Errors {
  const errs: Errors = {};

  const seen = new Set<string>();
  for (const r of d.columns) {
    const name = r.name.trim();
    if (name === '') errs[`col-${r.key}-name`] = 'A column needs a name';
    else if (seen.has(name)) errs[`col-${r.key}-name`] = `A column called ${name} already exists`;
    seen.add(name);
    if (r.wip !== '' && (!DIGITS.test(r.wip) || Number(r.wip) < 1)) {
      errs[`col-${r.key}-wip`] = 'Use a whole number above 0, or leave it empty';
    }
  }

  const groups = new Set<string>();
  for (const g of d.groups) {
    const name = g.name.trim();
    if (name === '') errs[`grp-${g.key}-name`] = 'A group needs a name';
    else if (groups.has(name.toLowerCase())) errs[`grp-${g.key}-name`] = `A group called ${name} already exists`;
    groups.add(name.toLowerCase());
    if (g.color.trim().startsWith('#') && !HEX.test(g.color.trim())) {
      errs[`grp-${g.key}-color`] = 'Use #rrggbb, for example #2f5bff';
    }
  }

  const priorities = new Set<string>();
  for (const p of d.priorities) {
    const name = p.name.trim();
    if (name === '') errs[`pri-${p.key}-name`] = 'A priority needs a name';
    else if (priorities.has(name.toLowerCase())) errs[`pri-${p.key}-name`] = `A priority called ${name} already exists`;
    priorities.add(name.toLowerCase());
  }

  if (d.archive.mode === 'auto' && !DIGITS.test(d.archive.days)) {
    errs['archive-days'] = 'Use a whole number of days';
  }

  const accent = d.appearance.accent;
  if (accent.startsWith('#') && !HEX.test(accent)) errs['accent'] = 'Use #rrggbb, for example #2f5bff';

  return errs;
}

// ----- Counts, from the whole board -----

/** How many live cards each column holds. */
export function columnCounts(cards: readonly CardView[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const c of cards) if (!c.archived) out[c.column] = (out[c.column] ?? 0) + 1;
  return out;
}

const cardsWord = (n: number) => (n === 1 ? '1 card' : `${n} cards`);

function removedNames(had: readonly string[], rows: ReadonlyArray<{ origin: string | null; name: string }>): string[] {
  return had.filter(
    (h) =>
      !rows.some((r) => r.origin === h) && // not renamed to something else
      !rows.some((r) => r.name.trim().toLowerCase() === h.toLowerCase()), // nor added back under the same name
  );
}

/** Whether this edit removes a group or priority, so the archive has to be counted as well. */
export function removesNames(base: Settings, d: Draft): boolean {
  return removedNames(base.groups.map((g) => g.name), d.groups).length > 0 || removedNames(base.priorities, d.priorities).length > 0;
}

/**
 * One line for each group or priority this edit removes while cards still have
 * it: the server clears it from them. `cards` is the whole board, not a filtered view, and
 * `archived` is the archive, which the server changes too.
 */
export function clearWarnings(base: Settings, d: Draft, cards: readonly CardView[], archived: readonly CardView[] = []): string[] {
  const out: string[] = [];
  const add = (names: string[], of: (c: CardView) => string) => {
    for (const name of names) {
      const has = (c: CardView) => of(c).toLowerCase() === name.toLowerCase();
      const old = archived.filter(has).length;
      const n = cards.filter(has).length + old;
      if (n > 0) out.push(`Removing ${name} clears it from ${cardsWord(n)}${old > 0 ? `, including ${old} archived` : ''}.`);
    }
  };
  add(
    removedNames(base.groups.map((g) => g.name), d.groups),
    (c) => c.group,
  );
  add(removedNames(base.priorities, d.priorities), (c) => c.priority);
  return out;
}
