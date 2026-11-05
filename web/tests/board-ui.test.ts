// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Board from '../src/components/Board.svelte';
import { ApiError } from '../src/lib/api';
import { createBoardStore, type BoardApi, type BoardStore } from '../src/lib/board.svelte';
import { toasts } from '../src/lib/events.svelte';
import { navigate, route } from '../src/lib/router.svelte';
import type { BoardResponse, CardView, Column, Settings, Swimlane } from '../src/lib/types';

const columns: Column[] = [
  { name: 'Todo', wip: 0, moves_to: ['In progress', 'Done'], done: false },
  { name: 'In progress', wip: 2, moves_to: ['Todo', 'Done'], done: false },
  { name: 'Done', wip: 0, moves_to: ['In progress'], done: true },
];

function settingsOf(over: Partial<Settings> = {}): Settings {
  return {
    name: 'Home',
    prefix: 'T',
    next_id: 20,
    columns,
    groups: [
      { name: 'Security', color: 'blue' },
      { name: 'Purchasing', color: 'amber' },
    ],
    priorities: ['P0', 'P1'],
    archive: { mode: 'manual', after_days: 0 },
    appearance: { theme: '', accent: '', density: '', swimlane: '', textures: '' },
    ...over,
  };
}

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

function response(cards: CardView[], settings = settingsOf()): BoardResponse {
  return {
    board: { settings, cards, warnings: [], settings_hash: 'sh' },
    appearance: { theme: 'dark', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
    readonly: false,
  };
}

function fakeApi(whole: CardView[], filtered?: (q: string) => CardView[], settings = settingsOf()) {
  return {
    board: vi.fn<BoardApi['board']>(async (_p, q) => response(q && filtered ? filtered(q) : whole, settings)),
    createCard: vi.fn<BoardApi['createCard']>(),
    patch: vi.fn<BoardApi['patch']>(async (_p, id, body) => ({ ...whole.find((c) => c.id === id)!, ...body }) as CardView),
    move: vi.fn<BoardApi['move']>(async (_p, id, column) => ({ ...whole.find((c) => c.id === id)!, column })),
    check: vi.fn<BoardApi['check']>(),
    deleteCard: vi.fn<BoardApi['deleteCard']>(),
    archiveDone: vi.fn<BoardApi['archiveDone']>(),
    putProjectSettings: vi.fn<BoardApi['putProjectSettings']>(),
  };
}

let store: BoardStore | null = null;

async function mount(api: ReturnType<typeof fakeApi>, lane: Swimlane = 'none', onAddCard = vi.fn()) {
  store = createBoardStore(api, 'Home');
  await store.refresh();
  const view = render(Board, { props: { store, project: 'Home', lane, onAddCard } });
  return { ...view, store, onAddCard };
}

const names = (el: HTMLElement) => [...el.querySelectorAll('[data-card-id]')].map((c) => (c as HTMLElement).dataset.cardId);
const zoneOf = (column: string, lane?: string): HTMLElement =>
  document.querySelector<HTMLElement>(`[data-zone][data-column="${column}"]${lane === undefined ? '' : `[data-lane="${lane}"]`}`)!;

// ----- Geometry: jsdom has no layout, so each zone and card is given a box -----

function setBox(el: Element, left: number, top: number, width: number, height: number) {
  (el as HTMLElement).getBoundingClientRect = () =>
    ({ left, top, right: left + width, bottom: top + height, width, height, x: left, y: top }) as DOMRect;
}

/** Columns 220px apart; lanes 400px apart; cards 100px high, 110px apart from 10px below the zone's top. */
function layOut(laneKeys: string[] | null) {
  const board = document.querySelector('[data-board]')!;
  setBox(board, 0, 0, 2000, 2000);
  for (const zone of document.querySelectorAll<HTMLElement>('[data-zone]')) {
    const col = columns.findIndex((c) => c.name === zone.dataset.column);
    const row = laneKeys === null ? 0 : laneKeys.indexOf(zone.dataset.lane ?? '');
    const left = col * 220;
    const top = row * 400;
    setBox(zone, left, top, 200, 380);
    const list = zone.querySelector('[data-list]')!;
    setBox(list, left, top, 200, 380);
    [...zone.querySelectorAll('[data-card-id]')].forEach((el, i) => setBox(el, left, top + 10 + i * 110, 200, 100));
  }
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

/** Like layOut, but worked out from the DOM at every call, so cards that appear later have boxes too. */
function autoLayOut(laneKeys: string[] | null) {
  const box = (left: number, top: number, width: number, height: number) =>
    ({ left, top, right: left + width, bottom: top + height, width, height, x: left, y: top }) as DOMRect;
  return vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const el = this as HTMLElement;
    if (el.hasAttribute('data-board')) return box(0, 0, 2000, 2000);
    const zone = el.closest<HTMLElement>('[data-zone]');
    if (!zone) return box(0, 0, 0, 0);
    const col = columns.findIndex((c) => c.name === zone.dataset.column);
    const row = laneKeys === null ? 0 : laneKeys.indexOf(zone.dataset.lane ?? '');
    const left = col * 220;
    const top = row * 400;
    if (el.hasAttribute('data-card-id')) {
      const at = [...zone.querySelectorAll('[data-card-id]')].indexOf(el);
      return box(left, top + 10 + at * 110, 200, 100);
    }
    return box(left, top, 200, 380);
  });
}

