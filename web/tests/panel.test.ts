// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import CardPanel from '../src/components/CardPanel.svelte';
import { ApiError } from '../src/lib/api';
import { createBoardStore, type BoardApi, type BoardStore } from '../src/lib/board.svelte';
import { takeParked } from '../src/lib/draft';
import { toasts } from '../src/lib/events.svelte';
import type { BoardResponse, CardView, Column, Settings } from '../src/lib/types';

const columns: Column[] = [
  { name: 'Todo', wip: 0, moves_to: ['In progress', 'Done'], done: false },
  { name: 'In progress', wip: 0, moves_to: ['Todo', 'Done'], done: false },
  { name: 'Done', wip: 1, moves_to: ['In progress'], done: true },
];

const settings: Settings = {
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
};

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

/** A server that remembers its cards, so tests can change them behind the panel's back. */
function server(initial: CardView[]) {
  const state = { cards: initial, archived: [] as CardView[] };
  const response = (): BoardResponse => ({
    board: { settings, cards: state.cards, warnings: [], settings_hash: 'sh' },
    appearance: { theme: 'dark', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
    readonly: false,
  });
  const api = {
    board: vi.fn<BoardApi['board']>(async () => response()),
    createCard: vi.fn<BoardApi['createCard']>(),
    patch: vi.fn<BoardApi['patch']>(async (_p, id, body, ifMatch) => {
      const c = state.cards.find((x) => x.id === id)!;
      if (ifMatch !== undefined && ifMatch !== c.hash) throw new ApiError(409, 'conflict', 'The card changed', { current: c });
      const next = { ...c, ...(body as Partial<CardView>), hash: `${c.hash}+` };
      state.cards = state.cards.map((x) => (x.id === id ? next : x));
      return next;
    }),
    move: vi.fn<BoardApi['move']>(async (_p, id, column) => {
      const c = state.cards.find((x) => x.id === id)!;
      const next = { ...c, column, hash: `${c.hash}~` };
      state.cards = state.cards.map((x) => (x.id === id ? next : x));
      return next;
    }),
    check: vi.fn<BoardApi['check']>(),
    deleteCard: vi.fn<BoardApi['deleteCard']>(async (_p, id) => {
      state.cards = state.cards.filter((x) => x.id !== id);
    }),
    archiveDone: vi.fn<BoardApi['archiveDone']>(),
    putProjectSettings: vi.fn<BoardApi['putProjectSettings']>(),
  };
  return { state, api, response };
}

let store: BoardStore | null = null;
let srv: ReturnType<typeof server>;
const onClose = vi.fn();

async function mount(cards: CardView[], id: string, opts: { archived?: CardView[]; loadArchive?: () => Promise<CardView[]> } = {}) {
  srv = server(cards);
  srv.state.archived = opts.archived ?? [];
  store = createBoardStore(srv.api, 'Home');
  await store.refresh();
  const loadArchive = opts.loadArchive ?? (async () => srv.state.archived);
  const view = render(CardPanel, { props: { store, project: 'Home', id, onClose, loadArchive } });
  return view;
}

const titleInput = () => screen.getByRole('textbox', { name: 'Title' }) as HTMLInputElement;
const saveButton = () => screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement;

async function type(el: HTMLElement, value: string) {
  await fireEvent.input(el, { target: { value } });
}

async function openEditor() {
  await fireEvent.click(screen.getByRole('button', { name: /^Edit/ }));
  return screen.getByRole('textbox', { name: 'Notes in Markdown' }) as HTMLTextAreaElement;
}

beforeEach(() => {
  onClose.mockReset();
  for (const id of ['T-1', 'T-2', 'T-9']) takeParked('Home', id); // drafts a test left behind
});

afterEach(() => {
  cleanup();
  store?.destroy();
  store = null;
  toasts.clear();
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

describe('saving', () => {
  it('sends the edited title against the hash the draft started from, on Cmd+Enter', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Renamed');
    await fireEvent.keyDown(titleInput(), { key: 'Enter', metaKey: true });
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalledTimes(1));
    expect(srv.api.patch).toHaveBeenCalledWith('Home', 'T-1', { title: 'Renamed' }, 'hash-T-1');
  });

  it('sends only what changed, and Ctrl+Enter works too', async () => {
    await mount([card('T-1', 'Todo', { priority: 'P1' })], 'T-1');
    await fireEvent.change(screen.getByLabelText('Group'), { target: { value: 'Security' } });
    await fireEvent.keyDown(screen.getByLabelText('Group'), { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ group: 'Security' });
  });

  it('saves with the Save button, which is off while nothing is changed', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    expect(saveButton()).toBeDisabled();
    await type(titleInput(), 'Renamed');
    expect(saveButton()).toBeEnabled();
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalledTimes(1));
  });

  it('resets the draft from the answer, which the server normalised, so it is not left dirty', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    srv.api.patch.mockImplementationOnce(async (_p, id) => ({ ...srv.state.cards.find((c) => c.id === id)!, title: 'Fixed', hash: 'h-new' }));
    await type(titleInput(), 'Fixed   ');
    await fireEvent.click(saveButton());
    await waitFor(() => expect(titleInput().value).toBe('Fixed'));
    expect(saveButton()).toBeDisabled();
    expect(screen.getByText('Saved')).toBeInTheDocument();
  });

  it('stops saying Saved as soon as the draft changes again', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'One');
    await fireEvent.click(saveButton());
    expect(await screen.findByText('Saved')).toBeInTheDocument();
    await type(titleInput(), 'Two');
    expect(screen.queryByText('Saved')).toBeNull();
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument();
  });

  it('keeps what was typed while the save was on its way', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    let release!: () => void;
    srv.api.patch.mockImplementationOnce(
      (_p, id) =>
        new Promise((resolve) => {
          release = () => resolve({ ...srv.state.cards.find((c) => c.id === id)!, title: 'One', hash: 'h2' });
        }),
    );
    await type(titleInput(), 'One');
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    await type(titleInput(), 'One more');
    release();
    await waitFor(() => expect(screen.queryByText('Saving')).toBeNull());
    expect(titleInput().value).toBe('One more');
    expect(saveButton()).toBeEnabled();
  });

  it('refuses an empty title and a bad extra field key before sending', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), '   ');
    expect(saveButton()).toBeDisabled();
    expect(screen.getByText('A card needs a title')).toBeInTheDocument();
    await type(titleInput(), 'Ok');
    await fireEvent.click(screen.getByRole('button', { name: /Add field/ }));
    await type(screen.getByLabelText('Field name 1'), 'group');
    await type(screen.getByLabelText('Field value 1'), 'x');
    expect(screen.getAllByText(/group is built in/).length).toBeGreaterThan(0);
    expect(saveButton()).toBeDisabled();
    await type(screen.getByLabelText('Field name 1'), 'Owner');
    expect(screen.getAllByText(/lowercase letters/).length).toBeGreaterThan(0);
    await type(screen.getByLabelText('Field name 1'), 'owner');
    expect(saveButton()).toBeEnabled();
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ title: 'Ok', set: { owner: 'x' } });
  });
});

