import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../src/lib/api';
import { createBoardStore, type BoardApi } from '../src/lib/board.svelte';
import { toasts } from '../src/lib/events.svelte';
import type { BoardResponse, CardView, Settings } from '../src/lib/types';

const settings = {
  name: 'Home',
  prefix: 'H',
  next_id: 10,
  columns: [],
  groups: [],
  priorities: [],
  archive: { mode: 'manual', after_days: 0 },
  appearance: { theme: '', accent: '', density: '', swimlane: '', textures: '' },
} as Settings;

function card(id: string, column: string, body = ''): CardView {
  return {
    id,
    title: `Card ${id}`,
    column,
    group: '',
    priority: '',
    depends: [],
    created: null,
    done: null,
    body,
    extra: [],
    archived: false,
    blockers: [],
    next: false,
    checklist: { done: (body.match(/\[x\]/g) ?? []).length, total: (body.match(/\[[ x]\]/g) ?? []).length },
    hash: `hash-${id}`,
  };
}

function response(cards: CardView[], hash = 'sh-1'): BoardResponse {
  return {
    board: { settings, cards, warnings: [], settings_hash: hash },
    appearance: { theme: 'system', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
    readonly: false,
  };
}

interface Deferred<T> {
  promise: Promise<T>;
  resolve(v: T): void;
  reject(e: unknown): void;
}
function deferred<T>(): Deferred<T> {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function fakeApi() {
  return {
    board: vi.fn<BoardApi['board']>(),
    createCard: vi.fn<BoardApi['createCard']>(),
    patch: vi.fn<BoardApi['patch']>(),
    move: vi.fn<BoardApi['move']>(),
    check: vi.fn<BoardApi['check']>(),
    deleteCard: vi.fn<BoardApi['deleteCard']>(),
    archiveDone: vi.fn<BoardApi['archiveDone']>(),
    restore: vi.fn<BoardApi['restore']>(),
    putProjectSettings: vi.fn<BoardApi['putProjectSettings']>(),
  };
}

const settle = () => vi.advanceTimersByTimeAsync(0);
const order = (s: { view: { cards: CardView[] } | null }, column?: string) =>
  s.view!.cards.filter((c) => column === undefined || c.column === column).map((c) => `${c.id}`);

const start = [card('A', 'todo'), card('B', 'todo'), card('C', 'todo'), card('D', 'doing')];

async function loaded() {
  const api = fakeApi();
  api.board.mockResolvedValue(response(start));
  const store = createBoardStore(api, 'Home');
  await store.refresh();
  api.board.mockClear();
  return { api, store };
}

beforeEach(() => {
  vi.useFakeTimers();
  toasts.clear();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('loading', () => {
  it('shows the board, its appearance and its settings hash', async () => {
    const { store } = await loaded();
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
    expect(store.appearance?.accent).toBe('orange');
    expect(store.settingsHash).toBe('sh-1');
    expect(store.readonly).toBe(false);
  });

  it('asks for the project and the query', async () => {
    const api = fakeApi();
    api.board.mockResolvedValue(response(start));
    const store = createBoardStore(api, 'Home reno');
    await store.refresh();
    expect(api.board).toHaveBeenCalledWith('Home reno', undefined);
  });
});

describe('move', () => {
  it('reorders the cards before the server answers', async () => {
    const { api, store } = await loaded();
    api.move.mockReturnValue(deferred<CardView>().promise);
    void store.move('C', 'todo', 0);
    expect(order(store, 'todo')).toEqual(['C', 'A', 'B']);
    void store.move('A', 'doing', 0);
    expect(store.view!.cards.find((c) => c.id === 'A')!.column).toBe('doing');
    expect(order(store, 'doing')).toEqual(['A', 'D']);
  });

  it('sends no index for a move to the bottom', async () => {
    const { api, store } = await loaded();
    api.move.mockResolvedValue(card('A', 'doing'));
    void store.move('A', 'doing', -1);
    expect(order(store, 'doing')).toEqual(['D', 'A']);
    await settle();
    expect(api.move).toHaveBeenCalledWith('Home', 'A', 'doing', undefined);
  });

  it('puts the exact order back and toasts the server message when the move fails', async () => {
    const { api, store } = await loaded();
    const d = deferred<CardView>();
    api.move.mockReturnValue(d.promise);
    api.board.mockResolvedValue(response(start));
    const p = store.move('A', 'doing', 1);
    expect(order(store, 'doing')).toEqual(['D', 'A']);
    d.reject(new ApiError(422, 'wip_limit', 'doing is at its WIP limit (1/1)'));
    await p;
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
    expect(store.view!.cards.find((c) => c.id === 'A')!.column).toBe('todo');
    expect(toasts.items.map((t) => t.msg)).toEqual(['doing is at its WIP limit (1/1)']);
  });

  it('refreshes after a failure so the server wins', async () => {
    const { api, store } = await loaded();
    api.move.mockRejectedValue(new ApiError(404, 'not_found', 'no card A'));
    api.board.mockResolvedValue(response([card('B', 'todo'), card('C', 'todo'), card('D', 'doing')]));
    await store.move('A', 'doing', 0);
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
    expect(order(store)).toEqual(['B', 'C', 'D']);
  });

  it('rolls a card back to its place in the whole column on a filtered board, not the view\'s', async () => {
    // The whole todo column is A X B C. The filter hides X.
    const whole = [card('A', 'todo'), card('X', 'todo'), card('B', 'todo'), card('C', 'todo'), card('D', 'doing')];
    const api = fakeApi();
    api.board.mockImplementation(async (_p, q) => response(q ? whole.filter((c) => c.id !== 'X') : whole));
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    store.setQuery('hide x');
    await vi.advanceTimersByTimeAsync(200);
    expect(order(store, 'todo')).toEqual(['A', 'B', 'C']);

    api.move.mockRejectedValue(new ApiError(422, 'wip_limit', 'doing is at its WIP limit (1/1)'));
    api.board.mockReturnValue(deferred<BoardResponse>().promise); // the refresh has not landed yet
    await store.move('A', 'doing', 0);
    const ids = (b: { cards: CardView[] } | null, column: string) => b!.cards.filter((c) => c.column === column).map((c) => c.id);
    expect(ids(store.view, 'todo')).toEqual(['A', 'B', 'C']);
    // The whole board has A back in front of X, not after it.
    expect(ids(store.baseline, 'todo')).toEqual(['A', 'X', 'B', 'C']);
  });

  it('sends writes one at a time, in order', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise).mockResolvedValue(card('B', 'todo'));
    void store.move('A', 'todo', 2);
    void store.move('B', 'todo', 0);
    await settle();
    expect(api.move).toHaveBeenCalledTimes(1);
    first.resolve(card('A', 'todo'));
    await settle();
    expect(api.move).toHaveBeenCalledTimes(2);
    expect(api.move.mock.calls.map((c) => c[1])).toEqual(['A', 'B']);
  });

  it('rolls back only the first of two moves when the first fails', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise).mockResolvedValueOnce(card('D', 'todo'));
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const p1 = store.move('A', 'doing', 0); // A goes to doing
    const p2 = store.move('D', 'todo', 0); // D goes to the top of todo
    expect(order(store)).toEqual(['D', 'B', 'C', 'A']);
    first.reject(new ApiError(422, 'move_not_allowed', 'todo cannot move to doing'));
    await Promise.all([p1, p2]);
    expect(order(store, 'todo')).toEqual(['D', 'A', 'B', 'C']);
    expect(store.view!.cards.find((c) => c.id === 'A')!.column).toBe('todo');
    expect(store.view!.cards.find((c) => c.id === 'D')!.column).toBe('todo');
  });

  it('puts a card back beside the neighbour it still has when the other one has gone', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise).mockResolvedValue(card('A', 'doing'));
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const p1 = store.move('B', 'doing', 0); // B leaves todo from between A and C
    const p2 = store.move('A', 'doing', 0); // and A leaves too
    expect(order(store, 'todo')).toEqual(['C']);
    first.reject(new ApiError(422, 'wip_limit', 'full'));
    await Promise.all([p1, p2]);
    expect(order(store, 'todo')).toEqual(['B', 'C']); // before C, its old successor
    expect(order(store, 'doing')).toEqual(['A', 'D']);
  });

  it('keeps a tick on another card when a move fails', async () => {
    const { api, store } = await loaded();
    const body = '- [ ] one\n- [ ] two';
    api.board.mockResolvedValue(response([card('A', 'todo'), card('B', 'todo', body), card('D', 'doing')]));
    await store.refresh();
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const first = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise);
    api.check.mockResolvedValue(card('B', 'todo', '- [x] one\n- [ ] two'));
    const p1 = store.move('A', 'doing', 0);
    const p2 = store.check('B', 1);
    expect(store.view!.cards.find((c) => c.id === 'B')!.checklist.done).toBe(1);
    first.reject(new ApiError(422, 'wip_limit', 'full'));
    await Promise.all([p1, p2]);
    expect(store.view!.cards.find((c) => c.id === 'B')!.checklist.done).toBe(1);
    expect(order(store)).toEqual(['A', 'B', 'D']);
  });

  it('inherits the starting place when the same card is moved twice and the first fails', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    const second = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const p1 = store.move('A', 'doing', 0);
    const p2 = store.move('A', 'todo', 2);
    first.reject(new ApiError(422, 'wip_limit', 'full'));
    await settle();
    // the second move is still showing
    expect(order(store, 'todo')).toEqual(['B', 'C', 'A']);
    second.reject(new ApiError(422, 'wip_limit', 'full'));
    await Promise.all([p1, p2]);
    expect(order(store, 'todo')).toEqual(['A', 'B', 'C']);
  });

  it('keeps the final place when the same card is moved twice and the first fails but the second works', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    api.move.mockReturnValueOnce(first.promise).mockResolvedValueOnce(card('A', 'todo'));
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const p1 = store.move('A', 'doing', 0);
    const p2 = store.move('A', 'todo', 2);
    first.reject(new ApiError(422, 'wip_limit', 'full'));
    await Promise.all([p1, p2]);
    expect(order(store, 'todo')).toEqual(['B', 'C', 'A']);
  });

  it('takes the server card (a new hash) once nothing else is waiting on it', async () => {
    const { api, store } = await loaded();
    api.move.mockResolvedValue({ ...card('A', 'doing'), hash: 'fresh' });
    await store.move('A', 'doing', 0);
    expect(store.view!.cards.find((c) => c.id === 'A')!.hash).toBe('fresh');
  });
});