function pointer(type: string, x: number, y: number, init: PointerEventInit = {}): PointerEvent {
  return new PointerEvent(type, { clientX: x, clientY: y, pointerId: 1, pointerType: 'mouse', isPrimary: true, button: 0, bubbles: true, cancelable: true, ...init });
}

async function dragTo(id: string, x: number, y: number) {
  const el = screen.getByRole('button', { name: new RegExp(`^${id} `) });
  const r = el.getBoundingClientRect();
  el.dispatchEvent(pointer('pointerdown', r.left + 20, r.top + 20));
  window.dispatchEvent(pointer('pointermove', r.left + 32, r.top + 28));
  window.dispatchEvent(pointer('pointermove', x, y));
  window.dispatchEvent(pointer('pointerup', x, y));
}

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', syncFrames);
  navigate('/');
});

afterEach(() => {
  cleanup();
  store?.destroy();
  store = null;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  toasts.clear();
  localStorage.clear();
  document.body.className = '';
  // A dropped card's lifted copy lingers for its settling animation.
  for (const el of document.querySelectorAll('.drag-clone')) el.remove();
});

// ----- The card -----

describe('the card', () => {
  it('shows the next-up card with a NEXT tag, and names it for assistive tech', async () => {
    await mount(fakeApi([card('T-1', 'Todo', { next: true, priority: 'P0' })]));
    const el = screen.getByRole('button', { name: 'T-1 Card T-1, Todo, next up' });
    expect(el).toHaveClass('next');
    expect(within(el).getByText('NEXT')).toBeInTheDocument();
  });

  it('shows what blocks a card, with missing cards marked', async () => {
    const blocked = card('T-2', 'Todo', { blockers: [{ id: 'T-17', missing: false }, { id: 'T-99', missing: true }] });
    await mount(fakeApi([blocked]));
    const el = screen.getByRole('button', { name: 'T-2 Card T-2, Todo, blocked by T-17, T-99 (missing)' });
    expect(el).toHaveTextContent('Blocked by T-17, T-99 (missing)');
  });

  it('puts every part of the label in order: id, title, column, blocked, next up', async () => {
    const c = card('T-3', 'In progress', { next: true, blockers: [{ id: 'T-1', missing: false }] });
    await mount(fakeApi([c]));
    expect(screen.getByRole('button', { name: 'T-3 Card T-3, In progress, blocked by T-1, next up' })).toBeInTheDocument();
  });

  it('draws a done card flat, with the title struck through and a check before the id', async () => {
    await mount(fakeApi([card('T-4', 'Done')]));
    const el = screen.getByRole('button', { name: /^T-4 / });
    expect(el).toHaveClass('done');
    expect(within(el).getByText('Card T-4')).toHaveClass('struck');
    expect(el.querySelector('.id svg')).not.toBeNull();
  });

  it('shows the group on the route line and the checklist progress', async () => {
    const c = card('T-5', 'Todo', { group: 'Security', priority: 'P0', checklist: { done: 1, total: 4 } });
    await mount(fakeApi([c]));
    const el = screen.getByRole('button', { name: /^T-5 / });
    expect(el).toHaveTextContent('Security');
    expect(el).toHaveTextContent('1/4');
    expect(within(el).getByText('P0')).toHaveClass('p0');
  });

  it('draws only the highest priority as a filled chip', async () => {
    await mount(fakeApi([card('T-6', 'Todo', { priority: 'P1' })]));
    expect(within(screen.getByRole('button', { name: /^T-6 / })).getByText('P1')).toHaveClass('p1');
  });
});

// ----- The column -----

