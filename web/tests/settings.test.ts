// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GlobalSettings from '../src/components/GlobalSettings.svelte';
import NewProjectDialog from '../src/components/NewProjectDialog.svelte';
import ProjectSettings from '../src/components/ProjectSettings.svelte';
import { ApiError } from '../src/lib/api';
import { createBoardStore, type BoardApi, type BoardStore } from '../src/lib/board.svelte';
import { toasts } from '../src/lib/events.svelte';
import { preview } from '../src/lib/preview.svelte';
import { navigate, route } from '../src/lib/router.svelte';
import type { BoardResponse, CardView, GlobalSettings as Global, ProjectInfo, Settings } from '../src/lib/types';

// ----- Data -----

const baseSettings = (): Settings => ({
  name: 'Home',
  prefix: 'T',
  next_id: 20,
  columns: [
    { name: 'Todo', wip: 0, moves_to: ['Doing', 'Done'], done: false },
    { name: 'Doing', wip: 0, moves_to: ['Todo', 'Done'], done: false },
    { name: 'Done', wip: 0, moves_to: [], done: true },
  ],
  groups: [
    { name: 'Security', color: 'blue' },
    { name: 'Purchasing', color: 'amber' },
  ],
  priorities: ['P0', 'P1'],
  archive: { mode: 'manual', after_days: 0 },
  appearance: { theme: '', accent: 'cobalt', density: '', swimlane: '', textures: '' },
});

function card(id: string, column: string, group = ''): CardView {
  return {
    id, title: id, column, group, priority: '', depends: [], created: null, done: null, body: '', extra: [],
    archived: false, blockers: [], next: false, checklist: { done: 0, total: 0 }, hash: id,
  };
}

const cardsOf = (): CardView[] => [
  card('T-1', 'Todo', 'Security'),
  card('T-2', 'Todo', 'Security'),
  card('T-3', 'Doing', 'Security'),
  card('T-4', 'Doing', 'Security'),
  card('T-5', 'Doing', 'Purchasing'),
];

const info: ProjectInfo = { name: 'Home', storage: 'markdown', path: '/data/home.md', accent: 'cobalt' };

const globalOf = (over: Partial<Global> = {}): Global => ({
  port: 4747,
  open_browser: true,
  tls: 'auto',
  appearance: { theme: 'dark', accent: '', density: '', swimlane: '', textures: '' },
  restart_required: false,
  ...over,
});

/** A server for one board: it keeps the settings, and answers a save by applying it. */
function server(settings: Settings = baseSettings(), cards: CardView[] = cardsOf()) {
  const state = { settings, cards, hash: 'sh', n: 0 };
  const response = (): BoardResponse => ({
    board: { settings: state.settings, cards: state.cards, warnings: [], settings_hash: state.hash },
    appearance: {
      theme: 'dark', accent: 'cobalt', density: 'comfortable', swimlane: 'none', textures: 'on',
      source: { theme: 'global', accent: 'project', density: 'default', swimlane: 'default', textures: 'default' },
    },
    readonly: false,
  });
  const api = {
    board: vi.fn<BoardApi['board']>(async () => response()),
    createCard: vi.fn<BoardApi['createCard']>(),
    patch: vi.fn<BoardApi['patch']>(),
    move: vi.fn<BoardApi['move']>(),
    check: vi.fn<BoardApi['check']>(),
    deleteCard: vi.fn<BoardApi['deleteCard']>(),
    archiveDone: vi.fn<BoardApi['archiveDone']>(),
    restore: vi.fn<BoardApi['restore']>(),
    putProjectSettings: vi.fn<BoardApi['putProjectSettings']>(async (_p, change) => {
      state.settings = change.settings;
      state.hash = `sh${++state.n}`;
      return response();
    }),
  };
  return { state, api, response };
}

let store: BoardStore | null = null;
let srv: ReturnType<typeof server>;
const globalClient = { settings: vi.fn(async () => globalOf()), putSettings: vi.fn(), archive: vi.fn(async (): Promise<CardView[]> => []) };

async function mount(settings?: Settings, cards?: CardView[]) {
  srv = server(settings, cards);
  store = createBoardStore(srv.api, 'Home');
  await store.refresh();
  const view = render(ProjectSettings, { props: { store, project: 'Home', info, client: globalClient } });
  await screen.findByRole('heading', { name: 'Columns' });
  return view;
}

