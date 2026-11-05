import { beforeEach, describe, expect, it } from 'vitest';
import {
  absorb,
  dirtyOf,
  fieldsOf,
  keyProblem,
  rowsProblem,
  parkDraft,
  saved,
  startDraft,
  takeParked,
  toPatch,
  type DraftState,
} from '../src/lib/draft';
import type { CardView } from '../src/lib/types';

function card(over: Partial<CardView> = {}): CardView {
  return {
    id: 'T-1',
    title: 'Title',
    column: 'Todo',
    group: '',
    priority: '',
    depends: [],
    created: null,
    done: null,
    body: 'body',
    extra: [],
    archived: false,
    blockers: [],
    next: false,
    checklist: { done: 0, total: 0 },
    hash: 'h1',
    ...over,
  };
}

const typed = (s: DraftState, over: Partial<DraftState['draft']>): DraftState => ({ ...s, draft: { ...s.draft, ...over } });

describe('dirtyOf', () => {
  it('is empty for a fresh draft', () => {
    expect(dirtyOf(startDraft(card()))).toEqual([]);
  });

  it('names the fields that differ', () => {
    const s = typed(startDraft(card()), { title: 'New', depends: ['T-2'] });
    expect(dirtyOf(s)).toEqual(['title', 'depends']);
  });

  it('ignores blank extra rows and the order of rows', () => {
    const s = startDraft(card({ extra: [{ key: 'a', value: '1' }, { key: 'b', value: '2' }] }));
    expect(dirtyOf(typed(s, { extra: [{ key: 'b', value: '2' }, { key: 'a', value: '1' }, { key: '', value: '' }] }))).toEqual([]);
    expect(dirtyOf(typed(s, { extra: [{ key: 'a', value: '1' }] }))).toEqual(['extra']);
  });
});

describe('toPatch', () => {
  it('sends only the dirty fields', () => {
    const s = typed(startDraft(card()), { title: 'New' });
    expect(toPatch(s, dirtyOf(s))).toEqual({ title: 'New' });
  });

  it('sets, changes and clears extra fields', () => {
    const s = startDraft(card({ extra: [{ key: 'owner', value: 'me' }, { key: 'sprint', value: '4' }] }));
    const t = typed(s, { extra: [{ key: 'owner', value: 'you' }, { key: 'size', value: 'L' }] });
    expect(toPatch(t, dirtyOf(t))).toEqual({ set: { owner: 'you', size: 'L', sprint: '' } });
  });

  it('clears a group by sending the empty string', () => {
    const s = typed(startDraft(card({ group: 'Security' })), { group: '' });
    expect(toPatch(s, dirtyOf(s))).toEqual({ group: '' });
  });
});

describe('absorb: what a store refresh does to a draft', () => {
  it('follows the server entirely while nothing is dirty', () => {
    const s = absorb(startDraft(card()), card({ title: 'Theirs', body: 'new', hash: 'h2' }), null);
    expect(s.draft.title).toBe('Theirs');
    expect(s.draft.body).toBe('new');
    expect(s.baseHash).toBe('h2');
    expect(dirtyOf(s)).toEqual([]);
  });

  it('never touches a dirty field, and keeps the hash the draft started from', () => {
    const s0 = typed(startDraft(card()), { body: 'my words' });
    const s = absorb(s0, card({ title: 'Theirs', body: 'their words', hash: 'h2' }), null);
    expect(s.draft.body).toBe('my words');
    expect(s.baseHash).toBe('h1');
    // the field I did not touch follows the server and is not dirty
    expect(s.draft.title).toBe('Theirs');
    expect(dirtyOf(s)).toEqual(['body']);
  });

  it('rebases the hash when the card changed in a way that touches no draft field: a move, a drag, an archive', () => {
    const s0 = typed(startDraft(card()), { title: 'Mine', body: 'my words' });
    const s = absorb(s0, card({ column: 'Done', hash: 'h2' }), null);
    expect(s.baseHash).toBe('h2');
    expect(s.draft.title).toBe('Mine');
    expect(dirtyOf(s)).toEqual(['title', 'body']);
    expect(absorb(s0, card({ archived: true, hash: 'h3' }), null).baseHash).toBe('h3');
  });

  it.each([
    ['title', { title: 'Theirs' }],
    ['group', { group: 'Security' }],
    ['priority', { priority: 'P0' }],
    ['depends', { depends: ['T-2'] }],
    ['body', { body: 'their words' }],
    ['extra', { extra: [{ key: 'a', value: 'b' }] }],
  ])('keeps the original hash when %s changed outside, even in a field that is not dirty', (_name, change) => {
    const s = absorb(typed(startDraft(card()), { title: 'Mine' }), card({ ...change, hash: 'h2' }), null);
    expect(s.baseHash).toBe('h1');
  });

  it('keeps the original hash for a change to a dirty field too', () => {
    const s = absorb(typed(startDraft(card()), { body: 'mine' }), card({ body: 'theirs', column: 'Done', hash: 'h2' }), null);
    expect(s.baseHash).toBe('h1');
  });

  it('rebases the hash after its own move: only the hash changed', () => {
    const s0 = typed(startDraft(card()), { body: 'my words' });
    const s = absorb(s0, card({ column: 'Done', hash: 'h2' }), { body: undefined });
    expect(s.baseHash).toBe('h2');
    expect(s.draft.body).toBe('my words');
  });

  it('rebases after its own tick: the body differs by exactly that tick', () => {
    const s0 = typed(startDraft(card({ body: '- [ ] a' })), { title: 'Mine' });
    const s = absorb(s0, card({ body: '- [x] a', hash: 'h2' }), { body: '- [x] a' });
    expect(s.baseHash).toBe('h2');
    expect(s.draft.title).toBe('Mine');
    expect(s.draft.body).toBe('- [x] a');
    expect(dirtyOf(s)).toEqual(['title']);
  });

  it('does not rebase on a change that is not its own, even while it has a write in flight', () => {
    const s0 = typed(startDraft(card({ body: '- [ ] a' })), { title: 'Mine' });
    const s = absorb(s0, card({ body: '- [x] a', title: 'Theirs', hash: 'h2' }), { body: '- [x] a' });
    expect(s.baseHash).toBe('h1');
  });
});