describe('the column', () => {
  it('shows each sign with the count, and n/WIP when a limit is set', async () => {
    await mount(fakeApi([card('T-1', 'Todo'), card('T-2', 'In progress')]));
    const heads = screen.getAllByRole('heading', { level: 2 });
    expect(heads.map((h) => h.textContent?.replace(/\s+/g, ' ').trim())).toEqual(['Todo 1', 'In progress 1/2', 'Done 0']);
  });

  it('is a quiet drop target when empty, with no fake cards', async () => {
    await mount(fakeApi([card('T-1', 'Todo')]));
    const done = zoneOf('Done');
    expect(done).toHaveTextContent('Nothing here yet');
    expect(names(done)).toEqual([]);
  });

  it('shows an Add card row at the end that calls the stub', async () => {
    const { onAddCard } = await mount(fakeApi([card('T-1', 'Todo')]));
    await fireEvent.click(within(zoneOf('Todo')).getByRole('button', { name: /Add card/ }));
    expect(onAddCard).toHaveBeenCalledWith('Todo');
  });

  it('does not rebuild the cards a move did not touch', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo'), card('D', 'Done'), card('E', 'Done')]);
    await mount(api);
    const untouched = [screen.getByRole('button', { name: /^D / }), screen.getByRole('button', { name: /^E / })];
    const sibling = screen.getByRole('button', { name: /^B / });
    const a = screen.getByRole('button', { name: /^A / });
    a.focus();
    await fireEvent.keyDown(a, { key: 'ArrowRight', shiftKey: true });
    await waitFor(() => expect(names(zoneOf('In progress'))).toEqual(['A']));
    expect(screen.getByRole('button', { name: /^D / })).toBe(untouched[0]);
    expect(screen.getByRole('button', { name: /^E / })).toBe(untouched[1]);
    expect(screen.getByRole('button', { name: /^B / })).toBe(sibling);
  });

  it('opens the card route on click and on Enter', async () => {
    await mount(fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo')]));
    await fireEvent.click(screen.getByRole('button', { name: /^T-1 / }));
    expect(route).toMatchObject({ name: 'card', project: 'Home', id: 'T-1' });
    const second = screen.getByRole('button', { name: /^T-2 / });
    second.focus();
    await fireEvent.keyDown(second, { key: 'Enter' });
    expect(route.id).toBe('T-2');
  });

  it('keeps one card in the tab order and moves focus with the arrow keys', async () => {
    await mount(fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo'), card('T-3', 'In progress')]));
    const tabbable = screen.getAllByRole('button', { name: /^T-\d / }).filter((b) => b.getAttribute('tabindex') === '0');
    expect(tabbable).toHaveLength(1);
    const one = screen.getByRole('button', { name: /^T-1 / });
    one.focus();
    await fireEvent.keyDown(one, { key: 'ArrowDown' });
    expect(screen.getByRole('button', { name: /^T-2 / })).toHaveFocus();
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
    expect(screen.getByRole('button', { name: /^T-3 / })).toHaveFocus();
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
    expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus();
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / }).getAttribute('tabindex')).toBe('0'));
    expect(screen.getByRole('button', { name: /^T-2 / }).getAttribute('tabindex')).toBe('-1');
  });
});

// ----- The keyboard -----

describe('moving with the keyboard', () => {
  it('Shift+ArrowRight moves the card to the next allowed column and says so', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo')]);
    const { store } = await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    expect(api.move).toHaveBeenCalledWith('Home', 'T-1', 'In progress', 0);
    expect(store.view!.cards.find((c) => c.id === 'T-1')!.column).toBe('In progress');
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('T-1 moved to In progress');
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });

  it('skips a column that is full and goes to the next one that takes the card', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'In progress'), card('T-3', 'In progress')]);
    await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    expect(api.move).toHaveBeenCalledWith('Home', 'T-1', 'Done', 0);
  });

  it('says why when no column that way takes the card, and moves nothing', async () => {
    const api = fakeApi([card('T-1', 'Done')]);
    await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    expect(api.move).not.toHaveBeenCalled();
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('already in the last column');
  });

  it('Shift+ArrowDown moves a card down within its column', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo'), card('C', 'Todo')]);
    await mount(api);
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowDown', shiftKey: true });
    // A goes below B: without A the column is B C, and B is at 0.
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'Todo', 1);
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('A moved to position 2 of 3 in Todo');
  });

  it('does nothing past the top or bottom of a column', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo')]);
    await mount(api);
    const a = screen.getByRole('button', { name: /^A / });
    a.focus();
    await fireEvent.keyDown(a, { key: 'ArrowUp', shiftKey: true });
    expect(api.move).not.toHaveBeenCalled();
  });

  it('does not reorder cards the filter hides: a step moves past the next VISIBLE card', async () => {
    // The whole column is A X B; the filter shows A and B. Moving A down puts it right after B.
    const whole = [card('A', 'Todo'), card('X', 'Todo'), card('B', 'Todo')];
    const api = fakeApi(whole, () => [whole[0]!, whole[2]!]);
    const { store } = await mount(api);
    store.setQuery('hide x');
    await waitFor(() => expect(store.view!.cards).toHaveLength(2));
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowDown', shiftKey: true });
    // Without A the whole column is X B; after B is index 2.
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'Todo', 2);
  });

  it('does nothing when the board is read-only', async () => {
    const api = fakeApi([card('T-1', 'Todo')]);
    const { store } = await mount(api);
    api.board.mockRejectedValue(new ApiError(423, 'parse', 'line 4: bad', { line: 4 }));
    await store.refresh();
    await waitFor(() => expect(store.readonly).toBe(true));
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    await fireEvent.keyDown(el, { key: 'm' });
    expect(api.move).not.toHaveBeenCalled();
    expect(screen.queryByRole('menu')).toBeNull();
    expect(within(zoneOf('Todo')).getByRole('button', { name: /Add card/ })).toBeDisabled();
  });
});

