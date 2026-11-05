import { ApiError, type api } from './api';
import { anchorsAmong, anchorsIn, columnOrder, placeCard, placeOf } from './cardOrder';
import { toggleItem } from './checklist';
import { toasts } from './events.svelte';
import type {
  BoardResponse,
  BoardView,
  CardPatch,
  CardView,
  NewCard,
  ProjectSettingsChange,
  ResolvedAppearance,
} from './types';

/** The routes the store calls. A test passes a fake. */
export type BoardApi = Pick<
  typeof api,
  'board' | 'createCard' | 'patch' | 'move' | 'check' | 'deleteCard' | 'archiveDone' | 'restore' | 'putProjectSettings'
>;

export interface ParseError {
  line?: number;
  message: string;
}

export interface BoardStore {
  readonly view: BoardView | null;
  /**
   * The whole board, whatever the query. It is `view` while no query is set;
   * with one, it is fetched beside the filtered board, so counts stay true.
   */
  readonly baseline: BoardView | null;
  readonly appearance: ResolvedAppearance | null;
  /** Set while the board file does not parse. The last good view stays on screen. */
  readonly parseError: ParseError | null;
  /** True while parseError is set: every write is refused. */
  readonly readonly: boolean;
  /** True while the server cannot be reached. The view is the last one fetched. */
  readonly offline: boolean;
  /** The settings_hash of the view, which updateSettings sends back. */
  readonly settingsHash: string | null;
  readonly query: string;
  /** Changes the query now and refetches once typing has paused for 120ms. */
  setQuery(q: string): void;
  refresh(): Promise<void>;
  /** Shows the move at once. A failure puts the card back, toasts, and refreshes. */
  move(id: string, column: string, index: number): Promise<void>;
  create(n: NewCard): Promise<CardView>;
  /**
   * A 409 is thrown for the panel to handle; it is not toasted. With `quiet`
   * no failure is toasted: the caller shows it where the person is looking.
   */
  patch(id: string, body: CardPatch, ifMatch?: string, opts?: WriteOptions): Promise<CardView>;
  /** Shows the tick at once; a failure undoes just that tick. Sent against the card's hash, so a changed card refuses it. */
  check(id: string, n: number): Promise<void>;
  /** Sent against the card's hash. A 409 is thrown for the caller to handle, and the board refreshes. `quiet` as for patch. */
  remove(id: string, opts?: WriteOptions): Promise<void>;
  archiveDone(): Promise<string[]>;
  /** Brings an archived card back to a column (the first when none is given). A refusal is toasted and thrown; nothing is shown early. */
  restore(id: string, column?: string): Promise<CardView>;
  /** Counts the server's `changed` events for this project, so a view with its own data can reload on a change. */
  readonly changes: number;
  /** Saves the settings against settingsHash. On a 409 it refreshes, then throws. */
  updateSettings(change: Omit<ProjectSettingsChange, 'if_match'>): Promise<void>;
  /** Feeds a server event in: a change to this project refreshes it. */
  handleEvent(e: { type: string; project: string }): void;
  /** Stops timers and ignores every response still on its way. */
  destroy(): void;
}

export interface WriteOptions {
  /** Do not toast a failure: the caller reports it itself. The board still refreshes. */
  quiet?: boolean;
}

const QUERY_DEBOUNCE_MS = 120;
const RETRY_FIRST_MS = 500;
const RETRY_MAX_MS = 10_000;

function messageOf(e: unknown): string {
  if (e instanceof ApiError && e.status === 0) return 'Could not reach Waypoint'; // not the browser's "Failed to fetch"
  return e instanceof Error && e.message !== '' ? e.message : 'Something went wrong';
}

function parseErrorOf(e: ApiError): ParseError {
  const message = e.detail ?? e.message;
  return e.line === undefined ? { message } : { line: e.line, message };
}

/** A move that has been shown but not yet confirmed. */
interface PendingMove {
  id: string;
  /** Where to put the card back if this move fails. */
  prev: Restore;
}

/**
 * A card's place on the whole board: its column, the column's ids without it,
 * and how many of them came before it. A filtered view shows only some of those
 * ids, so each list works out its own neighbours from this when the card goes back.
 */
interface Restore {
  column: string;
  order: string[];
  at: number;
}

