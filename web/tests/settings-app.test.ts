// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App.svelte';
import { resolveAccent } from '../src/lib/accent';
import { api } from '../src/lib/api';
import { toasts } from '../src/lib/events.svelte';
import { preview } from '../src/lib/preview.svelte';
import { navigate, route } from '../src/lib/router.svelte';
import type { BoardResponse, GlobalSettings, ProjectInfo, Settings } from '../src/lib/types';

vi.mock('../src/lib/api', async (importOriginal) => {
  const real = await importOriginal<typeof import('../src/lib/api')>();
  return {
    ...real,
    api: { projects: vi.fn(), board: vi.fn(), settings: vi.fn(), putSettings: vi.fn(), createProject: vi.fn(), putProjectSettings: vi.fn() },
  };
});

class FakeEventSource {
  static all: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  constructor(public url: string) {
    FakeEventSource.all.push(this);
  }
  addEventListener() {}
  close() {}
}

const settings: Settings = {
  name: 'Garden',
  prefix: 'G',
  next_id: 5,
  columns: [
    { name: 'Todo', wip: 0, moves_to: ['Done'], done: false },
    { name: 'Done', wip: 0, moves_to: [], done: true },
  ],
  groups: [],
  priorities: [],
  archive: { mode: 'manual', after_days: 0 },
  appearance: { theme: '', accent: 'cobalt', density: '', swimlane: '', textures: '' },
};

const board = (): BoardResponse => ({
  board: { settings, cards: [], warnings: [], settings_hash: 'sh' },
  appearance: { theme: 'dark', accent: 'cobalt', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
  readonly: false,
});

const global: GlobalSettings = {
  port: 4747, open_browser: true, tls: 'auto', restart_required: false,
  appearance: { theme: 'dark', accent: '', density: '', swimlane: '', textures: '' },
};

const garden: ProjectInfo = { name: 'Garden', storage: 'markdown', path: '/tmp/garden.md', accent: 'cobalt' };
const reno: ProjectInfo = { name: 'Home reno', storage: 'markdown', path: '/tmp/reno.md', accent: 'orange' };

const projects = vi.mocked(api.projects);

function mountApp(list: ProjectInfo[], path: string) {
  projects.mockResolvedValue(list);
  navigate(path);
  return render(App);
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.mocked(api.board).mockImplementation(async () => board());
  vi.mocked(api.settings).mockResolvedValue(global);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  toasts.clear();
  preview.clear();
  localStorage.clear();
  navigate('/');
  const root = document.documentElement;
  root.removeAttribute('style');
  for (const key of ['theme', 'density', 'textures']) delete root.dataset[key];
});