describe('the move menu', () => {
  it('opens on m and lists only the columns the card may go to, then top and bottom', async () => {
    const api = fakeApi([card('T-1', 'In progress'), card('T-2', 'Todo')]);
    const narrow: Column[] = [
      { name: 'Todo', wip: 0, moves_to: ['In progress'], done: false },
      { name: 'In progress', wip: 0, moves_to: ['Todo'], done: false },
      { name: 'Done', wip: 0, moves_to: [], done: true },
    ];
    api.board.mockResolvedValue(response([card('T-1', 'In progress'), card('T-2', 'Todo')], settingsOf({ columns: narrow })));
    await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    const menu = await screen.findByRole('menu', { name: 'Move T-1' });
    expect(within(menu).getAllByRole('menuitem').map((i) => i.textContent?.trim())).toEqual([
      'Move to Todo',
      'Top of column',
      'Bottom of column',
    ]);
    expect(within(menu).getAllByRole('menuitem')[0]).toHaveFocus();
  });

  it('does not list a column at its WIP limit', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'In progress'), card('T-3', 'In progress')]);
    await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    const menu = await screen.findByRole('menu');
    expect(within(menu).queryByRole('menuitem', { name: /In progress/ })).toBeNull();
    expect(within(menu).getByRole('menuitem', { name: /Done/ })).toBeInTheDocument();
  });

  it('moves the card with the chosen column and closes, returning focus', async () => {
    const api = fakeApi([card('T-1', 'Todo')]);
    await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: /Move to Done/ }));
    expect(api.move).toHaveBeenCalledWith('Home', 'T-1', 'Done', 0);
    expect(screen.queryByRole('menu')).toBeNull();
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });

  it('sends a card to the top or the bottom of its own column', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo'), card('C', 'Todo')]);
    await mount(api);
    const b = screen.getByRole('button', { name: /^B / });
    b.focus();
    await fireEvent.keyDown(b, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Top of column' }));
    expect(api.move).toHaveBeenLastCalledWith('Home', 'B', 'Todo', 0);
    const a = screen.getByRole('button', { name: /^A / });
    a.focus();
    await fireEvent.keyDown(a, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Bottom of column' }));
    expect(api.move).toHaveBeenLastCalledWith('Home', 'A', 'Todo', 2);
  });

  it('closes on Escape and gives focus back to the card', async () => {
    await mount(fakeApi([card('T-1', 'Todo')]));
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    const menu = await screen.findByRole('menu');
    await fireEvent.keyDown(menu, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });
});

// ----- The pointer -----

