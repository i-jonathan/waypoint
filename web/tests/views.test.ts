// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ArchiveView from '../src/components/ArchiveView.svelte';
import ListView from '../src/components/ListView.svelte';
import { ApiError } from '../src/lib/api';
import { createBoardStore, type BoardApi, type BoardStore } from '../src/lib/board.svelte';
import { toasts } from '../src/lib/events.svelte';
import { navigate, route } from '../src/lib/router.svelte';
import type { BoardResponse, CardView, Column, Settings } from '../src/lib/types';
import { archiveMonths, blockedGroups, doneCount, nextUp } from '../src/lib/views';

const columns: Column[] = [
  { name: 'Todo', wip: 0, moves_to: ['In progress', 'Done'], done: false },
  { name: 'In progress', wip: 0, moves_to: ['Todo', 'Done'], done: false },
  { name: 'Done', wip: 0, moves_to: ['In progress'], done: true },
];

function settingsOf(mode: 'off' | 'manual' | 'auto' = 'manual'): Settings {
  return {
    name: 'Home',
    prefix: 'T',
    next_id: 40,
    columns,
    groups: [],
    priorities: ['P0', 'P1'],
    archive: { mode, after_days: 0 },
    appearance: { theme: '', accent: '', density: '', swimlane: '', textures: '' },
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

const archived = (id: string, done: string | null, over: Partial<CardView> = {}) =>
  card(id, 'Done', { archived: true, done, ...over });

function response(cards: CardView[], settings = settingsOf()): BoardResponse {
  return {
    board: { settings, cards, warnings: [], settings_hash: 'sh' },
    appearance: { theme: 'dark', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
    readonly: false,
  };
}

function boardApi(whole: CardView[], filtered?: (q: string) => CardView[], settings = settingsOf()) {
  return {
    board: vi.fn<BoardApi['board']>(async (_p, q) => response(q && filtered ? filtered(q) : whole, settings)),
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

let store: BoardStore | null = null;

async function makeStore(api: ReturnType<typeof boardApi>): Promise<BoardStore> {
  store = createBoardStore(api, 'Home');
  await store.refresh();
  return store;
}

async function mountList(kind: 'next' | 'blocked', api: ReturnType<typeof boardApi>, onNewCard = vi.fn()) {
  const s = await makeStore(api);
  const view = render(ListView, { props: { store: s, project: 'Home', kind, onNewCard } });
  return { ...view, store: s, onNewCard };
}

type ArchiveClient = { archive: ReturnType<typeof vi.fn<(p: string, q?: string) => Promise<CardView[]>>> };

function archiveClient(list: CardView[] | ((q: string | undefined) => CardView[])): ArchiveClient {
  return {
    archive: vi.fn<(p: string, q?: string) => Promise<CardView[]>>(async (_p, q) => (typeof list === 'function' ? list(q) : list)),
  };
}

async function mountArchive(api: ReturnType<typeof boardApi>, client: ArchiveClient) {
  const s = await makeStore(api);
  const view = render(ArchiveView, { props: { store: s, project: 'Home', client } });
  await waitFor(() => expect(client.archive).toHaveBeenCalled());
  return { ...view, store: s };
}

beforeEach(() => {
  toasts.clear();
  navigate('/p/Home/next');
});

afterEach(() => {
  cleanup();
  store?.destroy();
  store = null;
});

const ids = (el: ParentNode) => [...el.querySelectorAll<HTMLElement>('[data-card-id]')].map((c) => c.dataset.cardId);
const text = (el: Element) => el.textContent ?? '';

// ----- The pure parts -----

describe('nextUp', () => {
  it('is the next card, then up to nine more from the first open column in rank order', () => {
    const cards = [
      ...Array.from({ length: 12 }, (_, i) => card(`T-${i + 1}`, 'Todo', { next: i === 0 })),
      card('T-20', 'In progress'),
      card('T-21', 'Done'),
    ];
    const n = nextUp({ settings: settingsOf(), cards, warnings: [], settings_hash: '' });
    expect(n.next?.id).toBe('T-1');
    expect(n.queue.map((c) => c.id)).toEqual(['T-2', 'T-3', 'T-4', 'T-5', 'T-6', 'T-7', 'T-8', 'T-9', 'T-10']);
    expect(n.column).toBe('Todo');
    expect(n.more).toBe(2);
    expect(n.total).toBe(14);
  });

  it('keeps archived cards and later columns out, and survives a next card elsewhere', () => {
    const cards = [card('T-1', 'Todo'), card('T-2', 'Todo', { archived: true }), card('T-3', 'In progress', { next: true })];
    const n = nextUp({ settings: settingsOf(), cards, warnings: [], settings_hash: '' });
    expect(n.next?.id).toBe('T-3');
    expect(n.queue.map((c) => c.id)).toEqual(['T-1']);
  });
});

describe('blockedGroups', () => {
  it('groups blocked cards by first blocker in board order and marks a missing one', () => {
    const cards = [
      card('T-1', 'Todo', { blockers: [{ id: 'T-7', missing: false }] }),
      card('T-2', 'Todo', { blockers: [{ id: 'T-99', missing: true }, { id: 'T-7', missing: false }] }),
      card('T-3', 'Todo', { blockers: [{ id: 'T-7', missing: false }] }),
      card('T-7', 'In progress', { title: 'Seven' }),
    ];
    const groups = blockedGroups({ settings: settingsOf(), cards, warnings: [], settings_hash: '' });
    expect(groups.map((g) => [g.blocker.id, g.blocker.missing, g.cards.map((c) => c.id)])).toEqual([
      ['T-7', false, ['T-1', 'T-3']],
      ['T-99', true, ['T-2']],
    ]);
    expect(groups[0]!.title).toBe('Seven');
    expect(groups[0]!.column).toBe('In progress');
  });
});

describe('doneCount', () => {
  it('counts live cards in done columns', () => {
    const cards = [card('A', 'Done'), card('B', 'Done'), card('C', 'Todo'), archived('D', '2026-10-01')];
    expect(doneCount({ settings: settingsOf(), cards, warnings: [], settings_hash: '' })).toBe(2);
  });
});

describe('archiveMonths', () => {
  it('uses the UTC month of the stored value, newest month first, keeping the file order inside a month', () => {
    const months = archiveMonths([
      archived('A', '2026-09-30T23:59:59Z'),
      archived('B', '2026-10-31T23:30:00Z'),
      archived('C', '2026-11-01T00:30:00+02:00'), // 2026-10-31T22:30Z: still October
      archived('D', '2026-10-26'),
      archived('E', '2026-11-02T00:00:00Z'),
      archived('F', '2025-12-25'),
    ]);
    expect(months.map((m) => [m.label, m.cards.map((c) => c.id)])).toEqual([
      ['NOVEMBER 2026', ['E']],
      ['OCTOBER 2026', ['B', 'C', 'D']],
      ['SEPTEMBER 2026', ['A']],
      ['DECEMBER 2025', ['F']],
    ]);
  });

  it('puts a card with no readable date last', () => {
    const months = archiveMonths([archived('A', null), archived('B', '2026-10-01'), archived('C', 'whenever')]);
    expect(months.map((m) => [m.label, m.cards.map((c) => c.id)])).toEqual([
      ['OCTOBER 2026', ['B']],
      ['NO DATE', ['A', 'C']],
    ]);
  });
});

// ----- Next up -----

describe('Next up view', () => {
  const twelve = [
    ...Array.from({ length: 12 }, (_, i) => card(`T-${i + 1}`, 'Todo', { next: i === 0 })),
    card('T-30', 'In progress'),
    card('T-31', 'Done'),
  ];

  it('shows the next card large at the top, then the next nine from the first open column', async () => {
    const { container } = await mountList('next', boardApi(twelve));
    const hero = container.querySelector('[data-next-card]')!;
    expect(ids(hero)).toEqual(['T-1']);
    const queue = container.querySelector('[data-queue]')!;
    expect(ids(queue)).toEqual(['T-2', 'T-3', 'T-4', 'T-5', 'T-6', 'T-7', 'T-8', 'T-9', 'T-10']);
    expect(screen.getByRole('heading', { level: 2, name: 'Next up' })).toBeInTheDocument();
  });

  it('reads the whole board, not the filtered view', async () => {
    const api = boardApi(twelve, () => [twelve[4]!]);
    const { container, store: s } = await mountList('next', api);
    s.setQuery('T-5');
    await waitFor(() => expect(s.view!.cards).toHaveLength(1));
    expect(ids(container).length).toBe(10);
    expect(ids(container.querySelector('[data-next-card]')!)).toEqual(['T-1']);
  });

  it('has an Open action that opens the card, and a click on the card does too', async () => {
    const { container } = await mountList('next', boardApi(twelve));
    await fireEvent.click(screen.getByRole('button', { name: 'Open T-3' }));
    expect(route.name).toBe('card');
    expect(route.id).toBe('T-3');
    navigate('/p/Home/next');
    await fireEvent.click(container.querySelector('[data-next-card] [data-card-id]')!);
    expect(route.id).toBe('T-1');
  });

  it('opens a card from the keyboard with Enter', async () => {
    const { container } = await mountList('next', boardApi(twelve));
    await fireEvent.keyDown(container.querySelector('[data-queue] [data-card-id="T-4"]')!, { key: 'Enter' });
    expect(route.id).toBe('T-4');
  });

  it('says so, with an action, when there are no cards', async () => {
    const { onNewCard } = await mountList('next', boardApi([]));
    expect(screen.getByText('No cards yet. Add one with N.')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: 'New card' }));
    expect(onNewCard).toHaveBeenCalledTimes(1);
  });

  it('moves focus to the heading', async () => {
    await mountList('next', boardApi(twelve));
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Next up' })).toHaveFocus());
  });

  it('says when no card is next up and the first open column is empty', async () => {
    await mountList('next', boardApi([card('T-1', 'Done')]));
    expect(screen.getByText('Nothing is waiting in Todo.')).toBeInTheDocument();
  });
});

// ----- Blocked -----

describe('Blocked view', () => {
  const blockedBoard = () => [
    card('T-1', 'Todo', { blockers: [{ id: 'T-7', missing: false }] }),
    card('T-2', 'Todo', { blockers: [{ id: 'T-99', missing: true }, { id: 'T-8', missing: false }] }),
    card('T-3', 'In progress', { blockers: [{ id: 'T-7', missing: false }] }),
    card('T-4', 'Todo'),
    card('T-7', 'In progress', { title: 'Get the roof quote' }),
    card('T-8', 'Todo', { title: 'Pick a roofer' }),
  ];

  it('groups blocked cards under their first blocker', async () => {
    await mountList('blocked', boardApi(blockedBoard()));
    const groups = [...document.querySelectorAll<HTMLElement>('[data-blocker-group]')];
    expect(groups.map((g) => [g.dataset.blockerGroup, ids(g)])).toEqual([
      ['T-7', ['T-1', 'T-3']],
      ['T-99', ['T-2']],
    ]);
    expect(text(groups[0]!)).toContain('Get the roof quote');
  });

  it('renders each blocker as a link that opens its card', async () => {
    await mountList('blocked', boardApi(blockedBoard()));
    const g = document.querySelector<HTMLElement>('[data-blocker-group="T-7"]')!;
    const link = within(g).getByRole('link', { name: /T-7/ });
    expect(link).toHaveAttribute('href', '/p/Home/card/T-7');
    await fireEvent.click(link);
    expect(route.name).toBe('card');
    expect(route.id).toBe('T-7');
  });

  it('links a second blocker too', async () => {
    await mountList('blocked', boardApi(blockedBoard()));
    const g = document.querySelector<HTMLElement>('[data-blocker-group="T-99"]')!;
    expect(within(g).getByRole('link', { name: 'T-8' })).toHaveAttribute('href', '/p/Home/card/T-8');
  });

  it('shows a missing blocker as T-99 (missing), with no link', async () => {
    await mountList('blocked', boardApi(blockedBoard()));
    const g = document.querySelector<HTMLElement>('[data-blocker-group="T-99"]')!;
    expect(text(g)).toContain('T-99 (missing)');
    expect(within(g).queryByRole('link', { name: /T-99/ })).toBeNull();
  });

  it('reads the whole board, not the filtered view', async () => {
    const api = boardApi(blockedBoard(), () => [card('T-4', 'Todo')]);
    const { store: s } = await mountList('blocked', api);
    s.setQuery('T-4');
    await waitFor(() => expect(s.view!.cards).toHaveLength(1));
    expect(document.querySelectorAll('[data-blocker-group]')).toHaveLength(2);
  });

  it('says nothing is blocked, with an action', async () => {
    await mountList('blocked', boardApi([card('T-1', 'Todo')]));
    expect(screen.getByText('Nothing is blocked.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open the board' })).toHaveAttribute('href', '/p/Home');
  });

  it('moves focus to the heading', async () => {
    await mountList('blocked', boardApi(blockedBoard()));
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Blocked' })).toHaveFocus());
  });
});

// ----- Archive -----

describe('Archive view', () => {
  const oldFirst = () => [
    archived('T-5', '2026-09-12T10:00:00Z', { title: 'September job' }),
    archived('T-8', '2026-10-26', { title: 'October job' }),
    archived('T-9', '2026-10-30T12:00:00Z', { title: 'Late October job' }),
  ];

  it('groups by the month done under sign headings, newest first, with the file order inside a month', async () => {
    await mountArchive(boardApi([]), archiveClient(oldFirst()));
    const heads = await screen.findAllByRole('heading', { level: 3 });
    expect(heads.map(text)).toEqual(['OCTOBER 2026', 'SEPTEMBER 2026']);
    for (const h of heads) expect(h).toHaveClass('sign');
    const sections = [...document.querySelectorAll<HTMLElement>('[data-month]')];
    expect(sections.map((s) => ids(s))).toEqual([['T-8', 'T-9'], ['T-5']]);
  });

  it('shows the done date with displayDate', async () => {
    await mountArchive(boardApi([]), archiveClient(oldFirst()));
    const row = (await screen.findByText('October job')).closest<HTMLElement>('[data-archived]')!;
    expect(text(row)).toContain('Done 2026-10-26');
  });

  it('restores to the first column by default, through the store', async () => {
    const api = boardApi([]);
    api.restore.mockImplementation(async (_p, id, column) => card(id, column ?? 'Todo'));
    const client = archiveClient(oldFirst());
    await mountArchive(api, client);
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore T-8' }));
    await waitFor(() => expect(api.restore).toHaveBeenCalledWith('Home', 'T-8', 'Todo'));
    await waitFor(() => expect(screen.queryByText('October job')).toBeNull());
    expect(toasts.items.map((t) => t.msg)).toContain('Restored T-8 to Todo');
  });

  it('restores to the column picked in the select', async () => {
    const api = boardApi([]);
    api.restore.mockImplementation(async (_p, id, column) => card(id, column ?? 'Todo'));
    await mountArchive(api, archiveClient(oldFirst()));
    const select = (await screen.findByLabelText('Restore to')) as HTMLSelectElement;
    expect([...select.options].map((o) => o.value)).toEqual(['Todo', 'In progress', 'Done']);
    expect(select.value).toBe('Todo');
    await fireEvent.change(select, { target: { value: 'In progress' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Restore T-5' }));
    await waitFor(() => expect(api.restore).toHaveBeenCalledWith('Home', 'T-5', 'In progress'));
  });

  it('shows the server message of a refused restore and keeps the card listed', async () => {
    const api = boardApi([]);
    api.restore.mockRejectedValue(new ApiError(422, 'wip_limit', 'Todo is at its WIP limit (3/3)'));
    await mountArchive(api, archiveClient(oldFirst()));
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore T-8' }));
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('Todo is at its WIP limit (3/3)'));
    expect(screen.getByText('October job')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Restore T-8' })).not.toBeDisabled();
  });

  it('keeps the keyboard in the list after a restore, on the card that took the place', async () => {
    const api = boardApi([]);
    api.restore.mockImplementation(async (_p, id, column) => card(id, column ?? 'Todo'));
    await mountArchive(api, archiveClient(oldFirst()));
    const first = await screen.findByRole('button', { name: 'Restore T-8' });
    first.focus();
    await fireEvent.click(first);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Restore T-9' })).toHaveFocus());
  });

  it('does not restore while the board is read-only', async () => {
    const api = boardApi([]);
    api.board.mockResolvedValueOnce(response([]));
    const s = await makeStore(api);
    api.board.mockRejectedValue(new ApiError(423, 'parse', 'bad', { line: 3 }));
    await s.refresh();
    expect(s.readonly).toBe(true);
    const client = archiveClient(oldFirst());
    render(ArchiveView, { props: { store: s, project: 'Home', client } });
    const b = await screen.findByRole('button', { name: 'Restore T-8' });
    expect(b).toHaveAttribute('aria-disabled', 'true');
    await fireEvent.click(b);
    expect(api.restore).not.toHaveBeenCalled();
  });

  it('searches the archive with a debounced q and keeps the text through a refresh', async () => {
    const client = archiveClient((q) => (q ? oldFirst().filter((c) => c.title.includes('Late')) : oldFirst()));
    const { store: s } = await mountArchive(boardApi([]), client);
    const box = screen.getByRole('searchbox', { name: 'Search the archive' }) as HTMLInputElement;
    await fireEvent.input(box, { target: { value: 'Late' } });
    expect(client.archive).not.toHaveBeenCalledWith('Home', 'Late'); // not on every key
    await waitFor(() => expect(client.archive).toHaveBeenLastCalledWith('Home', 'Late'));
    await waitFor(() => expect(document.querySelectorAll('[data-archived]')).toHaveLength(1));

    const calls = client.archive.mock.calls.length;
    s.handleEvent({ type: 'changed', project: 'Home' });
    await waitFor(() => expect(client.archive.mock.calls.length).toBeGreaterThan(calls));
    expect(box.value).toBe('Late');
    expect(client.archive).toHaveBeenLastCalledWith('Home', 'Late');
  });

  it('reloads on a change to this project only', async () => {
    const client = archiveClient(oldFirst());
    const { store: s } = await mountArchive(boardApi([]), client);
    const calls = client.archive.mock.calls.length;
    s.handleEvent({ type: 'changed', project: 'Elsewhere' });
    await new Promise((r) => setTimeout(r, 30));
    expect(client.archive.mock.calls.length).toBe(calls);
    s.handleEvent({ type: 'changed', project: 'Home' });
    await waitFor(() => expect(client.archive.mock.calls.length).toBe(calls + 1));
  });

  it('keeps the list when a reload fails, and says why', async () => {
    const list = oldFirst();
    const client = archiveClient(list);
    const { store: s } = await mountArchive(boardApi([]), client);
    await screen.findByText('October job');
    client.archive.mockRejectedValueOnce(new ApiError(500, 'error', 'disk on fire'));
    s.handleEvent({ type: 'changed', project: 'Home' });
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('disk on fire'));
    expect(screen.getByText('October job')).toBeInTheDocument();
  });

  it('says there are no archived cards yet', async () => {
    await mountArchive(boardApi([]), archiveClient([]));
    expect(await screen.findByText('No archived cards yet.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open the board' })).toBeInTheDocument();
  });

  it('says nothing matches a search, and clears it', async () => {
    const client = archiveClient((q) => (q ? [] : oldFirst()));
    await mountArchive(boardApi([]), client);
    const box = screen.getByRole('searchbox', { name: 'Search the archive' }) as HTMLInputElement;
    await fireEvent.input(box, { target: { value: 'zzz' } });
    expect(await screen.findByText('No archived cards match.')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: 'Clear search' }));
    expect(box.value).toBe('');
    await screen.findByText('October job');
  });

  it('moves focus to the heading', async () => {
    await mountArchive(boardApi([]), archiveClient(oldFirst()));
    await waitFor(() => expect(screen.getByRole('heading', { level: 2, name: 'Archive' })).toHaveFocus());
  });

  it('opens a card on click', async () => {
    await mountArchive(boardApi([]), archiveClient(oldFirst()));
    await fireEvent.click(await screen.findByText('October job'));
    expect(route.name).toBe('card');
    expect(route.id).toBe('T-8');
  });
});

describe('Archive done cards', () => {
  const four = () => [
    card('T-1', 'Done'),
    card('T-2', 'Done'),
    card('T-3', 'Done'),
    card('T-4', 'Done'),
    card('T-5', 'Todo'),
    archived('T-6', '2026-10-01'),
  ];

  it('confirms with the count, archives through the store, and toasts', async () => {
    const api = boardApi(four());
    api.archiveDone.mockResolvedValue({ archived: ['T-1', 'T-2', 'T-3', 'T-4'] });
    await mountArchive(api, archiveClient([archived('T-6', '2026-10-01')]));
    await fireEvent.click(screen.getByRole('button', { name: 'Archive done cards' }));
    const dialog = screen.getByRole('alertdialog');
    expect(within(dialog).getByRole('heading', { name: 'Archive 4 done cards?' })).toBeInTheDocument();
    expect(api.archiveDone).not.toHaveBeenCalled();
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Archive' }));
    await waitFor(() => expect(api.archiveDone).toHaveBeenCalledWith('Home'));
    await waitFor(() => expect(toasts.items.map((t) => t.msg)).toContain('Archived 4 cards'));
  });

  it('does nothing when the confirm is cancelled', async () => {
    const api = boardApi(four());
    await mountArchive(api, archiveClient([]));
    await fireEvent.click(screen.getByRole('button', { name: 'Archive done cards' }));
    await fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Cancel' }));
    expect(api.archiveDone).not.toHaveBeenCalled();
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('is hidden when the archive mode is off', async () => {
    await mountArchive(boardApi(four(), undefined, settingsOf('off')), archiveClient([]));
    expect(screen.queryByRole('button', { name: 'Archive done cards' })).toBeNull();
  });

  it('is shown in auto mode', async () => {
    await mountArchive(boardApi(four(), undefined, settingsOf('auto')), archiveClient([]));
    expect(screen.getByRole('button', { name: 'Archive done cards' })).toBeInTheDocument();
  });

  it('is disabled with a reason when nothing is done, and never opens a confirm', async () => {
    await mountArchive(boardApi([card('T-5', 'Todo')]), archiveClient([]));
    const b = screen.getByRole('button', { name: 'Archive done cards' });
    expect(b).toHaveAttribute('aria-disabled', 'true');
    expect(b).toHaveAccessibleDescription('No done cards to archive.');
    await fireEvent.click(b);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });
});

describe('copy', () => {
  it('has no em or en dash in any view', async () => {
    const blocked = [
      card('T-1', 'Todo', { blockers: [{ id: 'T-99', missing: true }] }),
      card('T-2', 'Todo', { next: true }),
    ];
    const a = await mountList('next', boardApi(blocked));
    expect(text(a.container)).not.toMatch(/[–—]/);
    a.unmount();
    store?.destroy();
    const b = await mountList('blocked', boardApi(blocked));
    expect(text(b.container)).not.toMatch(/[–—]/);
    b.unmount();
    store?.destroy();
    await mountArchive(boardApi(blocked), archiveClient([archived('T-9', '2026-10-01'), archived('T-10', null)]));
    await screen.findByText('Card T-9');
    expect(text(document.body)).not.toMatch(/[–—]/);
  });
});