describe('a draft against changes made elsewhere (Review Focus 2)', () => {
  it('keeps a dirty body through a refresh, takes the other fields, and resolves a 409 with Keep mine', async () => {
    await mount([card('T-1', 'Todo', { body: 'original' })], 'T-1');
    const editor = await openEditor();
    await type(editor, 'my words');

    // Someone else edits the card on disk; the store refreshes.
    srv.state.cards = [card('T-1', 'Todo', { title: 'Their title', body: 'their words', hash: 'hash-new' })];
    await store!.refresh();
    await waitFor(() => expect(titleInput().value).toBe('Their title'));
    expect(editor.value).toBe('my words');

    await fireEvent.click(saveButton());
    const dialog = await screen.findByRole('alertdialog', { name: 'This card changed outside Waypoint' });
    expect(srv.api.patch).toHaveBeenCalledWith('Home', 'T-1', { body: 'my words' }, 'hash-T-1');
    expect(within(within(dialog).getByRole('region', { name: 'Yours' })).getByText('my words')).toBeInTheDocument();
    expect(within(within(dialog).getByRole('region', { name: 'On disk' })).getByText('their words')).toBeInTheDocument();

    await fireEvent.click(within(dialog).getByRole('button', { name: 'Keep mine' }));
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalledTimes(2));
    expect(srv.api.patch).toHaveBeenLastCalledWith('Home', 'T-1', { body: 'my words' }, 'hash-new');
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    expect(srv.state.cards[0]!.title).toBe('Their title'); // what I did not touch is theirs
  });

  it('Use theirs drops the draft for the card on disk', async () => {
    await mount([card('T-1', 'Todo', { body: 'original' })], 'T-1');
    await type(titleInput(), 'Mine');
    srv.state.cards = [card('T-1', 'Todo', { title: 'Theirs', hash: 'hash-new' })];
    await store!.refresh();
    await fireEvent.click(saveButton());
    const dialog = await screen.findByRole('alertdialog');
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Use theirs' }));
    await waitFor(() => expect(titleInput().value).toBe('Theirs'));
    expect(saveButton()).toBeDisabled();
    expect(srv.api.patch).toHaveBeenCalledTimes(1);
  });

  it('follows the server entirely while nothing is dirty', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    srv.state.cards = [card('T-1', 'Todo', { title: 'Changed on disk', hash: 'hash-new' })];
    await store!.refresh();
    await waitFor(() => expect(titleInput().value).toBe('Changed on disk'));
    await type(titleInput(), 'Then mine');
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![3]).toBe('hash-new');
  });

  it('does not take its own move for a change from outside', async () => {
    await mount([card('T-1', 'Todo', { body: 'b' })], 'T-1');
    await type(titleInput(), 'Mine');
    await fireEvent.change(screen.getByLabelText('Column'), { target: { value: 'In progress' } });
    await waitFor(() => expect(srv.api.move).toHaveBeenCalled());
    await waitFor(() => expect((screen.getByLabelText('Column') as HTMLSelectElement).value).toBe('In progress'));
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![3]).toBe('hash-T-1~');
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('does not take its own tick for a change from outside', async () => {
    srv = server([card('T-1', 'Todo', { body: '- [ ] a\n- [ ] b' })]);
    srv.api.check.mockImplementation(async (_p, id) => {
      const next = { ...srv.state.cards[0]!, body: '- [ ] a\n- [x] b', hash: 'hash-ticked', id };
      srv.state.cards = [next];
      return next;
    });
    store = createBoardStore(srv.api, 'Home');
    await store.refresh();
    render(CardPanel, { props: { store, project: 'Home', id: 'T-1', onClose, loadArchive: async () => [] } });
    await type(titleInput(), 'Mine');
    await fireEvent.click(screen.getAllByRole('checkbox')[1]!);
    await waitFor(() => expect(srv.api.check).toHaveBeenCalledWith('Home', 'T-1', 2, 'hash-T-1'));
    await waitFor(() => expect((screen.getAllByRole('checkbox')[1] as HTMLInputElement).checked).toBe(true));
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![3]).toBe('hash-ticked');
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('keeps the original hash when an outside edit changes a dirty card, so saving meets the 409', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Mine');
    srv.state.cards = [card('T-1', 'Todo', { hash: 'hash-new' })];
    await store!.refresh();
    await fireEvent.click(saveButton());
    expect(await screen.findByRole('alertdialog')).toBeInTheDocument();
    expect(srv.api.patch.mock.calls[0]![3]).toBe('hash-T-1');
  });
});