describe('dragging a card', () => {
  it('drops between two cards of another column at that index', async () => {
    const api = fakeApi([card('A', 'Todo'), card('D', 'Done'), card('E', 'Done')]);
    await mount(api);
    layOut(null);
    // Done holds D (10..110) and E (120..220); y=115 is between them.
    await dragTo('A', 2 * 220 + 50, 115);
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'Done', 1);
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('A moved to Done');
  });

  it('drops on the empty column at index 0', async () => {
    const api = fakeApi([card('A', 'Todo')]);
    await mount(api);
    layOut(null);
    await dragTo('A', 2 * 220 + 50, 100);
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'Done', 0);
  });

  it('does not start a drag for a movement under 4px', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo')]);
    await mount(api);
    layOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 42, 42));
    window.dispatchEvent(pointer('pointerup', 42, 42));
    expect(document.querySelector('.drag-clone')).toBeNull();
    expect(api.move).not.toHaveBeenCalled();
  });

  it('dims a column that cannot take the card and shows the reason, and a drop there does nothing', async () => {
    const api = fakeApi([card('A', 'Todo')], undefined, settingsOf({ columns: columns.map((c) => (c.name === 'Todo' ? { ...c, moves_to: ['In progress'] } : c)) }));
    await mount(api);
    layOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 60, 50));
    await waitFor(() => expect(zoneOf('Done')).toHaveClass('refused'));
    expect(zoneOf('Done')).toHaveAttribute('title', 'Todo cannot move to Done');
    // The reason is written in the column once the pointer is over it.
    expect(zoneOf('Done').querySelector('.why')).toBeNull();
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 100));
    await waitFor(() => expect(zoneOf('Done')).toHaveTextContent('Todo cannot move to Done'));
    window.dispatchEvent(pointer('pointerup', 2 * 220 + 50, 100));
    expect(api.move).not.toHaveBeenCalled();
    await waitFor(() => expect(zoneOf('Done')).not.toHaveClass('refused'));
  });

  it('Escape cancels the drag and the card stays', async () => {
    const api = fakeApi([card('A', 'Todo')]);
    await mount(api);
    layOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 100));
    await waitFor(() => expect(el).toHaveClass('ghost'));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    window.dispatchEvent(pointer('pointerup', 2 * 220 + 50, 100));
    expect(api.move).not.toHaveBeenCalled();
    await waitFor(() => expect(el).not.toHaveClass('ghost'));
  });

  it('does not drag when the board is read-only', async () => {
    const api = fakeApi([card('A', 'Todo')]);
    const { store } = await mount(api);
    api.board.mockRejectedValue(new ApiError(423, 'parse', 'line 4: bad', { line: 4 }));
    await store.refresh();
    await waitFor(() => expect(store.readonly).toBe(true));
    layOut(null);
    await dragTo('A', 2 * 220 + 50, 100);
    expect(document.querySelector('.drag-clone')).toBeNull();
    expect(api.move).not.toHaveBeenCalled();
  });

  it('shows the placeholder where the card would land', async () => {
    const api = fakeApi([card('A', 'Todo'), card('D', 'Done'), card('E', 'Done')]);
    await mount(api);
    layOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 60, 50));
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 115));
    await waitFor(() => expect(zoneOf('Done').querySelector('.slot')).not.toBeNull());
    // Halfway between D (bottom 110) and E (top 120), less half the bar.
    expect((zoneOf('Done').querySelector('.slot') as HTMLElement).style.transform).toBe('translateY(113px)');
    window.dispatchEvent(pointer('pointerup', 2 * 220 + 50, 115));
  });
});

// ----- Filtered boards: the index is for the FULL column -----

describe('dropping on a filtered board', () => {
  it('lands right after the upper visible card in the full order, not at the same index among the visible ones', async () => {
    // Todo is A B C D. The filter shows A and D only.
    const whole = [card('A', 'Todo'), card('B', 'Todo'), card('C', 'Todo'), card('D', 'Todo'), card('X', 'In progress')];
    const api = fakeApi(whole, () => [whole[0]!, whole[3]!, whole[4]!]);
    const { store } = await mount(api);
    store.setQuery('p0');
    await waitFor(() => expect(store.view!.cards).toHaveLength(3));
    expect(names(zoneOf('Todo'))).toEqual(['A', 'D']);
    layOut(null);
    // Between A (10..110) and D (120..220) in Todo.
    await dragTo('X', 50, 115);
    expect(api.move).toHaveBeenCalledWith('Home', 'X', 'Todo', 1);
    // The filtered view shows it where the whole board has it: after A.
    expect(store.view!.cards.filter((c) => c.column === 'Todo').map((c) => c.id)).toEqual(['A', 'X', 'D']);
    // And the whole board is A X B C D, not A B X C D or A D X.
    expect(store.baseline!.cards.filter((c) => c.column === 'Todo').map((c) => c.id)).toEqual(['A', 'X', 'B', 'C', 'D']);
  });

  it('shows the card where it landed when hidden cards sit above the visible ones', async () => {
    // Todo is H1 H2 A D; the filter shows A and D. Between them is index 3 of the whole column,
    // which is past the end of the two cards the view has.
    const whole = [card('H1', 'Todo'), card('H2', 'Todo'), card('A', 'Todo'), card('D', 'Todo'), card('X', 'In progress')];
    const api = fakeApi(whole, () => [whole[2]!, whole[3]!, whole[4]!]);
    const { store } = await mount(api);
    store.setQuery('p0');
    await waitFor(() => expect(store.view!.cards).toHaveLength(3));
    layOut(null);
    await dragTo('X', 50, 115);
    expect(api.move).toHaveBeenCalledWith('Home', 'X', 'Todo', 3);
    expect(store.view!.cards.filter((c) => c.column === 'Todo').map((c) => c.id)).toEqual(['A', 'X', 'D']);
    expect(store.baseline!.cards.filter((c) => c.column === 'Todo').map((c) => c.id)).toEqual(['H1', 'H2', 'A', 'X', 'D']);
  });

  it('puts a card dropped below the last visible card right after it, ahead of hidden cards below', async () => {
    const whole = [card('A', 'Todo'), card('B', 'Todo'), card('C', 'Todo'), card('X', 'In progress')];
    const api = fakeApi(whole, () => [whole[0]!, whole[3]!]);
    const { store } = await mount(api);
    store.setQuery('p0');
    await waitFor(() => expect(store.view!.cards).toHaveLength(2));
    layOut(null);
    await dragTo('X', 50, 300);
    expect(api.move).toHaveBeenCalledWith('Home', 'X', 'Todo', 1);
  });

  it('counts the whole board against a WIP limit, not the filtered view', async () => {
    // In progress holds two cards; the filter shows none of them. The column is still full.
    const whole = [card('A', 'Todo'), card('P', 'In progress'), card('Q', 'In progress')];
    const api = fakeApi(whole, () => [whole[0]!]);
    const { store } = await mount(api);
    store.setQuery('a');
    await waitFor(() => expect(store.view!.cards).toHaveLength(1));
    layOut(null);
    await dragTo('A', 220 + 50, 100);
    expect(api.move).not.toHaveBeenCalled();
  });
});

