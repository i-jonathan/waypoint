// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { canDrop, columnIds, createDrag, dropIndex, type DragState } from '../src/lib/dnd';
import { adjacentColumn, focusTarget, readKey, stepTarget, type NavGrid } from '../src/lib/keys';
import { createLayout, lanesOf, loadCollapsed, saveCollapsed, zoneId } from '../src/lib/lanes';
import { anchorsIn, placeCard } from '../src/lib/cardOrder';
import type { BoardView, CardView, Column, Settings } from '../src/lib/types';

function card(id: string, column: string, over: Partial<CardView> = {}): CardView {
  return {
    id,
    title: `Card ${id}`,
    column,
    group: '',
    priority: '',
    depends: [],
    created: null,
    done: null,
    body: '',
    extra: [],
    archived: false,
    blockers: [],
    next: false,
    checklist: { done: 0, total: 0 },
    hash: `hash-${id}`,
    ...over,
  };
}

const columns: Column[] = [
  { name: 'Todo', wip: 0, moves_to: ['In progress'], done: false },
  { name: 'In progress', wip: 2, moves_to: ['Todo', 'Done'], done: false },
  { name: 'Done', wip: 0, moves_to: [], done: true },
];
const settings = { columns } as Settings;

describe('canDrop', () => {
  const cards = [card('T-1', 'Todo'), card('T-2', 'In progress'), card('T-3', 'In progress'), card('T-4', 'Done')];

  it('names the column a card cannot move to', () => {
    expect(canDrop(settings, cards, 'T-1', 'Done')).toBe('Todo cannot move to Done');
  });

  it('refuses a column at its WIP limit, counting the whole board', () => {
    expect(canDrop(settings, cards, 'T-1', 'In progress')).toBe('In progress is at its WIP limit (2/2)');
  });

  it('does not count archived cards against the limit', () => {
    const some = [card('T-1', 'Todo'), card('T-2', 'In progress'), card('T-3', 'In progress', { archived: true })];
    expect(canDrop(settings, some, 'T-1', 'In progress')).toBeNull();
  });

  it('always allows reordering within a column, even a full one', () => {
    expect(canDrop(settings, cards, 'T-2', 'In progress')).toBeNull();
  });

  it('allows an open column the card may reach', () => {
    expect(canDrop(settings, cards, 'T-2', 'Todo')).toBeNull();
  });

  it('refuses an unknown card or column rather than guessing', () => {
    expect(canDrop(settings, cards, 'T-9', 'Todo')).not.toBeNull();
    expect(canDrop(settings, cards, 'T-1', 'Nowhere')).not.toBeNull();
  });
});

describe('dropIndex', () => {
  const full = ['A', 'B', 'C', 'D', 'E'];

  it('puts a card right after the upper visible card in the FULL order, past cards the filter hides', () => {
    // The filter shows A and D. A drop between them is after A: B and C stay below it.
    expect(dropIndex(full, 'X', 'A', 'D')).toBe(1);
  });

  it('puts a card right before the lower visible card when nothing is above', () => {
    expect(dropIndex(full, 'X', null, 'D')).toBe(3);
  });

  it('puts a card at the bottom when nothing is visible', () => {
    expect(dropIndex(full, 'X', null, null)).toBe(5);
    expect(dropIndex([], 'X', null, null)).toBe(0);
  });

  it('counts the column without the moving card, as the server does', () => {
    // C moves to just after D: without C the column is A B D E, and D is at 2.
    expect(dropIndex(full, 'C', 'D', 'E')).toBe(3);
    // C moves to the top.
    expect(dropIndex(full, 'C', null, 'A')).toBe(0);
  });

  it('lists a column from the live cards only', () => {
    const cards = [card('A', 'Todo'), card('B', 'Done'), card('C', 'Todo', { archived: true }), card('D', 'Todo')];
    expect(columnIds(cards, 'Todo')).toEqual(['A', 'D']);
  });
});

describe('anchorsIn', () => {
  it('finds the nearest cards the view still shows, so a filtered view places a card where the whole board does', () => {
    const whole = [card('A', 'Todo'), card('B', 'Todo'), card('C', 'Todo'), card('D', 'Todo'), card('X', 'In progress')];
    const shown = whole.filter((c) => c.id !== 'B' && c.id !== 'C');
    // X goes to index 2 of the whole Todo column: after B. The view hides B and C.
    const place = anchorsIn(shown, whole, 'X', 'Todo', 2);
    expect(place.after).toBe('A');
    expect(place.before).toBe('D');
    expect(placeCard(shown, 'X', place).map((c) => c.id)).toEqual(['A', 'X', 'D']);
  });
});