describe('the body', () => {
  it('ticks the 2nd checkbox through the store as item 2', async () => {
    await mount([card('T-1', 'Todo', { body: '- [ ] one\n- [ ] two\n- [ ] three' })], 'T-1');
    srv.api.check.mockResolvedValue(card('T-1', 'Todo'));
    await fireEvent.click(screen.getAllByRole('checkbox')[1]!);
    await waitFor(() => expect(srv.api.check).toHaveBeenCalledTimes(1));
    expect(srv.api.check).toHaveBeenCalledWith('Home', 'T-1', 2, 'hash-T-1');
  });

  it('ticks inside the draft, not on the server, while the notes are unsaved', async () => {
    await mount([card('T-1', 'Todo', { body: '- [ ] one\n- [ ] two' })], 'T-1');
    const editor = await openEditor();
    await type(editor, '- [ ] one\n- [ ] two\n- [ ] new');
    await fireEvent.click(screen.getByRole('button', { name: /^Preview/ }));
    await fireEvent.click(screen.getAllByRole('checkbox')[2]!);
    expect(srv.api.check).not.toHaveBeenCalled();
    expect(screen.getAllByRole('checkbox').map((c) => (c as HTMLInputElement).checked)).toEqual([false, false, true]);
    expect(saveButton()).toBeEnabled();
  });

  it('does not render a script', async () => {
    await mount([card('T-1', 'Todo', { body: 'hello <script>window.__pwn = 1</script> <img src=x onerror="window.__pwn = 2">' })], 'T-1');
    expect(document.querySelector('script')).toBeNull();
    expect(document.querySelector('.md img')).toBeNull();
    expect(document.querySelector('.md')!.textContent).toContain('hello');
    expect((window as unknown as { __pwn?: number }).__pwn).toBeUndefined();
  });

  it('toggles to a Markdown editor and back', async () => {
    await mount([card('T-1', 'Todo', { body: '**bold**' })], 'T-1');
    expect(document.querySelector('.md strong')).not.toBeNull();
    const editor = await openEditor();
    expect(editor.value).toBe('**bold**');
    await fireEvent.click(screen.getByRole('button', { name: /^Preview/ }));
    expect(screen.queryByRole('textbox', { name: 'Notes in Markdown' })).toBeNull();
  });
});