const type = (el: HTMLElement, value: string) => fireEvent.input(el, { target: { value } });
const row = (kind: string, n: number) => screen.getByRole('group', { name: `${kind} ${n}` });
const save = () => screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement;
const put = () => srv.api.putProjectSettings;
const sent = (n = 0) => put().mock.calls[n]![1];
const fieldset = (name: string) => screen.getByRole('group', { name });

beforeEach(() => {
  globalClient.settings.mockImplementation(async () => globalOf());
  navigate('/p/Home/settings');
});

afterEach(() => {
  cleanup();
  store?.destroy();
  store = null;
  toasts.clear();
  vi.clearAllMocks();
  preview.clear();
  navigate('/');
  document.body.innerHTML = '';
});

// ----- Columns -----

describe('columns', () => {
  it('sends a rename as column_renames, against the hash the form was loaded from', async () => {
    await mount();
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalledTimes(1));
    expect(sent()).toMatchObject({ column_renames: { Todo: 'Backlog' }, group_renames: {}, priority_renames: {}, if_match: 'sh' });
    expect(sent().settings.columns.map((c) => c.name)).toEqual(['Backlog', 'Doing', 'Done']);
    expect(sent().settings.columns[1]!.moves_to).toEqual(['Backlog', 'Done']);
  });

  it('sends a swap of two names as two renames', async () => {
    await mount();
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Doing');
    await type(within(row('Column', 2)).getByLabelText('Name'), 'Todo');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().column_renames).toEqual({ Todo: 'Doing', Doing: 'Todo' });
  });

  it('disables Remove for a column with cards, and says why', async () => {
    await mount();
    const todo = within(row('Column', 1)).getByRole('button', { name: 'Remove column Todo' });
    expect(todo).toBeDisabled();
    expect(todo).toHaveAttribute('title', 'Move its 2 cards out first');
    expect(within(row('Column', 2)).getByRole('button', { name: 'Remove column Doing' })).toHaveAttribute('title', 'Move its 3 cards out first');
    expect(within(row('Column', 3)).getByRole('button', { name: 'Remove column Done' })).toBeEnabled();
  });

  it('removes an empty column, and drops it from the others', async () => {
    await mount();
    await fireEvent.click(within(row('Column', 3)).getByRole('button', { name: 'Remove column Done' }));
    expect(screen.queryByRole('group', { name: 'Column 3' })).toBeNull();
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.columns.map((c) => [c.name, c.moves_to])).toEqual([
      ['Todo', ['Doing']],
      ['Doing', ['Todo']],
    ]);
  });

  it('keeps the last column', async () => {
    const one = baseSettings();
    one.columns = [{ name: 'Only', wip: 0, moves_to: [], done: false }];
    await mount(one, []);
    const remove = within(row('Column', 1)).getByRole('button', { name: 'Remove column Only' });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAttribute('title', 'At least one column must remain');
  });

  it('reorders with the up and down buttons, announces it, and keeps focus on a button that still works', async () => {
    await mount();
    await fireEvent.click(within(row('Column', 2)).getByRole('button', { name: 'Move column Doing up' }));
    expect(within(row('Column', 1)).getByLabelText('Name')).toHaveValue('Doing');
    expect(within(row('Column', 2)).getByLabelText('Name')).toHaveValue('Todo');
    expect(screen.getByText('Doing moved to position 1 of 3')).toBeInTheDocument();
    await waitFor(() => expect(within(row('Column', 1)).getByRole('button', { name: 'Move column Doing down' })).toHaveFocus());
    expect(within(row('Column', 1)).getByRole('button', { name: 'Move column Doing up' })).toBeDisabled();
    expect(within(row('Column', 3)).getByRole('button', { name: 'Move column Done down' })).toBeDisabled();

    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.columns.map((c) => c.name)).toEqual(['Doing', 'Todo', 'Done']);
    expect(sent().column_renames).toEqual({});
  });

  it('adds New column with no moves, focused for renaming', async () => {
    await mount();
    await fireEvent.click(screen.getByRole('button', { name: 'Add column' }));
    const added = within(row('Column', 4)).getByLabelText('Name');
    expect(added).toHaveValue('New column');
    await waitFor(() => expect(added).toHaveFocus());
    expect(within(row('Column', 4)).getByText("Cards can't leave this column")).toBeInTheDocument();
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.columns[3]).toEqual({ name: 'New column', wip: 0, moves_to: [], done: false });
  });

  it('notes a column that cards cannot leave, and only that one', async () => {
    await mount();
    expect(within(row('Column', 3)).getByText("Cards can't leave this column")).toBeInTheDocument();
    expect(within(row('Column', 1)).queryByText("Cards can't leave this column")).toBeNull();
    await fireEvent.click(within(row('Column', 3)).getByRole('checkbox', { name: 'Doing' }));
    expect(within(row('Column', 3)).queryByText("Cards can't leave this column")).toBeNull();
  });

  it('takes a WIP limit, and refuses anything but a whole number with the error under the field and focus on it', async () => {
    await mount();
    const wip = within(row('Column', 2)).getByLabelText('WIP limit');
    await type(wip, '1.5');
    expect(within(row('Column', 2)).getByText('Use a whole number above 0, or leave it empty')).toBeInTheDocument();
    expect(wip).toHaveAttribute('aria-invalid', 'true');
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    await fireEvent.click(save());
    expect(put()).not.toHaveBeenCalled();
    await waitFor(() => expect(wip).toHaveFocus());

    await type(wip, '2');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.columns[1]!.wip).toBe(2);
  });

  it('flags a repeated column name', async () => {
    await mount();
    await type(within(row('Column', 2)).getByLabelText('Name'), 'Todo');
    expect(within(row('Column', 2)).getByText('A column called Todo already exists')).toBeInTheDocument();
  });
});

