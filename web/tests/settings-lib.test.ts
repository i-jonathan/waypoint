import { describe, expect, it } from 'vitest';
import {
  addColumn,
  buildChange,
  clearWarnings,
  columnCounts,
  isDirty,
  moveRow,
  toDraft,
  validateDraft,
} from '../src/lib/settingsEdit';
import { accentWarnings } from '../src/lib/accent';
import type { CardView, Settings } from '../src/lib/types';

const base: Settings = {
  name: 'Home',
  prefix: 'T',
  next_id: 20,
  columns: [
    { name: 'Todo', wip: 0, moves_to: ['Doing', 'Done'], done: false },
    { name: 'Doing', wip: 2, moves_to: ['Todo'], done: false },
    { name: 'Done', wip: 0, moves_to: [], done: true },
  ],
  groups: [
    { name: 'Security', color: 'blue' },
    { name: 'Purchasing', color: 'amber' },
  ],
  priorities: ['P0', 'P1'],
  archive: { mode: 'manual', after_days: 14 },
  appearance: { theme: '', accent: 'cobalt', density: '', swimlane: '', textures: '' },
};

function card(id: string, column: string, group = '', priority = ''): CardView {
  return {
    id, title: id, column, group, priority, depends: [], created: null, done: null, body: '', extra: [],
    archived: false, blockers: [], next: false, checklist: { done: 0, total: 0 }, hash: id,
  };
}

describe('toDraft and buildChange', () => {
  it('round-trips untouched settings with no renames', () => {
    const c = buildChange(base, toDraft(base));
    expect(c.settings).toEqual(base);
    expect(c.column_renames).toEqual({});
    expect(c.group_renames).toEqual({});
    expect(c.priority_renames).toEqual({});
    expect(isDirty(base, toDraft(base))).toBe(false);
  });

  it('records a column rename and keeps moves_to pointing at the new name', () => {
    const d = toDraft(base);
    d.columns[0]!.name = 'Backlog';
    const c = buildChange(base, d);
    expect(c.column_renames).toEqual({ Todo: 'Backlog' });
    expect(c.settings.columns.map((x) => x.name)).toEqual(['Backlog', 'Doing', 'Done']);
    expect(c.settings.columns[1]!.moves_to).toEqual(['Backlog']);
  });

  it('records a swap of two names as two renames', () => {
    const d = toDraft(base);
    d.columns[0]!.name = 'Doing';
    d.columns[1]!.name = 'Todo';
    expect(buildChange(base, d).column_renames).toEqual({ Todo: 'Doing', Doing: 'Todo' });
  });

  it('does not call a reorder a rename, and orders moves_to by the new column order', () => {
    const d = toDraft(base);
    d.columns = moveRow(d.columns, 2, 0);
    const c = buildChange(base, d);
    expect(c.column_renames).toEqual({});
    expect(c.settings.columns.map((x) => x.name)).toEqual(['Done', 'Todo', 'Doing']);
    expect(c.settings.columns[1]!.moves_to).toEqual(['Done', 'Doing']);
  });

  it('turns an empty WIP into 0 and a number into a number', () => {
    const d = toDraft(base);
    d.columns[1]!.wip = '';
    d.columns[0]!.wip = '3';
    const c = buildChange(base, d);
    expect(c.settings.columns.map((x) => x.wip)).toEqual([3, 0, 0]);
  });

  it('records group and priority renames, and drops a removed column from moves_to', () => {
    const d = toDraft(base);
    d.groups[0]!.name = 'Safety';
    d.priorities[1]!.name = 'High';
    d.columns = d.columns.filter((r) => r.name !== 'Done');
    const c = buildChange(base, d);
    expect(c.group_renames).toEqual({ Security: 'Safety' });
    expect(c.priority_renames).toEqual({ P1: 'High' });
    expect(c.settings.columns[0]!.moves_to).toEqual(['Doing']);
  });

  it('adds a column named New column with no moves_to, and numbers the next one', () => {
    const d = toDraft(base);
    d.columns = addColumn(d.columns);
    d.columns = addColumn(d.columns);
    const added = buildChange(base, d).settings.columns.slice(3);
    expect(added.map((x) => x.name)).toEqual(['New column', 'New column 2']);
    expect(added.every((x) => x.moves_to.length === 0 && x.wip === 0 && !x.done)).toBe(true);
    expect(buildChange(base, d).column_renames).toEqual({});
  });

  it('keeps the archive days when the mode is not auto, and takes the typed days when it is', () => {
    const d = toDraft(base);
    d.archive.mode = 'auto';
    d.archive.days = '30';
    expect(buildChange(base, d).settings.archive).toEqual({ mode: 'auto', after_days: 30 });
  });
});