describe('check', () => {
  const body = '- [x] one\n- [ ] two\n```\n- [ ] not an item\n```\n- [ ] three';

  async function withChecklist() {
    const api = fakeApi();
    api.board.mockResolvedValue(response([{ ...card('A', 'todo', body), checklist: { done: 1, total: 3 } }]));
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    api.board.mockClear();
    return { api, store };
  }

  it('changes the counts at once and sends the item number', async () => {
    const { api, store } = await withChecklist();
    api.check.mockReturnValue(deferred<CardView>().promise);
    void store.check('A', 3);
    expect(store.view!.cards[0]!.checklist).toEqual({ done: 2, total: 3 });
    await settle();
    expect(api.check).toHaveBeenCalledWith('Home', 'A', 3, 'hash-A');
  });

  it('unticks a ticked item', async () => {
    const { api, store } = await withChecklist();
    api.check.mockReturnValue(deferred<CardView>().promise);
    void store.check('A', 1);
    expect(store.view!.cards[0]!.checklist.done).toBe(0);
  });

  it('undoes only its own tick when it fails', async () => {
    const { api, store } = await withChecklist();
    const first = deferred<CardView>();
    const second = deferred<CardView>();
    api.check.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    api.board.mockReturnValue(deferred<BoardResponse>().promise);
    const p1 = store.check('A', 2);
    const p2 = store.check('A', 3);
    expect(store.view!.cards[0]!.checklist.done).toBe(3);
    first.reject(new ApiError(500, 'error', 'could not save'));
    await settle(); // the second check is now the one on the wire
    const a = store.view!.cards[0]!;
    expect(a.checklist.done).toBe(2);
    expect(a.body).toBe('- [x] one\n- [ ] two\n```\n- [ ] not an item\n```\n- [x] three');
    second.reject(new ApiError(500, 'error', 'could not save either'));
    await Promise.all([p1, p2]);
    expect(store.view!.cards[0]!.checklist.done).toBe(1);
    expect(toasts.items.map((t) => t.msg)).toEqual(['could not save', 'could not save either']);
  });
});

