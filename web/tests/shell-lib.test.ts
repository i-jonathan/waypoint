import { describe, expect, it } from 'vitest';
import { summarize } from '../src/lib/boardStats';
import { accentColor, groupColor } from '../src/lib/colors';
import { groupTerm, hasGroupTerm, toggleGroupTerm } from '../src/lib/filter';
import type { BoardView, CardView } from '../src/lib/types';

describe('toggleGroupTerm', () => {
  it('adds the term, quoted, to an empty query', () => {
    expect(toggleGroupTerm('', 'Security')).toBe('group:"Security"');
    expect(toggleGroupTerm('   ', 'Security')).toBe('group:"Security"');
  });

  it('appends to other words and removes again', () => {
    const on = toggleGroupTerm('p0 blocked', 'Home office');
    expect(on).toBe('p0 blocked group:"Home office"');
    expect(hasGroupTerm(on, 'Home office')).toBe(true);
    expect(toggleGroupTerm(on, 'Home office')).toBe('p0 blocked');
  });

  it('matches a typed term in any case, and leaves other groups alone', () => {
    const q = 'group:"security" group:"Purchasing"';
    expect(hasGroupTerm(q, 'Security')).toBe(true);
    expect(toggleGroupTerm(q, 'Security')).toBe('group:"Purchasing"');
  });

  it('does not match a longer name that starts the same', () => {
    expect(hasGroupTerm('group:"Security hardening"', 'Security')).toBe(false);
  });

  it('drops quotes from a name, since the query has no escape for them', () => {
    expect(groupTerm('say "hi"')).toBe('group:"say hi"');
  });
});

describe('summarize', () => {
  const base = { archived: false, blockers: [], group: '' } as unknown as CardView;
  const view = {
    settings: {
      columns: [
        { name: 'Todo', done: false },
        { name: 'Done', done: true },
      ],
    },
    cards: [
      { ...base, id: 'a', column: 'Todo', group: 'X' },
      { ...base, id: 'b', column: 'Done', group: 'X' },
      { ...base, id: 'c', column: 'Todo', group: 'Y', blockers: [{ id: 'a', missing: false }] },
      { ...base, id: 'd', column: 'Done', archived: true, group: 'X' },
      { ...base, id: 'e', column: 'Todo' },
    ],
  } as unknown as BoardView;

  it('counts cards, blocked cards and done/total per group, skipping archived cards', () => {
    expect(summarize(view)).toEqual({
      total: 4,
      blocked: 1,
      groups: { X: { done: 1, total: 2 }, Y: { done: 0, total: 1 } },
    });
  });
});

describe('colours', () => {
  it('gives a group name a dark and a light value, passes hex, and falls back to muted', () => {
    expect(groupColor('blue')).toBe('light-dark(#2f6fd6, #6fa8ff)');
    expect(groupColor('#ABCDEF')).toBe('#abcdef');
    expect(groupColor('nonsense')).toBe('var(--muted)');
    expect(groupColor('')).toBe('var(--muted)');
  });

  it('turns a project accent into a scheme-aware swatch colour', () => {
    expect(accentColor('orange')).toBe('light-dark(#e8601c, #ff7a3d)');
  });
});
