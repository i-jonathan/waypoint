import type { Card, CardPatch, ExtraField } from './types';

// The card panel's draft: what a person has typed, set against the card it was
// based on. Pure functions over a plain state, so the rules that keep a draft
// from being lost or from overwriting someone's change can be tested alone.

/** The fields of a card that a person edits. Column is moved, not edited. */
export interface Fields {
  title: string;
  group: string;
  priority: string;
  depends: string[];
  body: string;
  extra: ExtraField[];
}

/** A card that another can depend on, for the autocomplete. */
export interface KnownCard {
  id: string;
  title: string;
  archived: boolean;
}

export type FieldName = keyof Fields;

export const FIELD_NAMES: readonly FieldName[] = ['title', 'group', 'priority', 'depends', 'body', 'extra'];

export interface DraftState {
  /** The hash of the card the draft started from: what a save sends as `if_match`. */
  baseHash: string;
  /** The server's value of each field, as far as this tab has seen it. */
  base: Fields;
  /** What is on screen. */
  draft: Fields;
}

export function fieldsOf(c: Pick<Card, FieldName>): Fields {
  return { title: c.title, group: c.group, priority: c.priority, depends: [...c.depends], body: c.body, extra: c.extra.map((f) => ({ ...f })) };
}

export function startDraft(c: Pick<Card, FieldName> & { hash: string }): DraftState {
  return { baseHash: c.hash, base: fieldsOf(c), draft: fieldsOf(c) };
}

/** The extra fields that count: a blank key is an unfinished row, and an empty value means the field is not there. */
export function extraMap(rows: readonly ExtraField[]): Map<string, string> {
  const m = new Map<string, string>();
  for (const r of rows) {
    const key = r.key.trim();
    if (key !== '' && r.value !== '') m.set(key, r.value);
  }
  return m;
}

export function sameField(name: FieldName, a: Fields, b: Fields): boolean {
  switch (name) {
    case 'depends':
      return a.depends.length === b.depends.length && a.depends.every((d, i) => d === b.depends[i]);
    case 'extra': {
      const x = extraMap(a.extra);
      const y = extraMap(b.extra);
      return x.size === y.size && [...x].every(([k, v]) => y.get(k) === v);
    }
    default:
      return a[name] === b[name];
  }
}

/** The fields whose draft differs from the server's value. */
export function dirtyOf(s: Pick<DraftState, 'base' | 'draft'>): FieldName[] {
  return FIELD_NAMES.filter((f) => !sameField(f, s.draft, s.base));
}

/** A patch for the named fields. Extra fields are sent as `set`, where an empty value deletes the key. */
export function toPatch(s: Pick<DraftState, 'base' | 'draft'>, names: readonly FieldName[]): CardPatch {
  const patch: CardPatch = {};
  for (const f of names) {
    if (f === 'extra') {
      const now = extraMap(s.draft.extra);
      const was = extraMap(s.base.extra);
      const set: Record<string, string> = {};
      for (const [k, v] of now) if (was.get(k) !== v) set[k] = v;
      for (const k of was.keys()) if (!now.has(k)) set[k] = '';
      patch.set = set;
    } else if (f === 'depends') {
      patch.depends = [...s.draft.depends];
    } else {
      patch[f] = s.draft[f];
    }
  }
  return patch;
}

/** What an own write is known to do to a card besides its hash. */
export interface Own {
  /** The body after the ticks this panel has made; undefined when it has made none. */
  body: string | undefined;
}

/** Whether `c` is the draft's base card with nothing changed but what this panel's own writes change. */
function onlyOwnChanges(base: Fields, c: Fields, own: Own): boolean {
  return FIELD_NAMES.every((f) => {
    if (f === 'body' && own.body !== undefined && c.body === own.body) return true;
    return sameField(f, base, c);
  });
}

/**
 * A card from the store arrived. Fields that are not dirty follow the server;
 * a dirty one is never touched. The hash the draft is based on moves only when
 * nothing is dirty, or when the change is this panel's own write (`own`): a
 * move or tick of its own gives the card a new hash, which is not a conflict.
 * An outside edit leaves the old hash in place, so that saving meets the 409.
 */
export function absorb(s: DraftState, c: Pick<Card, FieldName> & { hash: string }, own: Own | null): DraftState {
  const dirty = new Set(dirtyOf(s));
  const incoming = fieldsOf(c);
  const base = { ...s.base };
  const draft = { ...s.draft };
  for (const f of FIELD_NAMES) {
    if (dirty.has(f)) continue;
    (base as Record<FieldName, unknown>)[f] = incoming[f];
    (draft as Record<FieldName, unknown>)[f] = incoming[f];
  }
  let baseHash = s.baseHash;
  if (dirty.size === 0 || (c.hash !== s.baseHash && own !== null && onlyOwnChanges(s.base, incoming, own))) baseHash = c.hash;
  return { baseHash, base, draft };
}

/**
 * A save succeeded. The server normalises text (trailing space, CRLF, heading
 * levels), so every field that is still what was sent takes the response's
 * value; one typed since the save began stays, and is dirty against the new base.
 */
export function saved(s: DraftState, sent: Fields, card: Pick<Card, FieldName> & { hash: string }): DraftState {
  const fresh = fieldsOf(card);
  const draft = { ...s.draft };
  for (const f of FIELD_NAMES) if (sameField(f, s.draft, sent)) (draft as Record<FieldName, unknown>)[f] = fresh[f];
  return { baseHash: card.hash, base: fresh, draft };
}

const FIELD_KEY = /^[a-z][a-z0-9_-]*$/;
// Written by the format itself (group, priority, depends, created, done), or a card's own column, id and rank.
const BUILT_IN = ['group', 'priority', 'depends', 'created', 'done', 'column', 'id', 'rank'];

/** Why a key cannot be an extra field's, or null. The server's rule, so a refusal is not a surprise at save time. */
export function keyProblem(key: string): string | null {
  if (BUILT_IN.includes(key)) return `${key} is built in`;
  if (!FIELD_KEY.test(key)) return 'Use lowercase letters, digits, - and _, starting with a letter';
  return null;
}

/** The first thing wrong with a set of extra rows, or null. A blank row is an unfinished one and is not wrong. */
export function rowsProblem(rows: readonly ExtraField[]): string | null {
  const seen = new Set<string>();
  for (const r of rows) {
    const key = r.key.trim();
    if (key === '') continue;
    const bad = keyProblem(key);
    if (bad) return `${key}: ${bad}`;
    if (seen.has(key)) return `${key} appears twice`;
    seen.add(key);
  }
  return null;
}

// Drafts that were left behind by leaving the panel in some way other than saving or discarding:
// the Back button, another project, another card. Opening that card again brings the text back.
const parked = new Map<string, DraftState>();
const parkKey = (project: string, id: string) => `${project}\n${id}`;

/** Keeps a dirty draft for later. A clean one is not worth keeping. */
export function parkDraft(project: string, id: string, s: DraftState): void {
  if (dirtyOf(s).length > 0) parked.set(parkKey(project, id), s);
  else parked.delete(parkKey(project, id));
}

/** Returns the parked draft of a card, and forgets it. */
export function takeParked(project: string, id: string): DraftState | null {
  const k = parkKey(project, id);
  const s = parked.get(k) ?? null;
  parked.delete(k);
  return s;
}