describe('stale cards', () => {
  const conflict = (current: CardView) => new ApiError(409, 'conflict', 'conflict: card A changed since it was read', { current });

  it('sends the hash of the card with a delete', async () => {
    const { api, store } = await loaded();
    api.deleteCard.mockResolvedValue(undefined);
    await store.remove('A');
    expect(api.deleteCard).toHaveBeenCalledWith('Home', 'A', 'hash-A');
  });

  it('sends the hash the first tick got back with the second, which was queued behind it', async () => {
    const { api, store } = await loaded();
    api.board.mockResolvedValue(response([card('A', 'todo', '- [ ] one\n- [ ] two')]));
    await store.refresh();
    const first = deferred<CardView>();
    api.check.mockReturnValueOnce(first.promise).mockResolvedValueOnce({ ...card('A', 'todo'), hash: 'after-two' });

    const p1 = store.check('A', 1);
    const p2 = store.check('A', 2);
    await settle();
    first.resolve({ ...card('A', 'todo', '- [x] one\n- [ ] two'), hash: 'after-one' });
    await Promise.all([p1, p2]);

    expect(api.check.mock.calls.map((c) => c[3])).toEqual(['hash-A', 'after-one']);
  });

  it('sends the hash a queued move got back', async () => {
    const { api, store } = await loaded();
    api.move.mockResolvedValue({ ...card('A', 'doing'), hash: 'after-move' });
    api.deleteCard.mockResolvedValue(undefined);
    const moved = store.move('A', 'doing', 0);
    const removed = store.remove('A');
    await Promise.all([moved, removed]);
    expect(api.deleteCard).toHaveBeenCalledWith('Home', 'A', 'after-move');
  });

  it('forgets an answer it could not show once a board has landed', async () => {
    const { api, store } = await loaded();
    api.board.mockResolvedValue(response([card('A', 'todo', '- [ ] one\n- [ ] two')]));
    await store.refresh();
    api.check
      .mockResolvedValueOnce({ ...card('A', 'todo'), hash: 'after-one' })
      .mockRejectedValueOnce(new ApiError(500, 'error', 'internal error'));
    api.board.mockResolvedValue(response([{ ...card('A', 'todo', '- [ ] one\n- [ ] two'), hash: 'from-board' }]));
    await Promise.all([store.check('A', 1), store.check('A', 2)]);
    await settle();
    api.check.mockResolvedValueOnce(card('A', 'todo'));
    await store.check('A', 1);
    expect(api.check.mock.calls[2]![3]).toBe('from-board');
  });

  it('undoes a refused tick, says so, and refreshes', async () => {
    const { api, store } = await loaded();
    api.board.mockResolvedValue(response([card('A', 'todo', '- [ ] one')]));
    await store.refresh();
    api.board.mockClear();
    const theirs = { ...card('A', 'todo', '- [ ] new\n- [ ] one'), hash: 'theirs' };
    api.check.mockRejectedValue(conflict(theirs));
    api.board.mockResolvedValue(response([theirs]));

    await store.check('A', 1);
    await settle();

    expect(api.board).toHaveBeenCalledTimes(1);
    expect(store.view!.cards[0]!.body).toBe('- [ ] new\n- [ ] one');
    expect(store.view!.cards[0]!.checklist.done).toBe(0);
    expect(toasts.items.map((t) => t.msg)).toEqual([expect.stringContaining('was not saved')]);
  });

  it('keeps a card whose delete was refused, and refreshes', async () => {
    const { api, store } = await loaded();
    const theirs = { ...card('A', 'todo', 'edited'), hash: 'theirs' };
    api.deleteCard.mockRejectedValue(conflict(theirs));
    api.board.mockResolvedValue(response([theirs]));

    await expect(store.remove('A')).rejects.toMatchObject({ status: 409 });
    await settle();

    expect(api.board).toHaveBeenCalledTimes(1);
    expect(store.view!.cards[0]!.body).toBe('edited');
  });
});