describe('moveRow', () => {
  it('moves a row up and down and ignores moves off either end', () => {
    expect(moveRow(['a', 'b', 'c'], 1, 0)).toEqual(['b', 'a', 'c']);
    expect(moveRow(['a', 'b', 'c'], 1, 2)).toEqual(['a', 'c', 'b']);
    expect(moveRow(['a', 'b', 'c'], 0, -1)).toEqual(['a', 'b', 'c']);
    expect(moveRow(['a', 'b', 'c'], 2, 3)).toEqual(['a', 'b', 'c']);
  });
});

describe('validateDraft', () => {
  it('passes untouched settings', () => {
    expect(validateDraft(toDraft(base))).toEqual({});
  });

  it('refuses an empty or repeated column name', () => {
    const d = toDraft(base);
    d.columns[0]!.name = '  ';
    d.columns[2]!.name = 'Doing';
    const errs = validateDraft(d);
    expect(errs[`col-${d.columns[0]!.key}-name`]).toMatch(/needs a name/);
    expect(errs[`col-${d.columns[2]!.key}-name`]).toMatch(/already/);
  });

  it('accepts an empty WIP or a positive whole number and nothing else', () => {
    const d = toDraft(base);
    const k = d.columns[0]!.key;
    for (const ok of ['', '1', '12']) {
      d.columns[0]!.wip = ok;
      expect(validateDraft(d)[`col-${k}-wip`]).toBeUndefined();
    }
    for (const bad of ['0', '-1', '1.5', 'x', '2e3']) {
      d.columns[0]!.wip = bad;
      expect(validateDraft(d)[`col-${k}-wip`]).toMatch(/whole number/);
    }
  });

  it('refuses repeated groups and priorities without regard to case', () => {
    const d = toDraft(base);
    d.groups[1]!.name = 'security';
    d.priorities[1]!.name = 'p0';
    const errs = validateDraft(d);
    expect(errs[`grp-${d.groups[1]!.key}-name`]).toMatch(/already/);
    expect(errs[`pri-${d.priorities[1]!.key}-name`]).toMatch(/already/);
  });

  it('refuses a custom colour that is not #rrggbb, and a custom accent too', () => {
    const d = toDraft(base);
    d.groups[0]!.color = '#12';
    d.appearance.accent = '#zzzzzz';
    const errs = validateDraft(d);
    expect(errs[`grp-${d.groups[0]!.key}-color`]).toMatch(/#rrggbb/);
    expect(errs['accent']).toMatch(/#rrggbb/);
  });

  it('wants whole days for auto archive', () => {
    const d = toDraft(base);
    d.archive.mode = 'auto';
    d.archive.days = '1.5';
    expect(validateDraft(d)['archive-days']).toMatch(/whole number/);
    d.archive.days = '7';
    expect(validateDraft(d)['archive-days']).toBeUndefined();
  });
});

describe('counts', () => {
  const cards = [
    card('T-1', 'Todo', 'Security', 'P0'),
    card('T-2', 'Todo', 'security'),
    card('T-3', 'Doing', 'Security'),
    card('T-4', 'Doing', 'Purchasing'),
    card('T-5', 'Done', 'Security', 'P1'),
    card('T-6', 'Done', 'Security'),
  ];

  it('counts the cards in each column', () => {
    expect(columnCounts(cards)).toEqual({ Todo: 2, Doing: 2, Done: 2 });
  });

  it('warns with a count for a removed group and a removed priority', () => {
    const d = toDraft(base);
    d.groups = d.groups.filter((g) => g.name !== 'Security');
    d.priorities = d.priorities.filter((p) => p.name !== 'P0');
    expect(clearWarnings(base, d, cards)).toEqual([
      'Removing Security clears it from 5 cards.',
      'Removing P0 clears it from 1 card.',
    ]);
  });

  it('does not warn for a rename, for a group with no cards, or for a name that is added back', () => {
    const d = toDraft(base);
    d.groups[0]!.name = 'Safety';
    expect(clearWarnings(base, d, cards)).toEqual([]);

    // Removed, then a new group of the same name: the cards keep it.
    const e = toDraft(base);
    e.groups = e.groups.filter((g) => g.name !== 'Security');
    e.groups.push({ key: 'new', origin: null, name: 'SECURITY', color: 'blue' });
    expect(clearWarnings(base, e, cards)).toEqual([]);

    const f = toDraft(base);
    f.priorities = f.priorities.filter((p) => p.name !== 'P1');
    expect(clearWarnings(base, f, cards.filter((c) => c.priority !== 'P1'))).toEqual([]);
  });
});

describe('accentWarnings', () => {
  it('is silent for a colour that reaches 3:1 against the canvas, and speaks for one that does not', () => {
    expect(accentWarnings('#ff7a3d', 'dark')).toEqual([]);
    expect(accentWarnings('#16201b', 'dark')[0]).toMatch(/3:1/);
    expect(accentWarnings('#e9eee8', 'light')[0]).toMatch(/3:1/);
  });
});