// ----- When the server says no -----

describe('a move the server refuses', () => {
  it('puts the card back where it was and shows the server message', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo')]);
    api.move.mockRejectedValue(new ApiError(422, 'wip_limit', 'In progress is at its WIP limit (2/2)'));
    const { store } = await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    expect(api.move).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('In progress is at its WIP limit (2/2)'));
    expect(store.view!.cards.map((c) => `${c.id}:${c.column}`)).toEqual(['T-1:Todo', 'T-2:Todo']);
    await waitFor(() => expect(names(zoneOf('Todo'))).toEqual(['T-1', 'T-2']));
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });
});

// ----- Swimlanes -----

describe('swimlanes', () => {
  const cards = () => [
    card('A', 'Todo', { group: 'Security' }),
    card('B', 'Done', { group: 'Security' }),
    card('C', 'Todo', { group: 'Purchasing' }),
    card('D', 'Todo'),
  ];

  it('shows a lane for each group and one for no group, each across the same columns', async () => {
    await mount(fakeApi(cards()), 'group');
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(3);
    const lanes = screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent?.replace(/\s+/g, ' ').trim());
    expect(lanes).toEqual(['Security 1 of 2 done', 'Purchasing 0 of 1 done', 'No group 0 of 1 done']);
    expect(names(zoneOf('Todo', 'Security'))).toEqual(['A']);
    expect(names(zoneOf('Todo', ''))).toEqual(['D']);
  });

  it('collapses a lane with its caret and remembers it for the project', async () => {
    const { unmount } = await mount(fakeApi(cards()), 'group');
    await fireEvent.click(screen.getByRole('button', { name: /Security/ }));
    expect(screen.getByRole('button', { name: /Security/ })).toHaveAttribute('aria-expanded', 'false');
    expect(document.querySelector('[data-lane="Security"]')).toBeNull();
    expect(JSON.parse(localStorage.getItem('waypoint.lanes.Home')!)).toEqual({ group: ['Security'] });
    unmount();
    store?.destroy();
    await mount(fakeApi(cards()), 'group');
    expect(screen.getByRole('button', { name: /Security/ })).toHaveAttribute('aria-expanded', 'false');
  });

  it('swimlanes by priority, with a lane for no priority', async () => {
    await mount(fakeApi([card('A', 'Todo', { priority: 'P0' }), card('B', 'Todo')]), 'priority');
    expect([...document.querySelectorAll('.lname')].map((n) => n.textContent)).toEqual(['P0', 'P1', 'No priority']);
  });

  it('changes the group, then moves, when a card is dropped into another lane', async () => {
    const api = fakeApi(cards());
    await mount(api, 'group');
    layOut(['Security', 'Purchasing', '']);
    // A, from Security/Todo, into Purchasing/In progress (empty): lane row 1, column 1.
    await dragTo('A', 220 + 50, 400 + 100);
    await waitFor(() => expect(api.move).toHaveBeenCalledTimes(1));
    expect(api.patch).toHaveBeenCalledWith('Home', 'A', { group: 'Purchasing' }, undefined);
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'In progress', 0);
    expect(api.patch.mock.invocationCallOrder[0]!).toBeLessThan(api.move.mock.invocationCallOrder[0]!);
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('A moved to In progress, Purchasing');
  });

  it('clears the group when a card is dropped into the No group lane', async () => {
    const api = fakeApi(cards());
    await mount(api, 'group');
    layOut(['Security', 'Purchasing', '']);
    await dragTo('A', 50, 800 + 200);
    await waitFor(() => expect(api.patch).toHaveBeenCalled());
    expect(api.patch).toHaveBeenCalledWith('Home', 'A', { group: '' }, undefined);
  });

  it('changes the priority in priority lanes', async () => {
    const api = fakeApi([card('A', 'Todo', { priority: 'P0' })]);
    await mount(api, 'priority');
    layOut(['P0', 'P1', '']);
    await dragTo('A', 220 + 50, 400 + 100);
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('Home', 'A', { priority: 'P1' }, undefined));
    await waitFor(() => expect(api.move).toHaveBeenCalled());
  });

  it('does not move the card when the group change fails, and shows why', async () => {
    const api = fakeApi(cards());
    api.patch.mockRejectedValue(new ApiError(422, 'unknown_group', 'No group called Purchasing'));
    const { store } = await mount(api, 'group');
    layOut(['Security', 'Purchasing', '']);
    await dragTo('A', 220 + 50, 400 + 100);
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('No group called Purchasing'));
    expect(api.move).not.toHaveBeenCalled();
    expect(store.view!.cards.find((c) => c.id === 'A')!.column).toBe('Todo');
  });

  it('only changes the group when the card lands where it already was', async () => {
    const api = fakeApi([card('A', 'Todo', { group: 'Security' }), card('D', 'Todo')]);
    await mount(api, 'group');
    layOut(['Security', 'Purchasing', '']);
    // A into No group / Todo, above D: Todo stays A D, so only the patch runs.
    await dragTo('A', 50, 800 + 5);
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('Home', 'A', { group: '' }, undefined));
    expect(api.move).not.toHaveBeenCalled();
  });

  it('keeps a keyboard move in its own lane', async () => {
    const api = fakeApi(cards());
    await mount(api, 'group');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'ArrowRight', shiftKey: true });
    expect(api.patch).not.toHaveBeenCalled();
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'In progress', 0);
  });
});