describe('entry points', () => {
  it('shows Project settings and App settings as links while a project is open, both reachable by keyboard', async () => {
    mountApp([garden, reno], '/p/Garden');
    const project = await screen.findByRole('link', { name: 'Project settings' });
    const app = screen.getByRole('link', { name: 'App settings' });
    expect(project).toHaveAttribute('href', '/p/Garden/settings');
    expect(app).toHaveAttribute('href', '/settings');
    expect(project.tabIndex).toBe(0);
    expect(app.tabIndex).toBe(0);
    await fireEvent.click(project);
    expect(route.name).toBe('projectSettings');
    expect(screen.getByRole('link', { name: 'Project settings' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'App settings' })).not.toHaveAttribute('aria-current');
  });

  it('shows only App settings when there is no project', async () => {
    mountApp([], '/');
    expect(await screen.findByRole('link', { name: 'App settings' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Project settings' })).toBeNull();
  });

  it('opens App settings with no project at all', async () => {
    mountApp([], '/settings');
    expect(await screen.findByLabelText('Port')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Start your first board' })).toBeNull();
  });

  it('opens the project settings form at /p/:p/settings', async () => {
    mountApp([garden], '/p/Garden/settings');
    expect(await screen.findByRole('heading', { name: 'Columns' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Project settings' })).toBeInTheDocument();
  });
});

describe('new project and open existing', () => {
  it('opens the dialog on the Create tab from the switcher', async () => {
    mountApp([garden], '/p/Garden');
    await fireEvent.click(await screen.findByRole('button', { name: /Switch project/ }));
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'New project' }));
    const dialog = await screen.findByRole('dialog', { name: 'New project' });
    expect(within(dialog).getByRole('tab', { name: 'Create' })).toHaveAttribute('aria-selected', 'true');
    expect(route.name).toBe('board'); // no longer a stub that navigates to Settings
  });

  it('opens it on the Open tab for Open existing file', async () => {
    mountApp([garden], '/p/Garden');
    await fireEvent.click(await screen.findByRole('button', { name: /Switch project/ }));
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'Open existing file' }));
    const dialog = await screen.findByRole('dialog', { name: 'New project' });
    expect(within(dialog).getByRole('tab', { name: 'Open existing file' })).toHaveAttribute('aria-selected', 'true');
  });

  it('opens it from the first-run empty state', async () => {
    mountApp([], '/');
    await fireEvent.click(await screen.findByRole('button', { name: 'New project' }));
    expect(await screen.findByRole('dialog', { name: 'New project' })).toBeInTheDocument();
  });

  it('goes to the new project by the name the registry gave, after the list has it', async () => {
    mountApp([garden], '/p/Garden');
    vi.mocked(api.createProject).mockImplementation(async () => {
      const made: ProjectInfo = { name: 'Kitchen', storage: 'markdown', path: '/tmp/kitchen.md', accent: 'orange' };
      projects.mockResolvedValue([garden, made]);
      return made;
    });
    await fireEvent.click(await screen.findByRole('button', { name: /Switch project/ }));
    await fireEvent.click(await screen.findByRole('menuitem', { name: 'New project' }));
    await fireEvent.input(await screen.findByLabelText('Name'), { target: { value: 'Kitchen' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
    await waitFor(() => expect(route.project).toBe('Kitchen'));
    expect(route.name).toBe('board');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('heading', { name: /No project called/ })).toBeNull();
  });
});

describe('the look while editing', () => {
  const acc = () => document.documentElement.style.getPropertyValue('--acc');

  it('applies a project accent as you choose it, and puts it back on Cancel', async () => {
    mountApp([garden], '/p/Garden/settings');
    await screen.findByRole('heading', { name: 'Columns' });
    await waitFor(() => expect(acc()).toBe(resolveAccent('cobalt', 'dark')));
    await fireEvent.click(within(screen.getByRole('group', { name: 'Accent' })).getByRole('radio', { name: 'Teal' }));
    await waitFor(() => expect(acc()).toBe(resolveAccent('teal', 'dark')));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(acc()).toBe(resolveAccent('cobalt', 'dark')));
  });

  it('does not keep the preview in the saved look', async () => {
    mountApp([garden], '/p/Garden/settings');
    await screen.findByRole('heading', { name: 'Columns' });
    await fireEvent.click(within(screen.getByRole('group', { name: 'Accent' })).getByRole('radio', { name: 'Teal' }));
    await waitFor(() => expect(acc()).toBe(resolveAccent('teal', 'dark')));
    expect(JSON.parse(localStorage.getItem('waypoint.look')!).accent).toBe('cobalt');
  });

  it('previews the app theme live and reverts on Cancel', async () => {
    mountApp([garden], '/settings');
    await screen.findByLabelText('Port');
    await fireEvent.click(within(screen.getByRole('group', { name: 'Theme' })).getByRole('radio', { name: 'Light' }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));
  });

  it('leaves the preview when the screen is left', async () => {
    mountApp([garden], '/p/Garden/settings');
    await screen.findByRole('heading', { name: 'Columns' });
    await fireEvent.click(within(screen.getByRole('group', { name: 'Accent' })).getByRole('radio', { name: 'Teal' }));
    await waitFor(() => expect(acc()).toBe(resolveAccent('teal', 'dark')));
    navigate('/p/Garden');
    await fireEvent.click(await screen.findByRole('button', { name: 'Leave' }));
    await waitFor(() => expect(acc()).toBe(resolveAccent('cobalt', 'dark')));
  });
});