// ----- Groups and priorities -----

describe('groups and priorities', () => {
  it('sends group and priority renames, and a colour change', async () => {
    await mount();
    await type(within(row('Group', 1)).getByLabelText('Name'), 'Safety');
    await fireEvent.change(within(row('Group', 2)).getByLabelText('Colour'), { target: { value: 'teal' } });
    await type(within(row('Priority', 2)).getByLabelText('Name'), 'High');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent()).toMatchObject({ group_renames: { Security: 'Safety' }, priority_renames: { P1: 'High' } });
    expect(sent().settings.groups).toEqual([
      { name: 'Safety', color: 'blue' },
      { name: 'Purchasing', color: 'teal' },
    ]);
    expect(sent().settings.priorities).toEqual(['P0', 'High']);
  });

  it('takes a custom hex colour and refuses a bad one', async () => {
    await mount();
    await fireEvent.change(within(row('Group', 1)).getByLabelText('Colour'), { target: { value: 'custom' } });
    const hex = within(row('Group', 1)).getByLabelText('Hex colour');
    await type(hex, '#12');
    expect(within(row('Group', 1)).getByText('Use #rrggbb, for example #2f5bff')).toBeInTheDocument();
    await type(hex, '#12ab9c');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.groups[0]).toEqual({ name: 'Security', color: '#12ab9c' });
  });

  it('adds a group and a priority', async () => {
    await mount();
    await fireEvent.click(screen.getByRole('button', { name: 'Add group' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Add priority' }));
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.groups.map((g) => g.name)).toEqual(['Security', 'Purchasing', 'New group']);
    expect(sent().settings.priorities).toEqual(['P0', 'P1', 'New priority']);
  });

  it('reorders priorities with the up and down buttons', async () => {
    await mount();
    await fireEvent.click(within(row('Priority', 2)).getByRole('button', { name: 'Move priority P1 up' }));
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.priorities).toEqual(['P1', 'P0']);
    expect(sent().priority_renames).toEqual({});
  });

  it('counts archived cards in the warning, and saves nothing if the archive cannot be read', async () => {
    await mount();
    await fireEvent.click(within(row('Group', 1)).getByRole('button', { name: 'Remove group Security' }));
    globalClient.archive.mockRejectedValueOnce(new Error('archive unreadable'));
    await fireEvent.click(save());
    expect(await screen.findByText('archive unreadable')).toBeInTheDocument();
    expect(put()).not.toHaveBeenCalled();

    globalClient.archive.mockResolvedValueOnce([card('T-99', 'Done', 'Security')]);
    await fireEvent.click(save());
    expect(await screen.findByRole('alertdialog')).toHaveTextContent('Removing Security clears it from 5 cards, including 1 archived.');
    expect(put()).not.toHaveBeenCalled();
  });

  it('warns with a count before removing a group that cards have, and sends nothing until told to', async () => {
    await mount();
    await fireEvent.click(within(row('Group', 1)).getByRole('button', { name: 'Remove group Security' }));
    await fireEvent.click(save());
    const dialog = await screen.findByRole('alertdialog');
    expect(dialog).toHaveTextContent('Removing Security clears it from 4 cards.');
    expect(put()).not.toHaveBeenCalled();

    await fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(screen.queryByRole('group', { name: 'Group 2' })).toBeNull(); // the form is kept as edited
    expect(put()).not.toHaveBeenCalled();

    await fireEvent.click(save());
    await fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Save and clear' }));
    await waitFor(() => expect(put()).toHaveBeenCalledTimes(1));
    expect(sent().settings.groups.map((g) => g.name)).toEqual(['Purchasing']);
  });

  it('counts the whole board, not the filtered view', async () => {
    srv = server();
    store = createBoardStore(srv.api, 'Home');
    await store.refresh();
    srv.api.board.mockImplementation(async (_p, q) => ({
      ...srv.response(),
      board: { ...srv.response().board, cards: q ? [card('T-1', 'Todo', 'Security')] : srv.state.cards },
    }));
    store.setQuery('T-1');
    await store.refresh();
    render(ProjectSettings, { props: { store, project: 'Home', info, client: globalClient } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Remove group Security' }));
    await fireEvent.click(save());
    expect(await screen.findByRole('alertdialog')).toHaveTextContent('Removing Security clears it from 4 cards.');
  });

  it('says a priority with no cards needs no warning', async () => {
    await mount();
    await fireEvent.click(within(row('Priority', 1)).getByRole('button', { name: 'Remove priority P0' }));
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('shows the server message plainly, with a hint to save in two steps, when a removal and a rename collide', async () => {
    await mount();
    put().mockRejectedValueOnce(
      new ApiError(422, 'invalid', 'invalid: groups "Security" and "Purchasing" would both become "Security"'),
    );
    await type(within(row('Group', 2)).getByLabelText('Name'), 'Security');
    await fireEvent.click(within(row('Group', 1)).getByRole('button', { name: 'Remove group Security' }));
    await fireEvent.click(save()); // the name is taken over by the other group, so nothing is cleared: no warning
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('groups "Security" and "Purchasing" would both become "Security"');
    expect(alert).toHaveTextContent('Save in two steps: remove the group first, then rename the other.');
  });
});

// ----- Appearance -----

describe('appearance', () => {
  it('shows where each value comes from', async () => {
    await mount();
    await waitFor(() => expect(globalClient.settings).toHaveBeenCalled());
    expect(within(fieldset('Theme')).getByText('From app settings')).toBeInTheDocument();
    expect(within(fieldset('Density')).getByText('Built-in')).toBeInTheDocument();
    expect(within(fieldset('Accent')).queryByText('From app settings')).toBeNull();
    expect(within(fieldset('Accent')).queryByText('Built-in')).toBeNull();
    expect(within(fieldset('Accent')).getByRole('button', { name: 'Reset to global' })).toBeInTheDocument();
    expect(within(fieldset('Theme')).queryByRole('button', { name: 'Reset to global' })).toBeNull();
    expect(within(fieldset('Theme')).getByRole('radio', { name: 'Dark' })).toBeChecked(); // the inherited value is shown
  });

  it('sends accent "" for Reset to global, and shows the inherited source afterwards', async () => {
    await mount();
    await fireEvent.click(within(fieldset('Accent')).getByRole('button', { name: 'Reset to global' }));
    expect(within(fieldset('Accent')).getByText('Built-in')).toBeInTheDocument();
    expect(within(fieldset('Accent')).getByRole('radio', { name: 'Orange' })).toBeChecked();
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.appearance.accent).toBe('');
  });

  it('sets a project value when a choice is made, and drops the badge', async () => {
    await mount();
    await fireEvent.click(within(fieldset('Theme')).getByRole('radio', { name: 'Light' }));
    expect(within(fieldset('Theme')).queryByText('From app settings')).toBeNull();
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.appearance.theme).toBe('light');
  });

  it('offers 8 accent swatches with text labels, and a custom hex checked on the client', async () => {
    await mount();
    const accent = fieldset('Accent');
    const names = within(accent).getAllByRole('radio').map((r) => r.getAttribute('aria-label') ?? (r as HTMLInputElement).labels?.[0]?.textContent?.trim());
    expect(names).toEqual(['Orange', 'Cobalt', 'Teal', 'Rose', 'Green', 'Amber', 'Sky', 'Violet']);
    const hex = within(accent).getByLabelText('Custom colour');
    await type(hex, '#12');
    expect(within(accent).getByText('Use #rrggbb, for example #2f5bff')).toBeInTheDocument();
    await fireEvent.click(save());
    expect(put()).not.toHaveBeenCalled();
    await waitFor(() => expect(hex).toHaveFocus());
    await type(hex, '#12ab9c');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.appearance.accent).toBe('#12ab9c');
  });

  it('warns about a custom accent that cannot reach 3:1 against the canvas of the current theme', async () => {
    await mount();
    const accent = fieldset('Accent');
    await type(within(accent).getByLabelText('Custom colour'), '#16201b'); // near the dark canvas
    expect(within(accent).getByText(/does not reach 3:1/)).toBeInTheDocument();
    await type(within(accent).getByLabelText('Custom colour'), '#ff7a3d');
    expect(within(accent).queryByText(/does not reach 3:1/)).toBeNull();
  });

  it('previews as you choose, and puts the look back on Cancel', async () => {
    await mount();
    expect(preview.look).toBeNull();
    await fireEvent.click(within(fieldset('Accent')).getByRole('radio', { name: 'Teal' }));
    await fireEvent.click(within(fieldset('Density')).getByRole('radio', { name: 'Compact' }));
    expect(preview.look).toMatchObject({ accent: 'teal', density: 'compact', theme: 'dark' });
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(preview.look).toBeNull();
    expect(within(fieldset('Accent')).getByRole('radio', { name: 'Cobalt' })).toBeChecked();
    expect(save()).toBeDisabled();
  });

  it('puts the look back when the screen goes away', async () => {
    const view = await mount();
    await fireEvent.click(within(fieldset('Accent')).getByRole('radio', { name: 'Teal' }));
    expect(preview.look).not.toBeNull();
    view.unmount();
    expect(preview.look).toBeNull();
  });
});