describe('keys', () => {
  it('reads the keys the board uses', () => {
    const k = (key: string, extra: Partial<KeyboardEvent> = {}) =>
      readKey({ key, shiftKey: false, ctrlKey: false, metaKey: false, altKey: false, ...extra });
    expect(k('ArrowDown')).toEqual({ kind: 'focus', dir: 'down' });
    expect(k('ArrowRight', { shiftKey: true })).toEqual({ kind: 'move', dir: 'right' });
    expect(k('m')).toEqual({ kind: 'menu' });
    expect(k('Enter')).toEqual({ kind: 'open' });
    expect(k('Escape')).toEqual({ kind: 'escape' });
    expect(k('ArrowDown', { metaKey: true })).toBeNull();
    expect(k('M', { shiftKey: true })).toBeNull();
    expect(k('x')).toBeNull();
  });

  const grid: NavGrid = [
    [
      { id: 'A', lane: 'one' },
      { id: 'B', lane: 'two' },
    ],
    [],
    [
      { id: 'C', lane: 'two' },
      { id: 'D', lane: 'two' },
    ],
  ];

  it('moves focus within a column and across columns, skipping empty ones', () => {
    expect(focusTarget(grid, 'A', 'down')).toBe('B');
    expect(focusTarget(grid, 'A', 'up')).toBeNull();
    expect(focusTarget(grid, 'B', 'right')).toBe('C');
    expect(focusTarget(grid, 'D', 'left')).toBe('B');
    expect(focusTarget(grid, 'C', 'right')).toBeNull();
  });

  it('prefers a card in the same lane when crossing columns', () => {
    // A is in lane one, which the far column does not have: it falls back to the nearest row.
    expect(focusTarget(grid, 'A', 'right')).toBe('C');
  });

  it('finds the neighbours a card will have after Shift+Up or Shift+Down, within its lane', () => {
    const col: NavGrid = [[{ id: 'A', lane: '' }, { id: 'B', lane: '' }, { id: 'C', lane: '' }, { id: 'D', lane: '' }]];
    expect(stepTarget(col, 'C', 'up')).toEqual({ after: 'A', before: 'B' });
    expect(stepTarget(col, 'B', 'up')).toEqual({ after: null, before: 'A' });
    expect(stepTarget(col, 'A', 'up')).toBeNull();
    expect(stepTarget(col, 'B', 'down')).toEqual({ after: 'C', before: 'D' });
    expect(stepTarget(col, 'C', 'down')).toEqual({ after: 'D', before: null });
    expect(stepTarget(col, 'D', 'down')).toBeNull();
  });

  it('finds the adjacent column that takes the card', () => {
    const names = ['Todo', 'In progress', 'Done'];
    const refuse = (c: string) => (c === 'In progress' ? 'full' : null);
    expect(adjacentColumn(names, 'Todo', 'right', () => null)).toBe('In progress');
    expect(adjacentColumn(names, 'Todo', 'right', refuse)).toBe('Done');
    expect(adjacentColumn(names, 'Todo', 'left', () => null)).toBeNull();
    expect(adjacentColumn(names, 'Done', 'left', refuse)).toBe('Todo');
  });
});

describe('lanes', () => {
  const view = {
    settings: {
      ...settings,
      groups: [
        { name: 'Security', color: 'blue' },
        { name: 'Purchasing', color: 'amber' },
      ],
      priorities: ['P0', 'P1'],
    },
    cards: [
      card('A', 'Todo', { group: 'Security', priority: 'P0' }),
      card('B', 'Todo', { group: '', priority: '' }),
      card('C', 'Todo', { group: 'Old', priority: 'P9' }),
    ],
    warnings: [],
    settings_hash: 'h',
  } as BoardView;

  it('lists the groups, any name only cards carry, and the lane for none, last', () => {
    expect(lanesOf(view, 'group').map((l) => [l.key, l.name])).toEqual([
      ['Security', 'Security'],
      ['Purchasing', 'Purchasing'],
      ['Old', 'Old'],
      ['', 'No group'],
    ]);
    expect(lanesOf(view, 'priority').map((l) => l.name)).toEqual(['P0', 'P1', 'P9', 'No priority']);
  });

  it('reuses a cell whose cards did not change, so a move touches only two cells', () => {
    const layout = createLayout();
    const a = layout(view, 'none');
    const moved = { ...view, cards: view.cards.map((c) => (c.id === 'B' ? { ...c, column: 'In progress' } : c)) };
    const b = layout(moved, 'none');
    expect(b.cells.get(zoneId('Done'))).toBe(a.cells.get(zoneId('Done')));
    expect(b.cells.get(zoneId('Todo'))).not.toBe(a.cells.get(zoneId('Todo')));
    expect(b.cells.get(zoneId('In progress'))!.map((c) => c.id)).toEqual(['B']);
  });

  it('puts cards in the cell of their column and lane', () => {
    const { cells } = createLayout()(view, 'group');
    expect(cells.get(zoneId('Todo', 'Security'))!.map((c) => c.id)).toEqual(['A']);
    expect(cells.get(zoneId('Todo', ''))!.map((c) => c.id)).toEqual(['B']);
    expect(cells.get(zoneId('Done', 'Security'))).toEqual([]);
  });

  it('remembers collapsed lanes per project and mode, and survives damaged storage', () => {
    localStorage.clear();
    saveCollapsed('Home', 'group', new Set(['Security']));
    saveCollapsed('Home', 'priority', new Set(['P0']));
    saveCollapsed('Other', 'group', new Set(['X']));
    expect([...loadCollapsed('Home', 'group')]).toEqual(['Security']);
    expect([...loadCollapsed('Home', 'priority')]).toEqual(['P0']);
    expect([...loadCollapsed('Nobody', 'group')]).toEqual([]);
    localStorage.setItem('waypoint.lanes.Home', '{nonsense');
    expect([...loadCollapsed('Home', 'group')]).toEqual([]);
    saveCollapsed('Home', 'group', new Set(['Again']));
    expect([...loadCollapsed('Home', 'group')]).toEqual(['Again']);
  });
});

