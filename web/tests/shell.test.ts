// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App.svelte';
import { accentInk, resolveAccent } from '../src/lib/accent';
import { api, ApiError } from '../src/lib/api';
import { toasts } from '../src/lib/events.svelte';
import { navigate } from '../src/lib/router.svelte';
import type { BoardResponse, CardView, ProjectInfo, Settings } from '../src/lib/types';

vi.mock('../src/lib/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../src/lib/api')>();
  return { ...real, api: { projects: vi.fn(), board: vi.fn() } };
});

// ----- Browser fakes -----

class FakeEventSource {
  static all: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeEventSource.all.push(this);
  }
  addEventListener() {}
  close() {
    this.closed = true;
  }
}

class FakeMedia {
  static all: FakeMedia[] = [];
  static light = false;
  listeners = new Set<(e: { matches: boolean }) => void>();
  constructor(public media: string) {
    FakeMedia.all.push(this);
  }
  get matches() {
    return FakeMedia.light;
  }
  addEventListener(_: string, fn: (e: { matches: boolean }) => void) {
    this.listeners.add(fn);
  }
  removeEventListener(_: string, fn: (e: { matches: boolean }) => void) {
    this.listeners.delete(fn);
  }
  static setLight(light: boolean) {
    FakeMedia.light = light;
    for (const m of FakeMedia.all) for (const fn of m.listeners) fn({ matches: light });
  }
}

// ----- Data -----

const settings = {
  name: 'Home reno',
  prefix: 'H',
  next_id: 10,
  columns: [
    { name: 'Todo', wip: 0, moves_to: [], done: false },
    { name: 'In progress', wip: 2, moves_to: [], done: false },
    { name: 'Done', wip: 0, moves_to: [], done: true },
  ],
  groups: [
    { name: 'Security', color: 'blue' },
    { name: 'Purchasing', color: 'amber' },
  ],
  priorities: ['P0', 'P1'],
  archive: { mode: 'manual', after_days: 0 },
  appearance: { theme: '', accent: '', density: '', swimlane: '', textures: '' },
} as Settings;

function card(id: string, column: string, group: string, blocked = false): CardView {
  return {
    id,
    title: `Card ${id}`,
    column,
    group,
    priority: '',
    depends: [],
    created: null,
    done: null,
    body: '',
    extra: [],
    archived: false,
    blockers: blocked ? [{ id: 'H-1', missing: false }] : [],
    next: false,
    checklist: { done: 0, total: 0 },
    hash: `hash-${id}`,
  };
}

function board(accent = 'orange', theme: 'system' | 'light' | 'dark' = 'system'): BoardResponse {
  return {
    board: {
      settings,
      cards: [
        card('H-1', 'Todo', 'Security'),
        card('H-2', 'Done', 'Security'),
        card('H-3', 'Done', 'Security'),
        card('H-4', 'In progress', 'Purchasing', true),
      ],
      warnings: [],
      settings_hash: 'sh-1',
    },
    appearance: { theme, accent, density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
    readonly: false,
  };
}

const reno: ProjectInfo = { name: 'Home reno', storage: 'markdown', path: '/tmp/reno.md', accent: 'orange' };
const garden: ProjectInfo = { name: 'Garden', storage: 'markdown', path: '/tmp/garden.md', accent: 'cobalt' };

const projects = vi.mocked(api.projects);
const boardCall = vi.mocked(api.board);

function mountApp(list: ProjectInfo[] = [reno, garden], path = '/p/Home%20reno') {
  projects.mockResolvedValue(list);
  navigate(path);
  return render(App);
}

beforeEach(() => {
  FakeEventSource.all = [];
  FakeMedia.all = [];
  FakeMedia.light = false;
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.stubGlobal('matchMedia', (q: string) => new FakeMedia(q));
  window.matchMedia = ((q: string) => new FakeMedia(q)) as unknown as typeof window.matchMedia;
  boardCall.mockResolvedValue(board());
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  toasts.clear();
  localStorage.clear();
  navigate('/');
  const root = document.documentElement;
  root.removeAttribute('style');
  for (const key of ['theme', 'density', 'textures']) delete root.dataset[key];
});

describe('routes', () => {
  it('toggles group:"Name" in the query on click, and clears it on a second click', async () => {
    mountApp();
    const filter = await screen.findByPlaceholderText('Filter');
    const security = await screen.findByRole('button', { name: /Security/ });

    await fireEvent.click(security);
    expect(filter).toHaveValue('group:"Security"');
    expect(security).toHaveAttribute('aria-pressed', 'true');
    await waitFor(() => expect(boardCall).toHaveBeenLastCalledWith('Home reno', 'group:"Security"'));

    await fireEvent.click(security);
    expect(filter).toHaveValue('');
    expect(security).toHaveAttribute('aria-pressed', 'false');
  });

  it('keeps other words in the query when a group is toggled', async () => {
    mountApp();
    const filter = await screen.findByPlaceholderText('Filter');
    await fireEvent.input(filter, { target: { value: 'p0 blocked' } });
    await fireEvent.click(await screen.findByRole('button', { name: /Purchasing/ }));
    expect(filter).toHaveValue('p0 blocked group:"Purchasing"');
    await fireEvent.click(screen.getByRole('button', { name: /Purchasing/ }));
    expect(filter).toHaveValue('p0 blocked');
  });

  it('names the Routes heading Groups, and shows done/total per group', async () => {
    mountApp();
    const heading = await screen.findByRole('heading', { name: 'Groups' });
    expect(heading).toHaveTextContent('Routes');
    expect(heading).toHaveAttribute('title', 'Groups');
    expect(await screen.findByRole('button', { name: /Security/ })).toHaveTextContent('2/3');
    expect(screen.getByRole('button', { name: /Purchasing/ })).toHaveTextContent('0/1');
  });

  it('keeps the counts of the whole board while a filter is on', async () => {
    mountApp();
    const security = await screen.findByRole('button', { name: /Security/ });
    await waitFor(() => expect(security).toHaveTextContent('2/3'));
    const narrowed = board();
    narrowed.board = { ...narrowed.board, cards: [narrowed.board.cards[0]!] };
    boardCall.mockResolvedValue(narrowed);
    await fireEvent.click(security);
    await waitFor(() => expect(boardCall).toHaveBeenLastCalledWith('Home reno', 'group:"Security"'));
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.getByRole('button', { name: /Security/ })).toHaveTextContent('2/3');
  });
});