// ----- Archive, storage -----

describe('archive and storage', () => {
  it('asks for days only in auto mode', async () => {
    await mount();
    expect(screen.queryByLabelText('Archive after (days)')).toBeNull();
    await fireEvent.click(within(fieldset('Archive mode')).getByRole('radio', { name: 'Auto' }));
    await type(screen.getByLabelText('Archive after (days)'), '30');
    await fireEvent.click(save());
    await waitFor(() => expect(put()).toHaveBeenCalled());
    expect(sent().settings.archive).toEqual({ mode: 'auto', after_days: 30 });
  });

  it('shows the prefix, path and type as read-only', async () => {
    await mount();
    const prefix = screen.getByLabelText('Prefix');
    expect(prefix).toHaveValue('T');
    expect(prefix).toHaveAttribute('readonly');
    expect(screen.getByText('Fixed when the project is created.')).toBeInTheDocument();
    expect(screen.getByText('/data/home.md')).toBeInTheDocument();
    expect(screen.getByText('markdown')).toBeInTheDocument();
  });
});

// ----- Saving, conflicts, leaving -----

describe('saving', () => {
  it('has Save off until something changes, then says so, then says Saved', async () => {
    await mount();
    expect(save()).toBeDisabled();
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    expect(save()).toBeEnabled();
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument();
    await fireEvent.click(save());
    expect(await screen.findByText('Saved')).toBeInTheDocument();
    expect(save()).toBeDisabled();
    expect(within(row('Column', 1)).getByLabelText('Name')).toHaveValue('Backlog');
  });

  it('keeps the form on a 409, says so, and lets the owner choose to reload', async () => {
    await mount();
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    // Someone else changes the settings on disk, and the save meets a 409.
    put().mockImplementationOnce(async () => {
      srv.state.settings = { ...baseSettings(), name: 'Home renamed elsewhere', priorities: ['P0', 'P1', 'P2'] };
      srv.state.hash = 'sh-elsewhere';
      throw new ApiError(409, 'conflict', 'the settings changed', { current: srv.response().board });
    });
    await fireEvent.click(save());
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('These settings changed elsewhere. Reload them?');
    expect(within(row('Column', 1)).getByLabelText('Name')).toHaveValue('Backlog'); // kept
    expect(save()).toBeDisabled(); // would overwrite what came from elsewhere
    expect(screen.queryByRole('group', { name: 'Priority 3' })).toBeNull();

    await fireEvent.click(within(alert).getByRole('button', { name: 'Reload' }));
    expect(within(row('Column', 1)).getByLabelText('Name')).toHaveValue('Todo');
    expect(screen.getByRole('group', { name: 'Priority 3' })).toBeInTheDocument();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('notices a change from elsewhere before saving, once the form is edited, and follows it while clean', async () => {
    await mount();
    srv.state.settings = { ...baseSettings(), priorities: ['P0', 'P1', 'P2'] };
    srv.state.hash = 'sh-elsewhere';
    await store!.refresh();
    await waitFor(() => expect(screen.getByRole('group', { name: 'Priority 3' })).toBeInTheDocument()); // clean: followed

    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    srv.state.settings = { ...baseSettings(), priorities: ['P0'] };
    srv.state.hash = 'sh-again';
    await store!.refresh();
    expect(await screen.findByRole('alert')).toHaveTextContent('These settings changed elsewhere. Reload them?');
    expect(put()).not.toHaveBeenCalled();
    expect(within(row('Column', 1)).getByLabelText('Name')).toHaveValue('Backlog');
  });

  it('asks before leaving with unsaved changes, and stays when told to', async () => {
    await mount();
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    navigate('/p/Home');
    const dialog = await screen.findByRole('alertdialog', { name: 'Leave without saving?' });
    expect(route.name).toBe('projectSettings');
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Stay' }));
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(route.name).toBe('projectSettings');

    navigate('/p/Home');
    await fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Leave' }));
    expect(route.name).toBe('board');
  });

  it('leaves without asking when nothing is changed', async () => {
    await mount();
    navigate('/p/Home');
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(route.name).toBe('board');
  });

  it('warns on a page unload while there are unsaved changes, and not otherwise', async () => {
    await mount();
    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);
    await type(within(row('Column', 1)).getByLabelText('Name'), 'Backlog');
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });
});

// ----- App settings -----

describe('GlobalSettings', () => {
  const client = () => ({
    settings: vi.fn(async () => globalOf()),
    putSettings: vi.fn(async (c: Parameters<typeof import('../src/lib/api').api.putSettings>[0]) =>
      globalOf({ ...c, appearance: { ...globalOf().appearance, ...c.appearance }, restart_required: c.port !== undefined && c.port !== 4747 }),
    ),
  });

  async function mountGlobal(c = client()) {
    const onSaved = vi.fn();
    render(GlobalSettings, { props: { client: c, onSaved } });
    await screen.findByLabelText('Port');
    return { c, onSaved };
  }

  it('loads the values and saves a change', async () => {
    const { c, onSaved } = await mountGlobal();
    expect(screen.getByLabelText('Port')).toHaveValue('4747');
    expect(within(fieldset('Theme')).getByRole('radio', { name: 'Dark' })).toBeChecked();
    expect(within(fieldset('TLS')).getByRole('radio', { name: 'Auto' })).toBeChecked();
    await fireEvent.click(within(fieldset('Accent')).getByRole('radio', { name: 'Sky' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Open the browser when Waypoint starts' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(c.putSettings).toHaveBeenCalledTimes(1));
    expect(c.putSettings.mock.calls[0]![0]).toMatchObject({ port: 4747, open_browser: false, tls: 'auto', appearance: { theme: 'dark', accent: 'sky' } });
    expect(await screen.findByText('Saved')).toBeInTheDocument();
    expect(onSaved).toHaveBeenCalled();
    expect(screen.queryByText('Restart "waypoint serve" to apply.')).toBeNull();
  });

  it('says to restart after a port change, and as soon as the port or TLS is edited', async () => {
    const { c } = await mountGlobal();
    await fireEvent.click(within(fieldset('TLS')).getByRole('radio', { name: 'Off' }));
    expect(screen.getByText('Restart "waypoint serve" to apply.')).toBeInTheDocument();
    await fireEvent.click(within(fieldset('TLS')).getByRole('radio', { name: 'Auto' }));
    expect(screen.queryByText('Restart "waypoint serve" to apply.')).toBeNull();
    await type(screen.getByLabelText('Port'), '5000');
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(c.putSettings).toHaveBeenCalled());
    expect(c.putSettings.mock.calls[0]![0]).toMatchObject({ port: 5000 });
    await waitFor(() => expect(screen.getByText('Restart "waypoint serve" to apply.')).toBeInTheDocument());
  });

  it('shows the server refusal of port 443 under the port field', async () => {
    const c = client();
    c.putSettings.mockRejectedValueOnce(new ApiError(422, 'invalid', 'invalid: port 443 is not supported: browsers leave it out of the Host header, which Waypoint checks'));
    await mountGlobal(c);
    const port = screen.getByLabelText('Port');
    await type(port, '443');
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    const error = await screen.findByText(/port 443 is not supported/);
    expect(error.id).not.toBe('');
    expect(port).toHaveAttribute('aria-describedby', expect.stringContaining(error.id));
    expect(port).toHaveAttribute('aria-invalid', 'true');
    await waitFor(() => expect(port).toHaveFocus());
  });

  it('checks the port on the client', async () => {
    const { c } = await mountGlobal();
    const port = screen.getByLabelText('Port');
    await type(port, '70000');
    expect(screen.getByText('Use a whole number from 1 to 65535')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(c.putSettings).not.toHaveBeenCalled();
    await waitFor(() => expect(port).toHaveFocus());
  });

  it('explains HTTPS in one sentence and offers no control that runs waypoint trust', async () => {
    await mountGlobal();
    expect(screen.getByText('Browsers need HTTPS to use HTTP/2, and running "waypoint trust" once enables it.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /trust/i })).toBeNull();
  });

  it('shows Built-in for values the app has not set, and has no em or en dash in its text', async () => {
    await mountGlobal();
    expect(within(fieldset('Density')).getByText('Built-in')).toBeInTheDocument();
    expect(within(fieldset('Theme')).queryByText('Built-in')).toBeNull();
    expect(document.body.textContent).not.toMatch(/[–—]/);
  });
});

// ----- New project -----

describe('NewProjectDialog', () => {
  const created = (name: string): ProjectInfo => ({ name, storage: 'markdown', path: `/data/${name}.md`, accent: 'orange' });

  function open(tab: 'create' | 'open' = 'create', createProject = vi.fn(async (p: { name: string }) => created(p.name))) {
    const onClose = vi.fn();
    const onCreated = vi.fn();
    render(NewProjectDialog, { props: { client: { createProject }, initialTab: tab, onClose, onCreated } });
    return { createProject, onClose, onCreated };
  }

  const field = (label: string) => screen.getByLabelText(label) as HTMLInputElement;
  const submit = (name: string) => fireEvent.click(screen.getByRole('button', { name }));

  it('has Create and Open tabs, and starts on the one asked for', async () => {
    open('open');
    expect(screen.getByRole('dialog', { name: 'New project' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Open existing file' })).toHaveAttribute('aria-selected', 'true');
    expect(field('Path')).toHaveFocus();
    await fireEvent.click(screen.getByRole('tab', { name: 'Create' }));
    expect(screen.getByRole('tab', { name: 'Create' })).toHaveAttribute('aria-selected', 'true');
    expect(field('Prefix')).toHaveValue('T');
    expect(screen.getByText("Leave empty to keep it in Waypoint's folder")).toBeInTheDocument();
  });

  it('creates with a name, a prefix and no location, and goes to the name the registry gave', async () => {
    const createProject = vi.fn(async () => created('Kitchen (2)'));
    const { onCreated } = open('create', createProject);
    await type(field('Name'), 'Kitchen');
    await type(field('Prefix'), 'K');
    await submit('Create project');
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('Kitchen (2)'));
    expect(createProject).toHaveBeenCalledWith({ name: 'Kitchen', prefix: 'K' });
  });

  it('sends the location when one is given', async () => {
    const { createProject } = open();
    await type(field('Name'), 'Kitchen');
    await type(field('Location'), '/home/me/kitchen.md');
    await submit('Create project');
    await waitFor(() => expect(createProject).toHaveBeenCalled());
    expect(createProject).toHaveBeenCalledWith({ name: 'Kitchen', path: '/home/me/kitchen.md', prefix: 'T' });
  });

  it('refuses a prefix that is not letters and numbers starting with a letter', async () => {
    const { createProject } = open();
    await type(field('Name'), 'Kitchen');
    await type(field('Prefix'), '9x');
    expect(screen.getByText('Use letters and numbers, starting with a letter')).toBeInTheDocument();
    await submit('Create project');
    expect(createProject).not.toHaveBeenCalled();
    expect(field('Prefix')).toHaveFocus();
    expect(field('Prefix')).toHaveAttribute('aria-invalid', 'true');
  });

  it('wants a name, and a path that ends in .md or .markdown when one is given', async () => {
    const { createProject } = open();
    await submit('Create project');
    expect(field('Name')).toHaveFocus();
    expect(screen.getByText('Give the project a name')).toBeInTheDocument();
    await type(field('Name'), 'Kitchen');
    await type(field('Location'), '/home/me/kitchen.txt');
    await submit('Create project');
    expect(screen.getByText('The path must end in .md or .markdown')).toBeInTheDocument();
    expect(field('Location')).toHaveFocus();
    expect(createProject).not.toHaveBeenCalled();
    await type(field('Location'), '/home/me/kitchen.MARKDOWN');
    await submit('Create project');
    await waitFor(() => expect(createProject).toHaveBeenCalled());
  });

  it('shows the API error under the path field and keeps what was typed', async () => {
    const createProject = vi.fn(async (): Promise<ProjectInfo> => {
      throw new ApiError(422, 'invalid', 'invalid: /data/x.md is not a Waypoint board (missing front matter)');
    });
    const { onCreated } = open('open', createProject);
    await type(field('Path'), '/data/x.md');
    await submit('Open file');
    const error = await screen.findByText(/not a Waypoint board \(missing front matter\)/);
    expect(field('Path')).toHaveAttribute('aria-describedby', expect.stringContaining(error.id));
    expect(field('Path')).toHaveAttribute('aria-invalid', 'true');
    expect(field('Path')).toHaveValue('/data/x.md');
    expect(onCreated).not.toHaveBeenCalled();
    await waitFor(() => expect(field('Path')).toHaveFocus());
  });

  it('opens an existing file under a name taken from the file, without a prefix', async () => {
    const { createProject, onCreated } = open('open');
    await type(field('Path'), '/home/me/Home reno.md');
    expect(field('Name')).toHaveValue('Home reno');
    await submit('Open file');
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('Home reno'));
    expect(createProject).toHaveBeenCalledWith({ name: 'Home reno', path: '/home/me/Home reno.md', existing: true });
  });

  it('puts a name error under the name field', async () => {
    const createProject = vi.fn(async (): Promise<ProjectInfo> => {
      throw new ApiError(422, 'invalid', 'invalid: project "Kitchen" already exists');
    });
    open('create', createProject);
    await type(field('Name'), 'Kitchen');
    await submit('Create project');
    const error = await screen.findByText(/project "Kitchen" already exists/);
    expect(field('Name')).toHaveAttribute('aria-describedby', expect.stringContaining(error.id));
  });

  it('closes on Escape and on Cancel', async () => {
    const { onClose } = open();
    await fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('moves between the tabs with the arrow keys', async () => {
    open();
    const create = screen.getByRole('tab', { name: 'Create' });
    create.focus();
    await fireEvent.keyDown(create, { key: 'ArrowRight' });
    expect(screen.getByRole('tab', { name: 'Open existing file' })).toHaveFocus();
    expect(screen.getByRole('tab', { name: 'Open existing file' })).toHaveAttribute('aria-selected', 'true');
  });
});