// ----- The pointer drag, on a hand-built board with measured boxes -----

function box(el: Element, left: number, top: number, width: number, height: number) {
  (el as HTMLElement).getBoundingClientRect = () =>
    ({ left, top, right: left + width, bottom: top + height, width, height, x: left, y: top }) as DOMRect;
}

/** Two columns of 200x400 side by side; cards 100 high, 10 apart, starting at y=10. */
function buildBoard(columnsAndCards: Array<[string, string[]]>) {
  document.body.innerHTML = '';
  const boardEl = document.createElement('div');
  boardEl.dataset.board = '';
  boardEl.dataset.scrollY = '';
  document.body.append(boardEl);
  box(boardEl, 0, 0, 220 * columnsAndCards.length, 400);
  const cards = new Map<string, HTMLElement>();
  columnsAndCards.forEach(([name, ids], c) => {
    const zone = document.createElement('section');
    zone.dataset.zone = name;
    zone.dataset.column = name;
    box(zone, c * 220, 0, 200, 400);
    const list = document.createElement('ul');
    list.dataset.list = '';
    box(list, c * 220, 0, 200, 400);
    zone.append(list);
    ids.forEach((id, i) => {
      const li = document.createElement('li');
      const el = document.createElement('div');
      el.dataset.cardId = id;
      el.textContent = id;
      box(el, c * 220, 10 + i * 110, 200, 100);
      li.append(el);
      list.append(li);
      cards.set(id, el);
    });
    boardEl.append(zone);
  });
  return { boardEl, cards };
}

/**
 * A frame that runs at once, so a test sees the result of each pointer move.
 * A frame asked for from inside a frame (auto-scroll, which jsdom never stops
 * because nothing clamps its scroll position) waits a tick instead.
 */
let inFrame = false;
function syncFrames(cb: FrameRequestCallback): number {
  if (inFrame) {
    setTimeout(() => cb(0), 0);
    return 1;
  }
  inFrame = true;
  try {
    cb(0);
  } finally {
    inFrame = false;
  }
  return 1;
}

function pointer(type: string, x: number, y: number, init: PointerEventInit = {}): PointerEvent {
  return new PointerEvent(type, { clientX: x, clientY: y, pointerId: 1, pointerType: 'mouse', isPrimary: true, button: 0, bubbles: true, cancelable: true, ...init });
}