export function createBoardStore(client: BoardApi, project: string): BoardStore {
  let view = $state.raw<BoardView | null>(null);
  // The unfiltered board. The same object as `view` when no query is set.
  let baseline = $state.raw<BoardView | null>(null);
  let appearance = $state.raw<ResolvedAppearance | null>(null);
  let parseError = $state.raw<ParseError | null>(null);
  let offline = $state(false);
  let query = $state('');
  let changes = $state(0);
  let destroyed = false;

  // Bumped whenever a write starts, so a board fetched before it can be told apart.
  let writeSeq = 0;
  // Bumped whenever a fetch starts; only the latest fetch may land.
  let fetchSeq = 0;

  // ----- Writes: one at a time, in order. -----
  let tail: Promise<void> = Promise.resolve();
  let writesPending = 0;

  function enqueue<T>(job: () => Promise<T>): Promise<T> {
    writesPending++;
    const result = tail.then(job);
    const settle = () => {
      writesPending--;
      if (writesPending === 0) pump();
    };
    tail = result.then(settle, settle);
    return result;
  }

  // ----- Refreshing: coalesced, and held back while writes are queued. -----
  let wanted = false;
  let waiters: Array<() => void> = [];
  let inFlight: object | null = null;

  // True from a failed write or refresh until a board lands: what is on screen
  // may be a guess, and no event is coming to correct it.
  let stale = false;
  let retryDelay = RETRY_FIRST_MS;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;

  /** Tries the refresh again after a backoff, unless a try is already waiting. */
  function retryLater() {
    stale = true;
    if (destroyed || retryTimer !== undefined) return;
    retryTimer = setTimeout(() => {
      retryTimer = undefined;
      wanted = true;
      pump();
    }, retryDelay);
    retryDelay = Math.min(retryDelay * 2, RETRY_MAX_MS);
  }

  function markFresh() {
    stale = false;
    retryDelay = RETRY_FIRST_MS;
    clearTimeout(retryTimer);
    retryTimer = undefined;
  }

  /** Asks for a refresh. Calls made while one is in flight share one more. */
  function refresh(): Promise<void> {
    if (destroyed) return Promise.resolve();
    wanted = true;
    return new Promise<void>((resolve) => {
      waiters.push(resolve);
      pump();
    });
  }

  function releaseWaiters(batch: Array<() => void>) {
    for (const resolve of batch) resolve();
  }

  /** Starts the wanted refresh if nothing stops it. `supersede` starts it even with another in flight. */
  function pump(supersede = false) {
    if (destroyed || !wanted || writesPending > 0) return;
    if (inFlight && !supersede) return;
    wanted = false;
    const batch = waiters;
    waiters = [];
    const token = {};
    inFlight = token;
    void fetchBoard().then((retry) => {
      if (inFlight === token) inFlight = null;
      if (retry) waiters.push(...batch); // they asked for a board that has not landed yet
      else releaseWaiters(batch);
      pump();
    });
  }

  /** Fetches and shows the board. Resolves true when the result was dropped because a write began, so it must be fetched again. */
  async function fetchBoard(): Promise<boolean> {
    const seq = ++fetchSeq;
    const asked = query;
    const writesThen = writeSeq;
    let res: BoardResponse;
    let whole: BoardView | null = null;
    try {
      if (asked === '') {
        res = await client.board(project, undefined);
      } else {
        // The filtered board and the whole one, side by side: the sidebar counts the whole.
        const [filtered, unfiltered] = await Promise.all([client.board(project, asked), client.board(project, undefined)]);
        res = filtered;
        whole = unfiltered.board;
      }
    } catch (e) {
      if (destroyed || seq !== fetchSeq) return false;
      if (e instanceof ApiError && e.status === 0) {
        offline = true; // keep the view; the event stream's own status says the rest
      } else if (e instanceof ApiError && e.status === 423) {
        offline = false;
        parseError = parseErrorOf(e);
      } else if (!stale) {
        toasts.push(messageOf(e)); // once: the retries that follow stay quiet
      }
      retryLater();
      return false;
    }
    if (destroyed || seq !== fetchSeq) return false; // a newer fetch has been made
    if (asked !== query) return false; // for a query that is no longer the one shown
    if (writesThen !== writeSeq) {
      wanted = true; // a write began meanwhile: this board may predate it
      return true;
    }
    markFresh();
    offline = false;
    parseError = null;
    view = res.board;
    baseline = whole ?? res.board;
    appearance = res.appearance;
    for (const id of answered.keys()) if (!busy.has(id)) answered.delete(id);
    return false;
  }

  // ----- Query -----
  let debounce: ReturnType<typeof setTimeout> | undefined;

  function setQuery(q: string) {
    if (destroyed || q === query) return;
    query = q;
    clearTimeout(debounce);
    debounce = setTimeout(() => {
      wanted = true;
      pump(true);
    }, QUERY_DEBOUNCE_MS);
  }

  // ----- Pending work per card, so a late answer never undoes a newer change. -----
  const busy = new Map<string, number>();
  const moves: PendingMove[] = [];
  // The hash in an answer that could not be shown because more work on the card
  // was waiting. That work has to send this hash, not the one on screen.
  const answered = new Map<string, string>();

  /** The hash of the server's latest version of a card, as far as this tab knows. */
  function hashOf(id: string): string | undefined {
    return answered.get(id) ?? view?.cards.find((c) => c.id === id)?.hash;
  }

  function hold(id: string) {
    busy.set(id, (busy.get(id) ?? 0) + 1);
  }
  /** Releases a card, and says whether more work on it is still waiting. */
  function release(id: string): boolean {
    const n = (busy.get(id) ?? 1) - 1;
    if (n <= 0) {
      busy.delete(id);
      return false;
    }
    busy.set(id, n);
    return true;
  }

  /** Applies a change to the cards of the view, and of the baseline when that is a separate board. */
  function setCards(update: (cards: CardView[]) => CardView[]) {
    if (!view) return;
    const shared = baseline === view;
    view = { ...view, cards: update(view.cards) };
    if (shared) baseline = view;
    else if (baseline) baseline = { ...baseline, cards: update(baseline.cards) };
  }

  /** Swaps the server's version of a card in, where it stands, unless more work on it is waiting. */
  function adopt(card: CardView, stillBusy: boolean) {
    if (stillBusy) {
      answered.set(card.id, card.hash);
      return;
    }
    answered.delete(card.id);
    setCards((cards) => cards.map((c) => (c.id === card.id ? card : c)));
  }

  // ----- The shared shape of a write. -----
  function refuseIfReadonly(): ApiError | null {
    if (!parseError) return null;
    return new ApiError(423, 'parse', parseError.message, parseError.line === undefined ? {} : { line: parseError.line });
  }

  /**
   * Runs one write in the queue. On failure it undoes the optimistic change
   * with `rollback`, toasts the server's message, and asks for a refresh so the
   * server's board wins, retrying until one lands. It does not wait for that
   * refresh: it is held back until the queue is empty, and this job is in the
   * queue. A 423 is not toasted, because the banner already says it.
   */
  function write<T>(job: () => Promise<T>, onFail: (e: unknown) => void = () => {}, quiet = false): Promise<T> {
    writeSeq++;
    return enqueue(async () => {
      try {
        return await job();
      } catch (e) {
        if (!destroyed) {
          onFail(e);
          const parse = e instanceof ApiError && e.status === 423;
          if (parse) {
            parseError = parseErrorOf(e);
          }
          if (!parse && !quiet && !(e instanceof ApiError && e.status === 409 && e.current)) toasts.push(messageOf(e));
          stale = true;
          void refresh();
        }
        throw e;
      }
    });
  }

  // ----- Operations -----
  function move(id: string, column: string, index: number): Promise<void> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);

    const whole = (baseline ?? view)?.cards;
    const was = whole ? placeOf(whole, id) : null;
    const prev: Restore | null = was && whole ? { column: was.column, order: columnOrder(whole, was.column, id), at: was.index } : null;
    if (prev) {
      // The index is in the whole column. A filtered view shows only some of it, so
      // each list is told which of its own cards the card goes between.
      setCards((cards) => placeCard(cards, id, anchorsIn(cards, whole!, id, column, index)));
    }
    const entry: PendingMove | null = prev ? { id, prev } : null;
    if (entry) moves.push(entry);
    hold(id);

    return write(
      async () => {
        const card = await client.move(project, id, column, index < 0 ? undefined : index);
        if (destroyed) return;
        if (entry) moves.splice(moves.indexOf(entry), 1);
        adopt(card, release(id));
      },
      () => {
        if (!entry) return;
        const at = moves.indexOf(entry);
        moves.splice(at, 1);
        const later = moves.slice(at).find((m) => m.id === id);
        if (later) later.prev = entry.prev; // the next move starts from where this one began
        else setCards((cards) => placeCard(cards, id, anchorsAmong(cards, entry.prev.column, entry.prev.order, entry.prev.at)));
      },
    ).catch(() => {
      release(id); // the failure was handled by write(): rolled back, toasted, refresh asked
    });
  }

  function check(id: string, n: number): Promise<void> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);

    let applied = false;
    const card = view?.cards.find((c) => c.id === id);
    if (card) {
      const t = toggleItem(card.body, n);
      if (t) {
        applied = true;
        const delta = t.wasChecked ? -1 : 1;
        const next = { ...card, body: t.body, checklist: { ...card.checklist, done: card.checklist.done + delta } };
        setCards((cards) => cards.map((c) => (c.id === id ? next : c)));
      }
    }
    hold(id);

    return write(
      async () => {
        const updated = await client.check(project, id, n, hashOf(id));
        if (destroyed) return;
        adopt(updated, release(id));
      },
      (e) => {
        if (e instanceof ApiError && e.status === 409) {
          toasts.push('This card changed elsewhere, so that tick was not saved.');
        }
        if (!applied) return;
        // Tick it back on whatever the card is now, so other ticks are kept.
        setCards((cards) =>
          cards.map((c) => {
            if (c.id !== id) return c;
            const t = toggleItem(c.body, n);
            if (!t) return c;
            return { ...c, body: t.body, checklist: { ...c.checklist, done: c.checklist.done + (t.wasChecked ? -1 : 1) } };
          }),
        );
      },
    ).catch(() => {
      release(id);
    });
  }

  function create(n: NewCard): Promise<CardView> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);
    return write(async () => {
      const card = await client.createCard(project, n);
      if (!destroyed) setCards((cards) => (cards.some((c) => c.id === card.id) ? cards : [...cards, card]));
      return card;
    });
  }

  function patch(id: string, body: CardPatch, ifMatch?: string, opts: WriteOptions = {}): Promise<CardView> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);
    hold(id);
    return write(async () => {
      try {
        const card = await client.patch(project, id, body, ifMatch);
        if (!destroyed) adopt(card, release(id));
        return card;
      } catch (e) {
        release(id);
        throw e;
      }
    }, undefined, opts.quiet);
  }

  function remove(id: string, opts: WriteOptions = {}): Promise<void> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);
    return write(async () => {
      await client.deleteCard(project, id, hashOf(id));
      answered.delete(id);
      if (!destroyed) setCards((cards) => cards.filter((c) => c.id !== id));
    }, undefined, opts.quiet);
  }

  function archiveDone(): Promise<string[]> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);
    return write(async () => {
      const { archived } = await client.archiveDone(project);
      if (!destroyed) setCards((cards) => cards.filter((c) => !archived.includes(c.id)));
      return archived;
    });
  }

  function restore(id: string, column?: string): Promise<CardView> {
    const refused = refuseIfReadonly();
    if (refused) return Promise.reject(refused);
    return write(async () => {
      const card = await client.restore(project, id, column);
      if (!destroyed) setCards((cards) => (cards.some((c) => c.id === card.id) ? cards.map((c) => (c.id === card.id ? card : c)) : [...cards, card]));
      return card;
    });
  }

  async function updateSettings(change: Omit<ProjectSettingsChange, 'if_match'>): Promise<void> {
    const refused = refuseIfReadonly();
    if (refused) throw refused;
    if (!view) throw new Error('The board has not loaded yet');

    let conflict: Promise<void> | null = null;
    try {
      await write(async () => {
        try {
          // Read now, not at the call: a save queued ahead of this one moves the hash.
          const res = await client.putProjectSettings(project, { ...change, if_match: view!.settings_hash });
          if (!destroyed) {
            offline = false;
            view = res.board;
            baseline = res.board;
            appearance = res.appearance;
          }
        } catch (e) {
          // Hold the refresh now, so it runs once the queue is empty.
          if (e instanceof ApiError && e.status === 409) conflict = refresh();
          throw e;
        }
      });
    } catch (e) {
      if (conflict) await conflict;
      throw e;
    }
  }

  function handleEvent(e: { type: string; project: string }) {
    if (e.type !== 'changed') return;
    if (e.project !== project && e.project !== '*') return;
    changes++;
    void refresh();
  }

  function destroy() {
    destroyed = true;
    clearTimeout(debounce);
    clearTimeout(retryTimer);
    wanted = false;
    const batch = waiters;
    waiters = [];
    releaseWaiters(batch);
  }

  return {
    get view() {
      return view;
    },
    get baseline() {
      return baseline;
    },
    get appearance() {
      return appearance;
    },
    get parseError() {
      return parseError;
    },
    get readonly() {
      return parseError !== null;
    },
    get offline() {
      return offline;
    },
    get settingsHash() {
      return view?.settings_hash ?? null;
    },
    get query() {
      return query;
    },
    setQuery,
    refresh,
    move,
    create,
    patch,
    check,
    remove,
    archiveDone,
    restore,
    get changes() {
      return changes;
    },
    updateSettings,
    handleEvent,
    destroy,
  };
}