describe('what goes wrong while saving', () => {
  it('keeps the draft and says so when the card is gone (404), with the text one click from the clipboard', async () => {
    const writeText = vi.fn(async () => {});
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    await mount([card('T-1', 'Todo', { body: 'b' })], 'T-1');
    srv.api.patch.mockRejectedValueOnce(new ApiError(404, 'not_found', 'card T-1 not found'));
    await type(titleInput(), 'My precious title');
    await fireEvent.click(saveButton());
    const alert = await screen.findByText(/gone from the board/);
    expect(alert).toBeInTheDocument();
    expect(titleInput().value).toBe('My precious title');
    await fireEvent.click(screen.getByRole('button', { name: /Copy text/ }));
    expect(writeText).toHaveBeenCalledWith('My precious title\n\nb');
    expect(screen.getByRole('button', { name: /Copied/ })).toBeInTheDocument();
  });

  it('keeps a card that was deleted while the panel was open readable, with a notice', async () => {
    await mount([card('T-1', 'Todo', { body: 'b' })], 'T-1');
    await type(titleInput(), 'Unsaved title');
    srv.state.cards = [];
    await store!.refresh();
    expect(await screen.findByText(/gone from the board/)).toBeInTheDocument();
    expect(titleInput().value).toBe('Unsaved title');
    expect(screen.getByRole('button', { name: /Copy text/ })).toBeInTheDocument();
  });

  it('keeps a card that was archived while the panel was open readable and editable', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    srv.state.archived = [card('T-1', 'Done', { archived: true, title: 'Card T-1', hash: 'hash-T-1' })];
    srv.state.cards = [];
    await store!.refresh();
    expect(await screen.findByText('Archived')).toBeInTheDocument();
    expect(screen.queryByText(/gone from the board/)).toBeNull();
    await type(titleInput(), 'Edited in the archive');
    await fireEvent.click(saveButton());
    srv.api.patch.mockImplementationOnce(async () => card('T-1', 'Done', { archived: true, title: 'Edited in the archive', hash: 'h2' }));
  });

  it('keeps the draft on a network failure, shows the error by Save, and Retry sends again', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    srv.api.patch.mockRejectedValueOnce(new ApiError(0, 'network', 'Failed to fetch'));
    await type(titleInput(), 'Kept');
    await fireEvent.click(saveButton());
    expect(await screen.findByText(/Could not reach Waypoint/)).toBeInTheDocument();
    expect(titleInput().value).toBe('Kept');
    expect(saveButton()).toBeEnabled();
    await fireEvent.click(screen.getByRole('button', { name: /Retry/ }));
    await waitFor(() => expect(screen.queryByText(/Could not reach/)).toBeNull());
    expect(srv.api.patch).toHaveBeenCalledTimes(2);
    expect(srv.state.cards[0]!.title).toBe('Kept');
  });

  it("shows the server's own message when it rejects a dependency id", async () => {
    await mount([card('T-1', 'Todo'), card('T-2', 'Todo')], 'T-1');
    srv.api.patch.mockRejectedValueOnce(new ApiError(422, 'invalid', 'invalid: dependency T-2 would make a cycle'));
    const input = screen.getByRole('combobox', { name: 'Add a dependency' });
    await type(input, 'T-2');
    await fireEvent.keyDown(input, { key: 'Enter' });
    await fireEvent.click(saveButton());
    expect(await screen.findByText('invalid: dependency T-2 would make a cycle')).toBeInTheDocument();
    expect(saveButton()).toBeEnabled();
  });
});