describe('navigation and landmarks', () => {
  it('lights the active item, counts blocked cards, and has nav, main and a skip link', async () => {
    mountApp();
    const nav = await screen.findByRole('navigation', { name: 'Project' });
    const board = within(nav).getByRole('link', { name: /Board/ });
    expect(board).toHaveAttribute('aria-current', 'page');
    await waitFor(() => expect(within(nav).getByRole('link', { name: /Blocked/ })).toHaveTextContent('1'));
    expect(within(nav).getByRole('link', { name: /Next up/ })).toHaveAttribute('href', '/p/Home%20reno/next');
    expect(screen.getByRole('main')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Skip to the board' })).toHaveAttribute('href', '#board');
  });

  it('lists the columns as a stand-in for the board', async () => {
    mountApp();
    const list = await screen.findByRole('list', { name: 'Columns' });
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(3));
    expect(list).toHaveTextContent('In progress');
  });

  it('has icon-only buttons with a name and a title', async () => {
    mountApp();
    for (const name of ['Board', 'Swimlanes', 'Menu']) {
      const b = await screen.findByRole('button', { name });
      expect(b).toHaveAttribute('title', name);
    }
  });

  it('opens and closes the sidebar drawer from the menu button', async () => {
    mountApp();
    const menu = await screen.findByRole('button', { name: 'Menu' });
    expect(menu).toHaveAttribute('aria-expanded', 'false');
    await fireEvent.click(menu);
    expect(menu).toHaveAttribute('aria-expanded', 'true');
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(menu).toHaveAttribute('aria-expanded', 'false');
  });

  it('focuses the filter on /', async () => {
    mountApp();
    const filter = await screen.findByPlaceholderText('Filter');
    await fireEvent.keyDown(document.body, { key: '/' });
    expect(filter).toHaveFocus();
  });
});

describe('read-only board', () => {
  it('shows the parse error with its line and disables New card', async () => {
    boardCall.mockRejectedValue(
      new ApiError(423, 'parse', '/tmp/reno.md: line 12: invalid card id "???" in depends', { line: 12 }),
    );
    mountApp();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(
      `This board can't be read: line 12: invalid card id "???" in depends. Fix the file and Waypoint will reload it.`,
    );
    expect(screen.getByRole('button', { name: /New card/ })).toBeDisabled();
  });

  it('leaves New card enabled on a good board', async () => {
    mountApp();
    await waitFor(() => expect(screen.getByRole('button', { name: /New card/ })).toBeEnabled());
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

describe('theme', () => {
  it('follows the system theme live when the setting is system', async () => {
    mountApp();
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));
    FakeMedia.setLight(true);
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'));
    expect(document.documentElement.style.getPropertyValue('--acc')).toBe(resolveAccent('orange', 'light'));
    FakeMedia.setLight(false);
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));
  });

  it('ignores the system theme when the project fixes one', async () => {
    boardCall.mockResolvedValue(board('orange', 'dark'));
    mountApp();
    await waitFor(() => expect(document.documentElement.dataset.density).toBe('comfortable'));
    FakeMedia.setLight(true);
    await new Promise((r) => setTimeout(r, 20));
    expect(document.documentElement.dataset.theme).toBe('dark');
  });

  it('sets --acc-ink to the computed ink for the accent', async () => {
    boardCall.mockResolvedValue(board('#2dd4bf'));
    mountApp();
    const root = document.documentElement;
    await waitFor(() => expect(root.style.getPropertyValue('--acc')).toBe('#2dd4bf'));
    expect(root.style.getPropertyValue('--acc-ink')).toBe(accentInk('#2dd4bf'));
    expect(root.dataset.textures).toBe('on');
  });

  it('uses the project accent from the list before the board arrives', async () => {
    boardCall.mockReturnValue(new Promise(() => {}));
    mountApp([garden], '/p/Garden');
    const root = document.documentElement;
    await waitFor(() => expect(root.style.getPropertyValue('--acc')).toBe(resolveAccent('cobalt', 'dark')));
    expect(root.style.getPropertyValue('--acc-ink')).toBe(accentInk(resolveAccent('cobalt', 'dark')));
  });
});

