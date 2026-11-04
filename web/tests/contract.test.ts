// The fixtures in tests/fixtures are real output of the Go server: run
// `waypoint serve --no-open` with a temporary WAYPOINT_CONFIG and HOME, then
// save the JSON. Each test ties the TypeScript types to those fixtures in both
// directions: a field the server adds or drops fails the key comparison, and a
// field the types add or drop fails type checking (svelte-check covers this
// folder).
import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api } from '../src/lib/api';
import type {
  Appearance,
  ArchiveSettings,
  Blocker,
  BoardResponse,
  BoardView,
  Card,
  CardView,
  Checklist,
  Column,
  ExtraField,
  GlobalSettings,
  Group,
  ProjectInfo,
  ResolvedAppearance,
  Settings,
} from '../src/lib/types';

function fixture<T>(name: string): { text: string; value: T } {
  const text = readFileSync(new URL(`./fixtures/${name}.json`, import.meta.url), 'utf8');
  return { text, value: JSON.parse(text) as T };
}

type Keys<T> = Record<keyof T, true>;

/** The keys of value, as the types list them. */
function expectKeys<T extends object>(value: object, keys: Keys<T>) {
  expect(Object.keys(value).sort()).toEqual(Object.keys(keys).sort());
}

const cardKeys: Keys<Card> = {
  id: true, title: true, column: true, group: true, priority: true, depends: true,
  created: true, done: true, body: true, extra: true,
};
const cardViewKeys: Keys<CardView> = {
  ...cardKeys, archived: true, blockers: true, next: true, checklist: true, hash: true,
};
const settingsKeys: Keys<Settings> = {
  name: true, prefix: true, next_id: true, columns: true, groups: true, priorities: true,
  archive: true, appearance: true,
};
const boardKeys: Keys<BoardView> = { settings: true, cards: true, warnings: true, settings_hash: true };
const boardResponseKeys: Keys<BoardResponse> = { board: true, appearance: true, readonly: true };
const resolvedKeys: Keys<ResolvedAppearance> = {
  theme: true, accent: true, density: true, swimlane: true, textures: true, source: true,
};
const appearanceKeys: Keys<Appearance> = { theme: true, accent: true, density: true, swimlane: true, textures: true };
const columnKeys: Keys<Column> = { name: true, wip: true, moves_to: true, done: true };
const groupKeys: Keys<Group> = { name: true, color: true };
const archiveKeys: Keys<ArchiveSettings> = { mode: true, after_days: true };
const checklistKeys: Keys<Checklist> = { done: true, total: true };
const blockerKeys: Keys<Blocker> = { id: true, missing: true };
const extraKeys: Keys<ExtraField> = { key: true, value: true };
const projectKeys: Keys<Omit<ProjectInfo, 'error'>> = { name: true, storage: true, path: true, accent: true };
const globalKeys: Keys<GlobalSettings> = {
  port: true, open_browser: true, tls: true, appearance: true, restart_required: true,
};

const fetchMock = vi.fn<typeof fetch>();

function serve(status: number, text: string) {
  fetchMock.mockImplementation(async () => new Response(text, { status, headers: { 'Content-Type': 'application/json' } }));
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('contract with the Go server', () => {
  it('lists projects', async () => {
    const { text, value } = fixture<ProjectInfo[]>('projects');
    expect(value.length).toBeGreaterThan(0);
    for (const p of value) expectKeys(p, projectKeys);

    serve(200, text);
    expect(await api.projects()).toEqual(value);
    expect(String(fetchMock.mock.calls[0]![0])).toBe('/api/projects');
  });

  it('reads a board', async () => {
    const { text, value } = fixture<BoardResponse>('board');
    expectKeys(value, boardResponseKeys);
    expectKeys(value.board, boardKeys);
    expectKeys(value.board.settings, settingsKeys);
    expectKeys(value.board.settings.archive, archiveKeys);
    expectKeys(value.board.settings.appearance, appearanceKeys);
    expectKeys(value.appearance, resolvedKeys);
    for (const c of value.board.settings.columns) expectKeys(c, columnKeys);
    for (const g of value.board.settings.groups) expectKeys(g, groupKeys);
    expect(value.board.settings.groups.length).toBeGreaterThan(0);

    expect(value.board.cards.length).toBeGreaterThan(0);
    for (const c of value.board.cards) {
      expectKeys(c, cardViewKeys);
      expectKeys(c.checklist, checklistKeys);
      for (const b of c.blockers) expectKeys(b, blockerKeys);
      for (const f of c.extra) expectKeys(f, extraKeys);
    }
    // The fixture covers every shape of the date fields and the derived facts.
    const cards = value.board.cards;
    expect(cards.some((c) => c.created?.includes('T'))).toBe(true);
    expect(cards.some((c) => c.created !== null && !c.created.includes('T'))).toBe(true);
    expect(cards.some((c) => c.done === null)).toBe(true);
    expect(cards.some((c) => typeof c.done === 'string')).toBe(true);
    expect(cards.some((c) => c.blockers.length > 0)).toBe(true);
    expect(cards.some((c) => c.extra.length > 0)).toBe(true);
    expect(cards.some((c) => c.next)).toBe(true);
    expect(value.board.settings_hash).toMatch(/^[0-9a-f]{64}$/);

    serve(200, text);
    expect(await api.board('Home reno')).toEqual(value);
    expect(String(fetchMock.mock.calls[0]![0])).toBe('/api/projects/Home%20reno/board');
  });

  it('reads the global settings', async () => {
    const { text, value } = fixture<GlobalSettings>('settings');
    expectKeys(value, globalKeys);
    expectKeys(value.appearance, appearanceKeys);

    serve(200, text);
    expect(await api.settings()).toEqual(value);
  });

  it('reports a conflict with the current card', async () => {
    const { text } = fixture<{ error: string; code: string; current: CardView }>('conflict');
    serve(409, text);
    const err = (await api.patch('Home reno', 'H-1', { title: 'x' }, 'stale').catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.code).toBe('conflict');
    const current = err.current as CardView;
    expectKeys(current, cardViewKeys);
    expect(current.id).toBe('H-1');
  });

  it('reports a parse error with its line', async () => {
    const { text } = fixture<{ error: string; code: string; line: number }>('parse-error');
    serve(423, text);
    const err = (await api.board('Garden').catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(423);
    expect(err.code).toBe('parse');
    expect(err.line).toBe(24);
  });
});