// ----- Lanes by keyboard and menu -----

describe('moving between lanes without a pointer', () => {
  const cards = () => [card('A', 'Todo', { group: 'Security' }), card('C', 'Todo', { group: 'Purchasing' }), card('D', 'Todo')];

  it('lists the other lanes in the move menu, with No group last, and not the card\'s own', async () => {
    await mount(fakeApi(cards()), 'group');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    const menu = await screen.findByRole('menu');
    const items = within(menu).getAllByRole('menuitem').map((i) => i.textContent?.trim());
    expect(items).toContain('Move to lane: Purchasing');
    expect(items).toContain('Move to lane: No group');
    expect(items).not.toContain('Move to lane: Security');
    expect(items.indexOf('Move to lane: Purchasing')).toBeLessThan(items.indexOf('Move to lane: No group'));
  });

  it('changes the group with the same patch a cross-lane drop sends, and does not move a card that stays put', async () => {
    const api = fakeApi(cards());
    await mount(api, 'group');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Move to lane: Purchasing' }));
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('Home', 'A', { group: 'Purchasing' }, undefined));
    expect(api.move).not.toHaveBeenCalled();
    expect(screen.getByRole('log', { name: 'Moves' })).toHaveTextContent('A moved to Purchasing');
    await waitFor(() => expect(screen.getByRole('button', { name: /^A / })).toHaveFocus());
  });

  it('clears the group for No group, and changes priority in priority lanes', async () => {
    const api = fakeApi(cards());
    await mount(api, 'group');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Move to lane: No group' }));
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('Home', 'A', { group: '' }, undefined));

    cleanup();
    store?.destroy();
    const api2 = fakeApi([card('A', 'Todo', { priority: 'P0' })]);
    await mount(api2, 'priority');
    const b = screen.getByRole('button', { name: /^A / });
    b.focus();
    await fireEvent.keyDown(b, { key: 'm' });
    const menu = await screen.findByRole('menu');
    expect(within(menu).getByRole('menuitem', { name: 'Move to lane: No priority' })).toBeInTheDocument();
    await fireEvent.click(within(menu).getByRole('menuitem', { name: 'Move to lane: P1' }));
    await waitFor(() => expect(api2.patch).toHaveBeenCalledWith('Home', 'A', { priority: 'P1' }, undefined));
  });

  it('shows the reason and does nothing more when the group change fails', async () => {
    const api = fakeApi(cards());
    api.patch.mockRejectedValue(new ApiError(422, 'unknown_group', 'No group called Purchasing'));
    await mount(api, 'group');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Move to lane: Purchasing' }));
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('No group called Purchasing'));
    expect(api.move).not.toHaveBeenCalled();
  });

  it('has no lane items outside swimlanes', async () => {
    await mount(fakeApi(cards()), 'none');
    const el = screen.getByRole('button', { name: /^A / });
    el.focus();
    await fireEvent.keyDown(el, { key: 'm' });
    const menu = await screen.findByRole('menu');
    expect(within(menu).queryByRole('menuitem', { name: /lane/ })).toBeNull();
  });
});