describe('projects', () => {
  it('shows an empty state with New project on a first run, and never asks for a board', async () => {
    mountApp([], '/');
    expect(await screen.findByRole('heading', { name: 'Start your first board' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'New project' })).toBeInTheDocument();
    expect(boardCall).not.toHaveBeenCalled();
    expect(screen.queryByPlaceholderText('Filter')).toBeNull();
  });

  it('shows nothing, not the empty state, while the projects load', async () => {
    projects.mockReturnValue(new Promise(() => {}));
    navigate('/');
    render(App);
    expect(screen.getByRole('main')).toHaveAttribute('aria-busy', 'true');
    expect(screen.queryByRole('heading', { name: 'Start your first board' })).toBeNull();
  });

  it('opens the first project that works when the route names none', async () => {
    mountApp([{ ...reno, error: 'broken' }, garden], '/');
    await waitFor(() => expect(boardCall).toHaveBeenCalledWith('Garden', undefined));
  });

  it('lists a broken project with its error, and shows the error when it is selected', async () => {
    const broken: ProjectInfo = { ...garden, name: 'Old', error: 'old.md: line 3: unterminated front matter' };
    mountApp([reno, broken]);
    await fireEvent.click(await screen.findByRole('button', { name: /Switch project/ }));
    const item = await screen.findByRole('menuitemradio', { name: /Old/ });
    expect(item).toHaveTextContent('old.md: line 3: unterminated front matter');

    await fireEvent.click(item);
    expect(await screen.findByRole('heading', { name: "Old can't be opened" })).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveTextContent('old.md: line 3: unterminated front matter');
    expect(boardCall).not.toHaveBeenCalledWith('Old', undefined);
  });

  it('makes a new store on a switch: the filter resets and the new board is fetched', async () => {
    mountApp();
    const filter = await screen.findByPlaceholderText('Filter');
    await fireEvent.input(filter, { target: { value: 'p0' } });
    expect(filter).toHaveValue('p0');

    await fireEvent.click(screen.getByRole('button', { name: /Switch project/ }));
    await fireEvent.click(await screen.findByRole('menuitemradio', { name: /Garden/ }));

    await waitFor(() => expect(boardCall).toHaveBeenCalledWith('Garden', undefined));
    expect(screen.getByPlaceholderText('Filter')).toHaveValue('');
    await waitFor(() => expect(document.title).toBe('Garden | Waypoint'));
  });

  it('says so when the route names a project that does not exist', async () => {
    mountApp([reno], '/p/Nope');
    expect(await screen.findByRole('heading', { name: 'No project called Nope' })).toBeInTheDocument();
  });

  it('offers New project and Open existing file in the switcher', async () => {
    mountApp();
    await fireEvent.click(await screen.findByRole('button', { name: /Switch project/ }));
    expect(await screen.findByRole('menuitem', { name: 'New project' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Open existing file' })).toBeInTheDocument();
  });
});

describe('status and toasts', () => {
  it('shows Reconnecting once the event stream fails, and not before', async () => {
    mountApp();
    await screen.findByPlaceholderText('Filter');
    const status = screen.getByRole('status');
    expect(status).toHaveTextContent('');
    FakeEventSource.all[0]!.onerror?.();
    await waitFor(() => expect(status).toHaveTextContent('Reconnecting'));
    FakeEventSource.all[1]?.onopen?.();
  });

  it('shows Reconnecting when the server cannot be reached for the board', async () => {
    boardCall.mockRejectedValue(new ApiError(0, 'network', 'fetch failed'));
    mountApp();
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Reconnecting'));
  });

  it('renders toasts in a polite live region and lets them be dismissed', async () => {
    mountApp();
    await screen.findByPlaceholderText('Filter');
    const region = screen.getByRole('region', { name: 'Notifications' });
    expect(region).toHaveAttribute('aria-live', 'polite');
    toasts.push('Card H-1 is locked');
    expect(await within(region).findByText('Card H-1 is locked')).toBeInTheDocument();
    await fireEvent.click(within(region).getByRole('button', { name: 'Dismiss' }));
    expect(within(region).queryByText('Card H-1 is locked')).toBeNull();
  });
});

describe('copy', () => {
  it('uses no em or en dash in the shell components', () => {
    const dirs = ['../src/components', '../src'];
    const files: string[] = [];
    for (const d of dirs) {
      const dir = new URL(d, import.meta.url).pathname;
      for (const f of readdirSync(dir)) if (f.endsWith('.svelte')) files.push(join(dir, f));
    }
    expect(files.length).toBeGreaterThan(5);
    for (const f of files) expect(readFileSync(f, 'utf8'), f).not.toMatch(/[–—]/);
  });
});