describe('createDrag', () => {
  let onDrop: ReturnType<typeof vi.fn>;
  let states: Array<DragState | null>;

  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', syncFrames);
    onDrop = vi.fn();
    states = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    document.body.innerHTML = '';
    document.body.className = '';
  });

  function make(refuse: Record<string, string> = {}) {
    return createDrag({
      canDrop: (_id, column) => refuse[column] ?? null,
      onDrop: onDrop as never,
      onChange: (s) => states.push(s),
    });
  }

  it('does not start a drag for a movement under 4px, and the click goes through', () => {
    const { cards } = buildBoard([['Todo', ['A', 'B']]]);
    const drag = make();
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 52, 52));
    expect(drag.active).toBe(false);
    window.dispatchEvent(pointer('pointerup', 52, 52));
    expect(onDrop).not.toHaveBeenCalled();
    expect(states).toEqual([]);
    expect(document.querySelector('.drag-clone')).toBeNull();
  });

  it('drags past the threshold and drops with the index among the visible cards', () => {
    const { cards } = buildBoard([
      ['Todo', ['A', 'B']],
      ['Doing', ['C', 'D']],
    ]);
    const drag = make();
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 55)); // 10px: starts
    expect(drag.active).toBe(true);
    expect(document.querySelector('.drag-clone')).not.toBeNull();
    // Over the second column, below C (10..110) and above D (120..220): between them.
    window.dispatchEvent(pointer('pointermove', 300, 115));
    window.dispatchEvent(pointer('pointerup', 300, 115));
    expect(onDrop).toHaveBeenCalledTimes(1);
    const [id, column, index, target] = onDrop.mock.calls[0]!;
    expect([id, column, index]).toEqual(['A', 'Doing', 1]);
    expect(target).toMatchObject({ after: 'C', before: 'D', lane: undefined });
    expect(drag.active).toBe(false);
  });

  it('counts the dragged card out of its own column', () => {
    const { cards } = buildBoard([['Todo', ['A', 'B', 'C']]]);
    const drag = make();
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 70));
    // Below B (120..220) and above C (230..330).
    window.dispatchEvent(pointer('pointermove', 60, 226));
    window.dispatchEvent(pointer('pointerup', 60, 226));
    expect(onDrop.mock.calls[0]!.slice(0, 3)).toEqual(['A', 'Todo', 1]);
    expect(onDrop.mock.calls[0]![3]).toMatchObject({ after: 'B', before: 'C' });
  });

  it('reports the columns that refuse the card, and drops nothing on one', () => {
    const { cards } = buildBoard([
      ['Todo', ['A']],
      ['Done', []],
    ]);
    const drag = make({ Done: 'Todo cannot move to Done' });
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 70, 60));
    expect(states.at(-1)?.blocked.get('Done')).toBe('Todo cannot move to Done');
    window.dispatchEvent(pointer('pointermove', 300, 60));
    expect(states.at(-1)?.target).toBeNull();
    window.dispatchEvent(pointer('pointerup', 300, 60));
    expect(onDrop).not.toHaveBeenCalled();
  });

  it('cancels on Escape: nothing is dropped and the lifted copy goes', () => {
    const { cards } = buildBoard([['Todo', ['A', 'B']]]);
    const drag = make();
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 200));
    expect(drag.active).toBe(true);
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(drag.active).toBe(false);
    window.dispatchEvent(pointer('pointerup', 60, 200));
    expect(onDrop).not.toHaveBeenCalled();
    expect(states.at(-1)).toBeNull();
    expect(document.body.classList.contains('wp-dragging')).toBe(false);
  });

  it('leaves a touch that moves before the long press to the browser, which scrolls', () => {
    vi.useFakeTimers();
    try {
      const { cards } = buildBoard([['Todo', ['A']]]);
      const drag = make();
      drag.begin(pointer('pointerdown', 50, 50, { pointerType: 'touch' }), 'A', cards.get('A')!);
      window.dispatchEvent(pointer('pointermove', 50, 90, { pointerType: 'touch' }));
      vi.advanceTimersByTime(500);
      expect(drag.active).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it('picks a card up with a long press that stays still', () => {
    vi.useFakeTimers();
    try {
      const { cards } = buildBoard([['Todo', ['A']]]);
      const drag = make();
      drag.begin(pointer('pointerdown', 50, 50, { pointerType: 'touch' }), 'A', cards.get('A')!);
      vi.advanceTimersByTime(400);
      expect(drag.active).toBe(true);
      window.dispatchEvent(pointer('pointercancel', 50, 50, { pointerType: 'touch' }));
      expect(drag.active).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it('lifts the card by growing its shadow from 3px to 6px, and under reduced motion shows it lifted at once', () => {
    const { cards } = buildBoard([['Todo', ['A']]]);
    const drag = make();
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 60));
    const clone = document.querySelector<HTMLElement>('.drag-clone')!;
    expect(clone.style.transition).toContain('box-shadow');
    window.dispatchEvent(pointer('pointerup', 60, 60));

    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), media: q }));
    const again = make();
    again.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 60));
    const still = [...document.querySelectorAll<HTMLElement>('.drag-clone')].at(-1)!;
    expect(still.style.boxShadow).toBe('6px 6px 0 var(--edge)');
    expect(still.style.transition).toBe('');
    window.dispatchEvent(pointer('pointerup', 60, 60));
    // Dropped under reduced motion, the lifted copy goes at once instead of settling.
    expect([...document.querySelectorAll('.drag-clone')]).toHaveLength(1);
  });

  it('stops the click that follows a drag, so the card does not open', () => {
    const { cards } = buildBoard([['Todo', ['A', 'B']]]);
    const drag = make();
    const clicked = vi.fn();
    document.addEventListener('click', clicked);
    drag.begin(pointer('pointerdown', 50, 50), 'A', cards.get('A')!);
    window.dispatchEvent(pointer('pointermove', 60, 200));
    window.dispatchEvent(pointer('pointerup', 60, 200));
    cards.get('A')!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    expect(clicked).not.toHaveBeenCalled();
    document.removeEventListener('click', clicked);
  });
});