describe('a refresh that fails', () => {
  const down = () => new ApiError(0, 'network', 'Failed to fetch');
  const refused = () => new ApiError(422, 'wip_limit', 'Doing is full');

  it('ends equal to the server after a failed write and a failed refresh, with no later event', async () => {
    const { api, store } = await loaded();
    const server = [card('B', 'todo'), card('A', 'doing'), card('D', 'done')];
    api.move.mockRejectedValue(refused());
    api.board.mockRejectedValueOnce(down()).mockResolvedValue(response(server));

    await store.move('A', 'doing', 0);
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
    expect(store.offline).toBe(true);

    await vi.advanceTimersByTimeAsync(500);
    expect(api.board).toHaveBeenCalledTimes(2);
    expect(store.view!.cards).toEqual(server);
    expect(store.offline).toBe(false);

    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.board).toHaveBeenCalledTimes(2); // a landed board ends the retries
  });

  it('retries a refresh an event asked for, too', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValueOnce(down()).mockResolvedValue(response([card('Z', 'todo')]));
    store.handleEvent({ type: 'changed', project: 'Home' });
    await settle();
    await vi.advanceTimersByTimeAsync(500);
    expect(order(store)).toEqual(['Z']);
  });

  it('backs off 0.5s, 1s, 2s and so on, up to 10s', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(down());
    await store.refresh();
    const gaps: number[] = [];
    let last = 0;
    for (let t = 1; t <= 45_000; t += 100) {
      await vi.advanceTimersByTimeAsync(100);
      const n = api.board.mock.calls.length;
      if (n > gaps.length + 1) {
        gaps.push(t + 99 - last);
        last = t + 99;
      }
    }
    expect(gaps.slice(0, 7)).toEqual([500, 1000, 2000, 4000, 8000, 10000, 10000]);
  });

  it('starts the backoff again after a board lands', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValueOnce(down()).mockRejectedValueOnce(down()).mockResolvedValue(response(start));
    await store.refresh();
    await vi.advanceTimersByTimeAsync(1500);
    expect(api.board).toHaveBeenCalledTimes(3);

    api.board.mockClear();
    api.board.mockRejectedValueOnce(down()).mockResolvedValue(response(start));
    await store.refresh();
    await vi.advanceTimersByTimeAsync(500);
    expect(api.board).toHaveBeenCalledTimes(2);
  });

  it('stays offline and silent through network failures', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(down());
    await store.refresh();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(api.board.mock.calls.length).toBeGreaterThan(3);
    expect(store.offline).toBe(true);
    expect(toasts.items).toEqual([]);
  });

  it('toasts a server failure once, not on every retry', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(new ApiError(500, 'error', 'internal error'));
    await store.refresh();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(api.board.mock.calls.length).toBeGreaterThan(3);
    expect(toasts.items.map((t) => t.msg)).toEqual(['internal error']);
  });

  it('waits for queued writes before it retries', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValueOnce(down()).mockResolvedValue(response(start));
    await store.refresh();
    api.board.mockClear();
    const held = deferred<CardView>();
    api.move.mockReturnValue(held.promise);
    const moved = store.move('A', 'doing', 0);

    await vi.advanceTimersByTimeAsync(1000);
    expect(api.board).not.toHaveBeenCalled();

    held.resolve(card('A', 'doing'));
    await moved;
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
  });

  it('stops retrying on destroy', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(down());
    await store.refresh();
    api.board.mockClear();
    store.destroy();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.board).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
});