describe('saved', () => {
  it('resets the draft from the response, which the server normalised', () => {
    const s0 = typed(startDraft(card()), { title: 'New   ', body: 'a  \r\nb' });
    const sent = s0.draft;
    const s = saved(s0, sent, card({ title: 'New', body: 'a\nb', hash: 'h2' }));
    expect(s.draft.title).toBe('New');
    expect(s.draft.body).toBe('a\nb');
    expect(s.baseHash).toBe('h2');
    expect(dirtyOf(s)).toEqual([]);
  });

  it('keeps what was typed while the save was in flight', () => {
    const s0 = typed(startDraft(card()), { title: 'New' });
    const sent = s0.draft;
    const s = saved(typed(s0, { body: 'typed meanwhile' }), sent, card({ title: 'New', hash: 'h2' }));
    expect(s.draft.body).toBe('typed meanwhile');
    expect(dirtyOf(s)).toEqual(['body']);
    expect(s.baseHash).toBe('h2');
  });
});

describe('keyProblem', () => {
  it.each(['owner', 'a', 'size-2', 'x_y'])('accepts %s', (k) => expect(keyProblem(k)).toBeNull());
  it.each(['', 'Owner', '1a', 'a b', 'a.b', '-a'])('refuses %j', (k) => expect(keyProblem(k)).not.toBeNull());
  it.each(['group', 'priority', 'depends', 'created', 'done', 'column', 'id', 'rank'])('refuses the built-in %s', (k) => {
    expect(keyProblem(k)).toMatch(/built in/);
  });
});

describe('parked drafts', () => {
  beforeEach(() => takeParked('p', 'T-1'));

  it('keeps a dirty draft for the card until it is taken', () => {
    const s = typed(startDraft(card()), { body: 'mine' });
    parkDraft('p', 'T-1', s);
    expect(takeParked('p', 'T-1')).toBe(s);
    expect(takeParked('p', 'T-1')).toBeNull();
  });

  it('does not park a clean draft', () => {
    parkDraft('p', 'T-1', startDraft(card()));
    expect(takeParked('p', 'T-1')).toBeNull();
  });
});

describe('fieldsOf', () => {
  it('copies the editable fields', () => {
    const c = card({ depends: ['T-2'], extra: [{ key: 'a', value: 'b' }] });
    const f = fieldsOf(c);
    expect(f).toEqual({ title: 'Title', group: '', priority: '', depends: ['T-2'], body: 'body', extra: [{ key: 'a', value: 'b' }] });
    expect(f.depends).not.toBe(c.depends);
  });
});

describe('rowsProblem', () => {
  it('is null for good and for blank rows', () => {
    expect(rowsProblem([{ key: 'owner', value: 'me' }, { key: '', value: '' }])).toBeNull();
  });
  it('names a bad key and a repeated one', () => {
    expect(rowsProblem([{ key: 'Owner', value: 'x' }])).toMatch(/^Owner: /);
    expect(rowsProblem([{ key: 'group', value: 'x' }])).toMatch(/built in/);
    expect(rowsProblem([{ key: 'a', value: '1' }, { key: 'a', value: '2' }])).toBe('a appears twice');
  });
});