describe('autosave on blur', () => {
  it('saves a dirty title when it loses focus', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Blurred');
    await fireEvent.blur(titleInput());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalledTimes(1));
  });

  it('saves a dirty body when the editor loses focus', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    const editor = await openEditor();
    await type(editor, 'typed');
    await fireEvent.blur(editor);
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalledTimes(1));
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ body: 'typed' });
  });

  it('does nothing when nothing is dirty', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await fireEvent.blur(titleInput());
    expect(srv.api.patch).not.toHaveBeenCalled();
  });

  it('does not save again behind an open conflict dialog', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Mine');
    srv.state.cards = [card('T-1', 'Todo', { hash: 'hash-new', title: 'Theirs' })];
    await store!.refresh();
    await fireEvent.blur(titleInput());
    await screen.findByRole('alertdialog');
    expect(srv.api.patch).toHaveBeenCalledTimes(1);
    await fireEvent.blur(titleInput());
    expect(srv.api.patch).toHaveBeenCalledTimes(1);
  });

  it('holds off after the conflict dialog is put aside, until the draft changes', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Mine');
    srv.state.cards = [card('T-1', 'Todo', { hash: 'hash-new', title: 'Theirs' })];
    await store!.refresh();
    await fireEvent.blur(titleInput());
    const dialog = await screen.findByRole('alertdialog');
    await fireEvent.keyDown(dialog, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    await fireEvent.blur(titleInput());
    expect(srv.api.patch).toHaveBeenCalledTimes(1);
    expect(titleInput().value).toBe('Mine');
  });
});

describe('closing', () => {
  it('prompts when the draft is dirty, and Keep editing leaves it', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Unsaved');
    await fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    expect(onClose).not.toHaveBeenCalled();
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(titleInput().value).toBe('Unsaved');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes after Discard', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Unsaved');
    await fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(takeParked('Home', 'T-1')).toBeNull();
  });

  it('closes at once when nothing is dirty, and Esc closes and returns focus to the card', async () => {
    const opener = document.createElement('div');
    opener.tabIndex = 0;
    opener.dataset.cardId = 'T-1';
    document.body.append(opener);
    await mount([card('T-1', 'Todo')], 'T-1');
    await fireEvent.keyDown(titleInput(), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(document.activeElement).toBe(opener));
  });

  it('puts focus on the card even though the route no longer names it by then', async () => {
    const opener = document.createElement('div');
    opener.tabIndex = 0;
    opener.dataset.cardId = 'T-1';
    document.body.append(opener);
    const view = await mount([card('T-1', 'Todo')], 'T-1');
    // The route change that closes the panel also empties the id it was given, before the panel is gone.
    onClose.mockImplementation(() => view.rerender({ id: undefined as unknown as string }));
    await fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(document.activeElement).toBe(opener));
  });

  it('Esc with a dirty draft asks first', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Unsaved');
    await fireEvent.keyDown(titleInput(), { key: 'Escape' });
    expect(await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('warns before the page unloads while dirty, and not otherwise', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);
    await type(titleInput(), 'Unsaved');
    await waitFor(() => {
      const dirty = new Event('beforeunload', { cancelable: true });
      window.dispatchEvent(dirty);
      expect(dirty.defaultPrevented).toBe(true);
    });
  });

  it('keeps an unsaved draft when the panel goes away by other means, and brings it back', async () => {
    const first = await mount([card('T-1', 'Todo')], 'T-1');
    await type(titleInput(), 'Left behind');
    // A Back press changes the route first, so the panel's props read empty while it is taken down.
    await first.rerender({ id: undefined as unknown as string, project: undefined as unknown as string });
    first.unmount();
    render(CardPanel, { props: { store: store!, project: 'Home', id: 'T-1', onClose, loadArchive: async () => [] } });
    await waitFor(() => expect(titleInput().value).toBe('Left behind'));
    expect(screen.getByText(/unsaved changes from earlier are back/)).toBeInTheDocument();
  });
});