describe('refreshing', () => {
  it('shares one more refresh among all the events that arrive while one is in flight', async () => {
    const { api, store } = await loaded();
    const first = deferred<BoardResponse>();
    api.board.mockReturnValueOnce(first.promise).mockResolvedValue(response([card('Z', 'todo')]));
    void store.refresh();
    for (let i = 0; i < 5; i++) store.handleEvent({ type: 'changed', project: 'Home' });
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
    first.resolve(response(start));
    await settle();
    expect(api.board).toHaveBeenCalledTimes(2);
    expect(order(store)).toEqual(['Z']);
  });

  it('ignores events for another project and other event types', async () => {
    const { api, store } = await loaded();
    store.handleEvent({ type: 'changed', project: 'Work' });
    store.handleEvent({ type: 'projects', project: 'Home' });
    await settle();
    expect(api.board).not.toHaveBeenCalled();
    store.handleEvent({ type: 'changed', project: '*' });
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
  });

  it('waits for queued writes before refreshing, so the optimistic state does not flicker back', async () => {
    const { api, store } = await loaded();
    const write = deferred<CardView>();
    api.move.mockReturnValue(write.promise);
    api.board.mockResolvedValue(response(start));
    void store.move('C', 'todo', 0);
    store.handleEvent({ type: 'changed', project: 'Home' });
    await settle();
    expect(api.board).not.toHaveBeenCalled();
    expect(order(store, 'todo')).toEqual(['C', 'A', 'B']);
    write.resolve(card('C', 'todo'));
    await settle();
    expect(api.board).toHaveBeenCalledTimes(1);
  });

  it('drops a board that was fetched before a write began and fetches again once the write is done', async () => {
    const { api, store } = await loaded();
    const stale = deferred<BoardResponse>();
    api.board.mockReturnValueOnce(stale.promise).mockResolvedValue(response([card('C', 'todo'), card('A', 'todo'), card('B', 'todo'), card('D', 'doing')]));
    const refreshed = store.refresh();
    const write = deferred<CardView>();
    api.move.mockReturnValue(write.promise);
    void store.move('C', 'todo', 0);
    stale.resolve(response(start)); // the old order, fetched before the move
    await settle();
    expect(order(store, 'todo')).toEqual(['C', 'A', 'B']);
    write.resolve(card('C', 'todo'));
    await refreshed;
    expect(api.board).toHaveBeenCalledTimes(2);
    expect(order(store, 'todo')).toEqual(['C', 'A', 'B']);
  });

  it('drops the answer to a previous query', async () => {
    const { api, store } = await loaded();
    const forA = deferred<BoardResponse>();
    const forB = deferred<BoardResponse>();
    api.board.mockImplementation(async (_p, q) => (q === 'a' ? forA.promise : q === 'ab' ? forB.promise : response(start)));
    store.setQuery('a');
    await vi.advanceTimersByTimeAsync(120);
    store.setQuery('ab');
    await vi.advanceTimersByTimeAsync(120);
    expect(api.board.mock.calls.map((c) => c[1]).filter((q) => q !== undefined)).toEqual(['a', 'ab']);
    forB.resolve(response([card('B', 'todo')]));
    await settle();
    forA.resolve(response([card('A', 'todo')]));
    await settle();
    expect(order(store)).toEqual(['B']);
  });

  it('drops an answer for a query that changed while it was on its way', async () => {
    const { api, store } = await loaded();
    const forA = deferred<BoardResponse>();
    api.board.mockReturnValueOnce(forA.promise);
    store.setQuery('a');
    await vi.advanceTimersByTimeAsync(120);
    store.setQuery('b'); // not yet fetched
    forA.resolve(response([card('A', 'todo')]));
    await settle();
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
  });
});

describe('query', () => {
  it('waits 120ms after the last keystroke, then fetches once', async () => {
    const { api, store } = await loaded();
    api.board.mockResolvedValue(response([card('A', 'todo')]));
    store.setQuery('k');
    store.setQuery('ki');
    store.setQuery('kit');
    expect(store.query).toBe('kit');
    await vi.advanceTimersByTimeAsync(119);
    expect(api.board).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    // The filtered board once, and the whole board beside it for the counts.
    expect(api.board.mock.calls.filter((c) => c[1] === 'kit')).toHaveLength(1);
    expect(api.board).toHaveBeenCalledTimes(2);
    expect(api.board).toHaveBeenCalledWith('Home', 'kit');
  });
});

