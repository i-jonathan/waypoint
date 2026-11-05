// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Board from '../src/components/Board.svelte';
import QuickAdd from '../src/components/QuickAdd.svelte';
import { createBoardStore, type BoardApi, type BoardStore } from '../src/lib/board.svelte';
import { trapFocus } from '../src/lib/trap';
import type { BoardResponse, CardView, Column, Settings } from '../src/lib/types';

afterEach(() => {
  cleanup();
  store?.destroy();
  store = null;
  document.body.innerHTML = '';
});

let store: BoardStore | null = null;

const input = () => screen.getByRole('textbox', { name: /New card title/ }) as HTMLInputElement;

async function paste(el: HTMLElement, text: string) {
  await fireEvent.paste(el, { clipboardData: { getData: () => text } });
}

function mountQuickAdd(onCreate = vi.fn(async (_t: string) => {})) {
  const onClose = vi.fn();
  render(QuickAdd, { props: { column: 'Todo', onCreate, onClose } });
  return { onCreate, onClose };
}

describe('QuickAdd', () => {
  it('creates a card from a trimmed title on Enter, and clears the field for the next one', async () => {
    const { onCreate } = mountQuickAdd();
    expect(input()).toHaveFocus();
    await fireEvent.input(input(), { target: { value: '  Buy paint  ' } });
    await fireEvent.keyDown(input(), { key: 'Enter' });
    expect(onCreate).toHaveBeenCalledWith('Buy paint');
    expect(input().value).toBe('');
  });

  it('ignores Enter on an empty or blank title', async () => {
    const { onCreate } = mountQuickAdd();
    await fireEvent.keyDown(input(), { key: 'Enter' });
    await fireEvent.input(input(), { target: { value: '   ' } });
    await fireEvent.keyDown(input(), { key: 'Enter' });
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('puts the title back and says why when the card could not be made', async () => {
    const { onCreate } = mountQuickAdd(vi.fn(async () => Promise.reject(new Error('WIP limit reached'))));
    await fireEvent.input(input(), { target: { value: 'Too many' } });
    await fireEvent.keyDown(input(), { key: 'Enter' });
    expect(await screen.findByText(/Could not add "Too many": WIP limit reached/)).toBeInTheDocument();
    expect(input().value).toBe('Too many');
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it('closes on Esc', async () => {
    const { onClose } = mountQuickAdd();
    await fireEvent.keyDown(input(), { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('confirms a multi-line paste, then creates one card per non-empty line in order', async () => {
    const { onCreate } = mountQuickAdd();
    await paste(input(), 'a\nb\n\nc');
    const dialog = await screen.findByRole('alertdialog', { name: 'Create 3 cards?' });
    expect(onCreate).not.toHaveBeenCalled();
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Create 3 cards' }));
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(3));
    expect(onCreate.mock.calls.map((c) => c[0])).toEqual(['a', 'b', 'c']);
    expect(await screen.findByText('Created 3 cards')).toBeInTheDocument();
  });

  it('creates nothing when the confirm is cancelled', async () => {
    const { onCreate } = mountQuickAdd();
    await paste(input(), 'a\nb');
    await fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(onCreate).not.toHaveBeenCalled();
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('handles Windows line ends and trims each line', async () => {
    const { onCreate } = mountQuickAdd();
    await paste(input(), '  one \r\n two\r\n');
    await fireEvent.click(await screen.findByRole('button', { name: 'Create 2 cards' }));
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2));
    expect(onCreate.mock.calls.map((c) => c[0])).toEqual(['one', 'two']);
  });

  it('lets a single line with a trailing newline in as plain text, without asking', async () => {
    mountQuickAdd();
    await paste(input(), 'just one\n');
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(input().value).toBe('just one');
  });

  it('asks once, with a warning, for a paste of more than 50 lines', async () => {
    const { onCreate } = mountQuickAdd();
    await paste(input(), Array.from({ length: 51 }, (_, i) => `card ${i}`).join('\n'));
    const dialog = await screen.findByRole('alertdialog', { name: 'Create 51 cards?' });
    expect(dialog).toHaveTextContent('more than 50');
    await fireEvent.click(within(dialog).getByRole('button', { name: 'Create 51 cards' }));
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(51));
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('stops at the first failure and says how many were made', async () => {
    const onCreate = vi.fn(async (t: string) => {
      if (t === 'c') throw new Error('title is too long');
    });
    mountQuickAdd(onCreate);
    await paste(input(), 'a\nb\nc\nd\ne');
    await fireEvent.click(await screen.findByRole('button', { name: 'Create 5 cards' }));
    expect(await screen.findByText('Created 2 of 5 cards. Stopped at "c": title is too long')).toBeInTheDocument();
    expect(onCreate.mock.calls.map((c) => c[0])).toEqual(['a', 'b', 'c']);
  });
});

// ----- On the board -----

const columns: Column[] = [
  { name: 'Todo', wip: 0, moves_to: ['Done'], done: false },
  { name: 'Done', wip: 0, moves_to: ['Todo'], done: true },
];
const settings: Settings = {
  name: 'Home',
  prefix: 'T',
  next_id: 5,
  columns,
  groups: [],
  priorities: ['P0'],
  archive: { mode: 'manual', after_days: 0 },
  appearance: { theme: '', accent: '', density: '', swimlane: '', textures: '' },
};

async function mountBoard(props: { adding: string | null; lane?: 'none' | 'group' }) {
  const created: string[] = [];
  const api = {
    board: vi.fn<BoardApi['board']>(async () => ({
      board: { settings, cards: [], warnings: [], settings_hash: 'sh' },
      appearance: { theme: 'dark', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on', source: {} },
      readonly: false,
    }) as BoardResponse),
    createCard: vi.fn<BoardApi['createCard']>(async (_p, n) => {
      created.push(n.title);
      return { id: `T-${created.length}`, title: n.title, column: n.column ?? 'Todo', group: '', priority: '', depends: [], created: null, done: null, body: '', extra: [], archived: false, blockers: [], next: false, checklist: { done: 0, total: 0 }, hash: 'h' } as CardView;
    }),
    patch: vi.fn<BoardApi['patch']>(),
    move: vi.fn<BoardApi['move']>(),
    check: vi.fn<BoardApi['check']>(),
    deleteCard: vi.fn<BoardApi['deleteCard']>(),
    archiveDone: vi.fn<BoardApi['archiveDone']>(),
    putProjectSettings: vi.fn<BoardApi['putProjectSettings']>(),
  };
  store = createBoardStore(api, 'Home');
  await store.refresh();
  const onCloseAdd = vi.fn();
  render(Board, { props: { store, project: 'Home', lane: props.lane ?? 'none', onAddCard: vi.fn(), adding: props.adding, onCloseAdd } });
  return { api, created, onCloseAdd };
}

describe('quick add on the board', () => {
  it('sits at the foot of the column it is open in, and replaces that column\'s Add card row', async () => {
    await mountBoard({ adding: 'Done' });
    const done = document.querySelector('[data-column="Done"]') as HTMLElement;
    expect(within(done).getByRole('textbox', { name: 'New card title in Done' })).toBeInTheDocument();
    expect(within(done).queryByRole('button', { name: /Add card/ })).toBeNull();
    const todo = document.querySelector('[data-column="Todo"]') as HTMLElement;
    expect(within(todo).getByRole('button', { name: /Add card/ })).toBeInTheDocument();
  });

  it('creates a pasted list through the store, in order, in that column', async () => {
    const { api, created } = await mountBoard({ adding: 'Todo' });
    await paste(input(), 'a\nb\n\nc');
    await fireEvent.click(await screen.findByRole('button', { name: 'Create 3 cards' }));
    await waitFor(() => expect(api.createCard).toHaveBeenCalledTimes(3));
    expect(created).toEqual(['a', 'b', 'c']);
    expect(api.createCard.mock.calls.map((c) => c[1].column)).toEqual(['Todo', 'Todo', 'Todo']);
    await waitFor(() => expect(document.querySelectorAll('[data-card-id]').length).toBe(3));
  });

  it('opens as a bar above the lanes in swimlane mode', async () => {
    await mountBoard({ adding: 'Todo', lane: 'group' });
    expect(screen.getByRole('textbox', { name: 'New card title in Todo' })).toBeInTheDocument();
  });

  it('is not shown when nothing is adding', async () => {
    await mountBoard({ adding: null });
    expect(screen.queryByRole('textbox', { name: /New card title/ })).toBeNull();
  });
});

describe('trapFocus', () => {
  function build() {
    document.body.innerHTML = '<div id="t" tabindex="-1"><button id="a">a</button><button id="b">b</button></div><button id="out">out</button>';
    const node = document.getElementById('t')!;
    const action = trapFocus(node, true);
    return { node, action, a: document.getElementById('a')!, b: document.getElementById('b')! };
  }

  it('wraps Tab from the last control to the first, and Shift+Tab back', async () => {
    const { node, a, b } = build();
    b.focus();
    await fireEvent.keyDown(node, { key: 'Tab' });
    expect(a).toHaveFocus();
    await fireEvent.keyDown(node, { key: 'Tab', shiftKey: true });
    expect(b).toHaveFocus();
  });

  it('does nothing when it is off', async () => {
    const { node, action, b } = build();
    action.update(false);
    b.focus();
    const e = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    node.dispatchEvent(e);
    expect(e.defaultPrevented).toBe(false);
  });
});