describe('opening a card', () => {
  it('opens an archived card from a deep link, readable and editable', async () => {
    await mount([card('T-2', 'Todo')], 'T-9', { archived: [card('T-9', 'Done', { archived: true, title: 'Old job', body: 'was done' })] });
    expect(await screen.findByDisplayValue('Old job')).toBeInTheDocument();
    expect(screen.getByText('Archived')).toBeInTheDocument();
    expect(screen.getByText('was done')).toBeInTheDocument();
    expect((screen.getByLabelText('Column') as HTMLSelectElement).disabled).toBe(true);
    await type(titleInput(), 'Old job, renamed');
    expect(saveButton()).toBeEnabled();
  });

  it('says Card not found for an id nothing has, without crashing', async () => {
    await mount([card('T-2', 'Todo')], 'T-404');
    expect(await screen.findByRole('heading', { name: 'Card not found' })).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: 'Back to the board' }));
    expect(onClose).toHaveBeenCalled();
  });

  it('says so when the archive cannot be searched, and can try again', async () => {
    const load = vi.fn<() => Promise<CardView[]>>().mockRejectedValue(new Error('offline'));
    await mount([card('T-2', 'Todo')], 'T-9', { loadArchive: load });
    expect(await screen.findByRole('heading', { name: 'Could not look for T-9' })).toBeInTheDocument();
    load.mockResolvedValue([card('T-9', 'Done', { archived: true })]);
    await fireEvent.click(screen.getByRole('button', { name: /Try again/ }));
    expect(await screen.findByDisplayValue('Card T-9')).toBeInTheDocument();
  });

  it('shows created and done dates through displayDate, read only', async () => {
    await mount([card('T-1', 'Done', { created: '2026-11-04T00:00:00Z', done: '2026-10-26' })], 'T-1');
    expect(screen.getByText('Created 2026-11-04')).toBeInTheDocument();
    expect(screen.getByText('Done 2026-10-26')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /created/i })).toBeNull();
  });
});

describe('the fields', () => {
  it('limits the column select to the moves allowed, counting the WIP limit', async () => {
    await mount([card('T-1', 'Todo'), card('T-2', 'Done')], 'T-1');
    const options = [...(screen.getByLabelText('Column') as HTMLSelectElement).options].map((o) => o.value);
    // Done holds 1 of 1 already; Todo is where the card is.
    expect(options).toEqual(['Todo', 'In progress']);
  });

  it('keeps a group or priority that settings no longer list, visible and selected, and does not send it', async () => {
    await mount([card('T-1', 'Todo', { group: 'Legacy', priority: 'P9' })], 'T-1');
    const group = screen.getByLabelText('Group') as HTMLSelectElement;
    expect(group.value).toBe('Legacy');
    expect(within(group).getByRole('option', { name: 'Legacy (not in settings)' })).toBeInTheDocument();
    expect((screen.getByLabelText('Priority') as HTMLSelectElement).value).toBe('P9');
    await type(titleInput(), 'Renamed');
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ title: 'Renamed' });
  });

  it('lists No group and None, and clears them with an empty string', async () => {
    await mount([card('T-1', 'Todo', { group: 'Security', priority: 'P0' })], 'T-1');
    await fireEvent.change(screen.getByLabelText('Group'), { target: { value: '' } });
    await fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '' } });
    expect(screen.getByRole('option', { name: 'No group' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'None' })).toBeInTheDocument();
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ group: '', priority: '' });
  });

  it('removes and sets extra fields', async () => {
    await mount([card('T-1', 'Todo', { extra: [{ key: 'owner', value: 'me' }, { key: 'sprint', value: '4' }] })], 'T-1');
    await fireEvent.click(screen.getByRole('button', { name: 'Remove field owner' }));
    await type(screen.getByLabelText('Field value 1'), '5');
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ set: { owner: '', sprint: '5' } });
  });
});