describe('the unfiltered baseline', () => {
  const all = [card('A', 'todo'), card('B', 'todo'), card('C', 'done'), card('D', 'doing')];
  const narrowed = [card('A', 'todo')];

  /** A server that filters: with a query it answers `narrowed`, without one `all`. */
  function filtering() {
    const api = fakeApi();
    api.board.mockImplementation(async (_p, q) => response(q ? narrowed : all));
    return api;
  }

  it('is the board itself while no query is set', async () => {
    const api = filtering();
    const store = createBoardStore(api, 'Home');
    expect(store.baseline).toBeNull();
    await store.refresh();
    expect(store.baseline?.cards.map((c) => c.id)).toEqual(['A', 'B', 'C', 'D']);
    expect(api.board).toHaveBeenCalledTimes(1);
  });

  it('is fetched beside the filtered board when a query is set, and the view stays filtered', async () => {
    const api = filtering();
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    api.board.mockClear();
    store.setQuery('group:"X"');
    await vi.advanceTimersByTimeAsync(120);
    expect(api.board).toHaveBeenCalledWith('Home', 'group:"X"');
    expect(api.board).toHaveBeenCalledWith('Home', undefined);
    expect(order(store)).toEqual(['A']);
    expect(store.baseline?.cards.map((c) => c.id)).toEqual(['A', 'B', 'C', 'D']);
  });

  it('follows a refresh made while the query is on', async () => {
    const api = filtering();
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    store.setQuery('x');
    await vi.advanceTimersByTimeAsync(120);
    api.board.mockImplementation(async (_p, q) => response(q ? narrowed : [...all, card('E', 'done')]));
    store.handleEvent({ type: 'changed', project: 'Home' });
    await settle();
    expect(store.baseline?.cards.map((c) => c.id)).toEqual(['A', 'B', 'C', 'D', 'E']);
    expect(order(store)).toEqual(['A']);
  });

  it('shows a move at once in the baseline as well', async () => {
    const api = filtering();
    api.move.mockResolvedValue(card('A', 'done'));
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    store.setQuery('x');
    await vi.advanceTimersByTimeAsync(120);
    const moving = store.move('A', 'done', -1);
    expect(store.baseline?.cards.find((c) => c.id === 'A')?.column).toBe('done');
    await moving;
    expect(store.baseline?.cards.find((c) => c.id === 'A')?.column).toBe('done');
  });

  it('keeps the last good baseline when a filtered fetch fails', async () => {
    const api = filtering();
    const store = createBoardStore(api, 'Home');
    await store.refresh();
    api.board.mockRejectedValue(new ApiError(500, 'error', 'boom'));
    store.setQuery('x');
    await vi.advanceTimersByTimeAsync(120);
    expect(store.baseline?.cards).toHaveLength(4);
  });
});

describe('a board file that does not parse', () => {
  const parse = () => new ApiError(423, 'parse', 'line 12: expected a column', { line: 12 });

  it('shows the bare message when the server sends one', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(
      new ApiError(423, 'parse', '/x/board.md: line 12: expected a column', { line: 12, detail: 'expected a column' }),
    );
    await store.refresh();
    expect(store.parseError).toEqual({ line: 12, message: 'expected a column' });

    const refused = await store.check('A', 1).catch((e: unknown) => e);
    expect(refused).toMatchObject({ status: 423, message: 'expected a column', line: 12 });
  });

  it('sets parseError and read-only, and keeps the last good view', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(parse());
    await store.refresh();
    expect(store.parseError).toEqual({ line: 12, message: 'line 12: expected a column' });
    expect(store.readonly).toBe(true);
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
    expect(toasts.items).toHaveLength(0);
  });

  it('clears it on the next good refresh', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValueOnce(parse());
    await store.refresh();
    api.board.mockResolvedValue(response(start));
    await store.refresh();
    expect(store.parseError).toBeNull();
    expect(store.readonly).toBe(false);
  });

  it('refuses every write without calling the API', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(parse());
    await store.refresh();
    const before = order(store);
    const refused = { status: 423, code: 'parse' };
    await expect(store.move('A', 'doing', 0)).rejects.toMatchObject(refused);
    await expect(store.check('A', 1)).rejects.toMatchObject(refused);
    await expect(store.create({ title: 'x' })).rejects.toMatchObject(refused);
    await expect(store.patch('A', { title: 'x' })).rejects.toMatchObject(refused);
    await expect(store.remove('A')).rejects.toMatchObject(refused);
    await expect(store.archiveDone()).rejects.toMatchObject(refused);
    await expect(store.restore('A')).rejects.toMatchObject(refused);
    await expect(store.updateSettings({ settings })).rejects.toMatchObject(refused);
    for (const fn of [api.move, api.check, api.createCard, api.patch, api.deleteCard, api.archiveDone, api.restore, api.putProjectSettings]) {
      expect(fn).not.toHaveBeenCalled();
    }
    expect(order(store)).toEqual(before);
  });
});