// ----- Focus when the board changes under it -----

describe('focus across a refresh', () => {
  it('stays on a card that another client moved to a new column, though its element is rebuilt', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo')]);
    const { store } = await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    api.board.mockResolvedValue(response([card('T-1', 'Done'), card('T-2', 'Todo')]));
    await store.refresh();
    await waitFor(() => expect(names(zoneOf('Done'))).toEqual(['T-1']));
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });

  it('goes to the nearest neighbour when the focused card is gone', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo'), card('T-3', 'Todo')]);
    const { store } = await mount(api);
    screen.getByRole('button', { name: /^T-2 / }).focus();
    api.board.mockResolvedValue(response([card('T-1', 'Todo'), card('T-3', 'Todo')]));
    await store.refresh();
    await waitFor(() => expect(screen.getByRole('button', { name: /^T-1 / })).toHaveFocus());
  });

  it('does not take focus back from someone who left the board on purpose', async () => {
    const api = fakeApi([card('T-1', 'Todo'), card('T-2', 'Todo')]);
    const { store } = await mount(api);
    const el = screen.getByRole('button', { name: /^T-1 / });
    el.focus();
    el.blur();
    await new Promise((r) => setTimeout(r, 0));
    api.board.mockResolvedValue(response([card('T-1', 'Done'), card('T-2', 'Todo')]));
    await store.refresh();
    await waitFor(() => expect(names(zoneOf('Done'))).toEqual(['T-1']));
    expect(document.body).toHaveFocus();
  });

  it('leaves focus alone when it is in the filter box', async () => {
    const api = fakeApi([card('T-1', 'Todo')]);
    const { store } = await mount(api);
    const input = document.createElement('input');
    document.body.append(input);
    screen.getByRole('button', { name: /^T-1 / }).focus();
    input.focus();
    api.board.mockResolvedValue(response([card('T-1', 'Done')]));
    await store.refresh();
    await waitFor(() => expect(names(zoneOf('Done'))).toEqual(['T-1']));
    expect(input).toHaveFocus();
    input.remove();
  });
});

// ----- A drag while the board refreshes -----

describe('a refresh during a drag', () => {
  it('measures again, so the drop lands by the new layout', async () => {
    const api = fakeApi([card('A', 'Todo'), card('D', 'Done'), card('E', 'Done')]);
    const { store } = await mount(api);
    autoLayOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 60, 50));
    // Between D (10..110) and E (120..220) of Done: after D.
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 115));
    // Meanwhile another client adds N at the top of Done: it is now N D E, and y=115 is between N and D.
    api.board.mockResolvedValue(response([card('A', 'Todo'), card('N', 'Done'), card('D', 'Done'), card('E', 'Done')]));
    await store.refresh();
    await waitFor(() => expect(names(zoneOf('Done'))).toEqual(['N', 'D', 'E']));
    await new Promise((r) => setTimeout(r, 0)); // the board has had its turn after the refresh
    window.dispatchEvent(pointer('pointerup', 2 * 220 + 50, 115));
    await waitFor(() => expect(api.move).toHaveBeenCalledWith('Home', 'A', 'Done', 1));
  });

  it('cancels and says so when the dragged card was removed elsewhere', async () => {
    const api = fakeApi([card('A', 'Todo'), card('B', 'Todo'), card('D', 'Done')]);
    const { store } = await mount(api);
    autoLayOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 60, 50));
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 300));
    expect(document.querySelector('.drag-clone')).not.toBeNull();
    api.board.mockResolvedValue(response([card('B', 'Todo'), card('D', 'Done')]));
    await store.refresh();
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('A was removed'));
    expect(document.querySelector('.drag-clone')).toBeNull();
    window.dispatchEvent(pointer('pointerup', 2 * 220 + 50, 300));
    expect(api.move).not.toHaveBeenCalled();
    expect(document.body.classList.contains('wp-dragging')).toBe(false);
  });

  it('does not cancel when a filter, not a removal, hides the dragged card', async () => {
    const whole = [card('A', 'Todo'), card('B', 'Todo'), card('D', 'Done')];
    const api = fakeApi(whole, () => [whole[1]!, whole[2]!]);
    const { store } = await mount(api);
    autoLayOut(null);
    const el = screen.getByRole('button', { name: /^A / });
    el.dispatchEvent(pointer('pointerdown', 40, 40));
    window.dispatchEvent(pointer('pointermove', 60, 50));
    window.dispatchEvent(pointer('pointermove', 2 * 220 + 50, 300));
    store.setQuery('not a');
    await waitFor(() => expect(store.view!.cards).toHaveLength(2));
    expect(toasts.items.map((t) => t.msg)).not.toContain('A was removed');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
  });
});