describe('depends', () => {
  const cards = [card('T-1', 'Todo'), card('T-2', 'Todo', { title: 'Fix gutters' }), card('T-3', 'Done', { title: 'Buy ladder' })];

  it('offers ids and titles of the other cards, archived ones too, never itself', async () => {
    await mount(cards, 'T-1', { archived: [card('T-7', 'Done', { archived: true, title: 'Old ladder job' })] });
    const input = screen.getByRole('combobox', { name: 'Add a dependency' });
    await fireEvent.focus(input);
    const list = await screen.findByRole('listbox', { name: 'Cards' });
    await waitFor(() => expect(within(list).getAllByRole('option').length).toBe(3));
    const options = within(list).getAllByRole('option').map((o) => o.textContent!.replace(/\s+/g, ' ').trim());
    expect(options).toContain('T-2 Fix gutters');
    expect(options).toContain('T-3 Buy ladder');
    expect(options).toContain('T-7 Old ladder job archived');
    expect(options.some((o) => o.startsWith('T-1 '))).toBe(false);
  });

  it('filters by title and adds with the keyboard', async () => {
    await mount(cards, 'T-1');
    const input = screen.getByRole('combobox', { name: 'Add a dependency' });
    await type(input, 'ladder');
    expect(within(screen.getByRole('listbox')).getAllByRole('option').map((o) => o.textContent!.trim().split(' ')[0])).toEqual(['T-3']);
    await fireEvent.keyDown(input, { key: 'Enter' });
    const list = screen.getByRole('list', { name: 'Depends on' });
    expect(within(list).getByText('T-3')).toBeInTheDocument();
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ depends: ['T-3'] });
  });

  it('refuses a card depending on itself', async () => {
    await mount(cards, 'T-1');
    const input = screen.getByRole('combobox', { name: 'Add a dependency' });
    await type(input, 't-1');
    await fireEvent.keyDown(input, { key: 'Enter' });
    expect(screen.getByText('T-1 cannot depend on itself')).toBeInTheDocument();
    expect(saveButton()).toBeDisabled();
  });

  it('marks an id no card has', async () => {
    await mount([card('T-1', 'Todo', { depends: ['T-99'] })], 'T-1');
    expect(screen.getByText('(no such card)')).toBeInTheDocument();
  });

  it('removes a token', async () => {
    await mount([card('T-1', 'Todo', { depends: ['T-2'] }), card('T-2', 'Todo')], 'T-1');
    await fireEvent.click(screen.getByRole('button', { name: 'Remove T-2' }));
    await fireEvent.click(saveButton());
    await waitFor(() => expect(srv.api.patch).toHaveBeenCalled());
    expect(srv.api.patch.mock.calls[0]![2]).toEqual({ depends: [] });
  });
});

describe('deleting', () => {
  it('asks with the id and title, then deletes and closes', async () => {
    await mount([card('T-1', 'Todo', { title: 'Mend the fence' })], 'T-1');
    await fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    await fireEvent.click(screen.getByRole('menuitem', { name: /Delete card/ }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Delete T-1?' });
    expect(dialog).toHaveTextContent('Mend the fence');
    expect(srv.api.deleteCard).not.toHaveBeenCalled();
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Delete card' }));
    await waitFor(() => expect(srv.api.deleteCard).toHaveBeenCalledWith('Home', 'T-1', 'hash-T-1'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('does nothing when cancelled', async () => {
    await mount([card('T-1', 'Todo')], 'T-1');
    await fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    await fireEvent.click(screen.getByRole('menuitem', { name: /Delete card/ }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(srv.api.deleteCard).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});