describe('when the server cannot be reached', () => {
  it('keeps the view and does not toast', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(new ApiError(0, 'network', 'Failed to fetch'));
    for (let i = 0; i < 4; i++) await store.refresh();
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
    expect(store.offline).toBe(true);
    expect(toasts.items).toHaveLength(0);
    api.board.mockResolvedValue(response(start));
    await store.refresh();
    expect(store.offline).toBe(false);
  });

  it('toasts a failed write', async () => {
    const { api, store } = await loaded();
    api.deleteCard.mockRejectedValue(new ApiError(0, 'network', 'Failed to fetch'));
    api.board.mockRejectedValue(new ApiError(0, 'network', 'Failed to fetch'));
    await expect(store.remove('A')).rejects.toThrow('Failed to fetch');
    expect(toasts.items.map((t) => t.msg)).toEqual(['Failed to fetch']);
  });
});

describe('toasts', () => {
  it('shows the same message once per 2 seconds', () => {
    toasts.push('boom');
    toasts.push('boom');
    vi.advanceTimersByTime(1999);
    toasts.push('boom');
    expect(toasts.items).toHaveLength(1);
    vi.advanceTimersByTime(1);
    toasts.push('boom');
    toasts.push('other');
    expect(toasts.items.map((t) => t.msg)).toEqual(['boom', 'boom', 'other']);
  });

  it('keeps a message as plain text', () => {
    toasts.push('<img src=x onerror=alert(1)>');
    expect(toasts.items[0]!.msg).toBe('<img src=x onerror=alert(1)>');
    expect(typeof toasts.items[0]!.msg).toBe('string');
  });

  it('uses the server error text for a failed write', async () => {
    const { api, store } = await loaded();
    api.deleteCard.mockRejectedValue(new ApiError(404, 'not_found', 'card A not found'));
    api.board.mockResolvedValue(response(start));
    await expect(store.remove('A')).rejects.toBeInstanceOf(ApiError);
    expect(toasts.items.map((t) => t.msg)).toEqual(['card A not found']);
  });
});

describe('a write refused because the board does not parse', () => {
  it('does not toast, since the banner says it', async () => {
    const { api, store } = await loaded();
    const parse = new ApiError(423, 'parse', 'line 12: expected a column', { line: 12 });
    api.move.mockRejectedValue(parse);
    api.board.mockRejectedValue(parse);
    await store.move('A', 'doing', 0);
    await settle();
    expect(store.readonly).toBe(true);
    expect(toasts.items).toEqual([]);
  });
});

describe('other writes', () => {
  it('adds a created card, and removes a deleted one', async () => {
    const { api, store } = await loaded();
    api.createCard.mockResolvedValue(card('E', 'todo'));
    api.deleteCard.mockResolvedValue(undefined);
    const made = await store.create({ title: 'New' });
    expect(made.id).toBe('E');
    expect(order(store, 'todo')).toEqual(['A', 'B', 'C', 'E']);
    await store.remove('A');
    expect(order(store)).toEqual(['B', 'C', 'D', 'E']);
  });

  it('removes the archived cards', async () => {
    const { api, store } = await loaded();
    api.archiveDone.mockResolvedValue({ archived: ['D'] });
    expect(await store.archiveDone()).toEqual(['D']);
    expect(order(store)).toEqual(['A', 'B', 'C']);
  });

  it('restores an archived card through the queue, into the board', async () => {
    const { api, store } = await loaded();
    const back = { ...card('Z', 'todo'), archived: false };
    api.restore.mockResolvedValue(back);
    expect(await store.restore('Z', 'todo')).toEqual(back);
    expect(api.restore).toHaveBeenCalledWith('Home', 'Z', 'todo');
    expect(order(store, 'todo')).toEqual(['A', 'B', 'C', 'Z']);
  });

  it('restores without a column when none is chosen', async () => {
    const { api, store } = await loaded();
    api.restore.mockResolvedValue(card('Z', 'todo'));
    await store.restore('Z');
    expect(api.restore).toHaveBeenCalledWith('Home', 'Z', undefined);
  });

  it('runs a restore after the writes queued before it', async () => {
    const { api, store } = await loaded();
    const first = deferred<CardView>();
    api.move.mockReturnValue(first.promise);
    api.restore.mockResolvedValue(card('Z', 'todo'));
    void store.move('C', 'todo', 0);
    const done = store.restore('Z');
    await settle();
    expect(api.restore).not.toHaveBeenCalled();
    first.resolve(card('C', 'todo'));
    await done;
    expect(api.restore).toHaveBeenCalledTimes(1);
  });

  it('toasts the server message of a refused restore and rejects', async () => {
    const { api, store } = await loaded();
    api.restore.mockRejectedValue(new ApiError(422, 'move_not_allowed', 'Todo is at its WIP limit (2/2)'));
    api.board.mockResolvedValue(response(start));
    await expect(store.restore('Z', 'todo')).rejects.toMatchObject({ status: 422 });
    expect(toasts.items.map((t) => t.msg)).toEqual(['Todo is at its WIP limit (2/2)']);
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
  });

  it('refuses a restore while the board is read-only', async () => {
    const { api, store } = await loaded();
    api.board.mockRejectedValue(new ApiError(423, 'parse', 'bad', { line: 3 }));
    await store.refresh();
    await expect(store.restore('Z')).rejects.toMatchObject({ status: 423 });
    expect(api.restore).not.toHaveBeenCalled();
  });

  it('counts the changed events of its own project, so a view can reload', async () => {
    const { store } = await loaded();
    const before = store.changes;
    store.handleEvent({ type: 'changed', project: 'Other' });
    expect(store.changes).toBe(before);
    store.handleEvent({ type: 'projects', project: 'Home' });
    expect(store.changes).toBe(before);
    store.handleEvent({ type: 'changed', project: 'Home' });
    expect(store.changes).toBe(before + 1);
    store.handleEvent({ type: 'changed', project: '*' });
    expect(store.changes).toBe(before + 2);
  });

  it('throws a patch conflict for the panel without a toast', async () => {
    const { api, store } = await loaded();
    const current = card('A', 'todo');
    api.patch.mockRejectedValue(new ApiError(409, 'conflict', 'card A changed', { current }));
    api.board.mockResolvedValue(response(start));
    await expect(store.patch('A', { title: 'x' }, 'old')).rejects.toMatchObject({ status: 409, current });
    expect(api.patch).toHaveBeenCalledWith('Home', 'A', { title: 'x' }, 'old');
    expect(toasts.items).toHaveLength(0);
  });

  it('takes the patched card', async () => {
    const { api, store } = await loaded();
    api.patch.mockResolvedValue({ ...card('A', 'todo'), title: 'Renamed' });
    await store.patch('A', { title: 'Renamed' });
    expect(store.view!.cards[0]!.title).toBe('Renamed');
  });
});

describe('settings', () => {
  const change = { settings: { ...settings, name: 'Renamed' } };

  it('sends the settings hash as if_match and takes the answer', async () => {
    const { api, store } = await loaded();
    api.putProjectSettings.mockResolvedValue(response([card('A', 'todo')], 'sh-2'));
    await store.updateSettings(change);
    expect(api.putProjectSettings).toHaveBeenCalledWith('Home', { ...change, if_match: 'sh-1' });
    expect(store.settingsHash).toBe('sh-2');
    expect(order(store)).toEqual(['A']);
  });

  it('refreshes on a 409, then rethrows', async () => {
    const { api, store } = await loaded();
    api.putProjectSettings.mockRejectedValue(new ApiError(409, 'conflict', 'settings changed'));
    api.board.mockResolvedValue(response(start, 'sh-9'));
    await expect(store.updateSettings(change)).rejects.toMatchObject({ status: 409 });
    expect(api.board).toHaveBeenCalledTimes(1);
    expect(store.settingsHash).toBe('sh-9');
  });
});

describe('settings queued behind other settings', () => {
  it('sends the hash the earlier save left, read when the job runs', async () => {
    const { api, store } = await loaded();
    const first = deferred<BoardResponse>();
    api.putProjectSettings.mockReturnValueOnce(first.promise).mockResolvedValueOnce(response(start, 'sh-3'));

    const p1 = store.updateSettings({ settings });
    const p2 = store.updateSettings({ settings });
    await settle();
    first.resolve(response(start, 'sh-2'));
    await Promise.all([p1, p2]);

    expect(api.putProjectSettings.mock.calls.map((c) => c[1].if_match)).toEqual(['sh-1', 'sh-2']);
  });
});

describe('destroy', () => {
  it('cancels a pending query fetch', async () => {
    const { api, store } = await loaded();
    store.setQuery('x');
    store.destroy();
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.board).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('ignores a board that arrives late', async () => {
    const { api, store } = await loaded();
    const late = deferred<BoardResponse>();
    api.board.mockReturnValue(late.promise);
    const p = store.refresh();
    store.destroy();
    late.resolve(response([card('Z', 'todo')]));
    await p;
    expect(order(store)).toEqual(['A', 'B', 'C', 'D']);
  });

  it('ignores a write answer and a failure that arrive late', async () => {
    const { api, store } = await loaded();
    const slow = deferred<CardView>();
    api.move.mockReturnValue(slow.promise);
    const p = store.move('A', 'doing', 0);
    store.destroy();
    slow.reject(new ApiError(422, 'wip_limit', 'full'));
    await p;
    expect(order(store, 'doing')).toEqual(['A', 'D']); // no rollback onto a dead store
    expect(toasts.items).toHaveLength(0);
    await settle();
    expect(api.board).not.toHaveBeenCalled();
  });

  it('does not refresh after being destroyed', async () => {
    const { api, store } = await loaded();
    store.destroy();
    store.handleEvent({ type: 'changed', project: 'Home' });
    await store.refresh();
    expect(api.board).not.toHaveBeenCalled();
  });
});
