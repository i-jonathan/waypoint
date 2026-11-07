// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../src/lib/api';
import ImportDialog from '../src/components/ImportDialog.svelte';
import type { ImportResult } from '../src/lib/types';

afterEach(cleanup);

const csv = (o: Partial<Extract<ImportResult, { format: 'csv' }>> = {}): ImportResult => ({
  format: 'csv',
  dry_run: true,
  created: ['T-4', 'T-5'],
  updated: ['T-1'],
  new_columns: [],
  new_groups: [],
  ...o,
});
const json = (o: Partial<Extract<ImportResult, { format: 'json' }>> = {}): ImportResult => ({ format: 'json', dry_run: true, cards: 3, archived: 2, ...o });

function open(name: string, importFile: ReturnType<typeof vi.fn>) {
  const onClose = vi.fn();
  const onDone = vi.fn();
  const file = new File(['x'], name);
  render(ImportDialog, { props: { project: 'Home', file, client: { importFile: importFile as never }, onClose, onDone } });
  return { file, onClose, onDone };
}

const dialog = () => screen.getByRole('alertdialog');
const confirm = (name: string | RegExp) => within(dialog()).getByRole('button', { name }) as HTMLButtonElement;

describe('ImportDialog with a CSV file', () => {
  it('checks the file first, says what it would do, and imports on confirm', async () => {
    const importFile = vi.fn(async (_p: string, _f: Blob, o: { dryRun?: boolean }) => (o.dryRun ? csv() : csv({ dry_run: false })));
    const { file, onDone } = open('cards.csv', importFile);

    expect(await screen.findByText('Adds 2 cards and updates 1 card.')).toBeInTheDocument();
    expect(importFile).toHaveBeenCalledWith('Home', file, { format: 'csv', create: true, dryRun: true });
    expect(screen.queryByText(/Adds columns/)).toBeNull();

    await fireEvent.click(confirm('Import'));
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(csv({ dry_run: false })));
    expect(importFile).toHaveBeenLastCalledWith('Home', file, { format: 'csv', create: true });
  });

  it('names the columns and groups it would add, and asks before creating them', async () => {
    const importFile = vi.fn(async () => csv({ new_columns: ['Review'], new_groups: ['Ops', 'Legal'] }));
    open('cards.csv', importFile);
    expect(await screen.findByText('Adds columns: Review')).toBeInTheDocument();
    expect(screen.getByText('Adds groups: Ops, Legal')).toBeInTheDocument();
    expect(confirm('Create and import')).toBeEnabled();
  });

  it('keeps Import off while it checks, and shows a refusal with the line', async () => {
    let fail!: (e: Error) => void;
    const importFile = vi.fn(() => new Promise<ImportResult>((_, reject) => (fail = reject)));
    open('cards.csv', importFile);
    expect(confirm('Import')).toBeDisabled();
    expect(screen.getByText('Checking the file')).toBeInTheDocument();

    fail(new ApiError(422, 'invalid', 'invalid: line 3: the title is empty'));
    expect(await screen.findByRole('alert')).toHaveTextContent('line 3: the title is empty');
    expect(confirm('Import')).toBeDisabled();
    expect(importFile).toHaveBeenCalledTimes(1);
  });

  it('shows a failure of the real import and stays open', async () => {
    const importFile = vi.fn(async (_p: string, _f: Blob, o: { dryRun?: boolean }) => {
      if (o.dryRun) return csv();
      throw new ApiError(409, 'conflict', 'the board changed while importing');
    });
    const { onDone, onClose } = open('cards.csv', importFile);
    await screen.findByText('Adds 2 cards and updates 1 card.');
    await fireEvent.click(confirm('Import'));
    expect(await screen.findByRole('alert')).toHaveTextContent('the board changed while importing');
    expect(onDone).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('cancels', async () => {
    const { onClose } = open('cards.csv', vi.fn(async () => csv()));
    await screen.findByText('Adds 2 cards and updates 1 card.');
    await fireEvent.click(confirm('Cancel'));
    expect(onClose).toHaveBeenCalled();
  });
});

describe('ImportDialog with a JSON export', () => {
  it('says what the file holds and imports into an empty project', async () => {
    const importFile = vi.fn(async (_p: string, _f: Blob, o: { dryRun?: boolean }) => json({ dry_run: !!o.dryRun }));
    const { file, onDone } = open('backup.json', importFile);
    expect(await screen.findByText('3 cards and 2 archived cards.')).toBeInTheDocument();
    expect(importFile).toHaveBeenCalledWith('Home', file, { format: 'json', dryRun: true });
    expect(screen.getByLabelText('Replace everything in this project')).not.toBeChecked();
    await fireEvent.click(confirm('Import'));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(importFile).toHaveBeenLastCalledWith('Home', file, { format: 'json' });
  });

  it('explains a project that already has cards, and checks again when replacing is chosen', async () => {
    const importFile = vi.fn(async (_p: string, _f: Blob, o: { dryRun?: boolean; replace?: boolean }) => {
      if (!o.replace) throw new ApiError(422, 'invalid', 'invalid: this project already has cards; import into an empty project, or use replace to swap everything');
      return json({ dry_run: !!o.dryRun });
    });
    const { file, onDone } = open('backup.json', importFile);
    expect(await screen.findByRole('alert')).toHaveTextContent('already has cards');
    expect(confirm('Import')).toBeDisabled();

    await fireEvent.click(screen.getByLabelText('Replace everything in this project'));
    expect(await screen.findByText('3 cards and 2 archived cards.')).toBeInTheDocument();
    expect(importFile).toHaveBeenLastCalledWith('Home', file, { format: 'json', replace: true, dryRun: true });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByText(/replaces all the cards and settings/i)).toBeInTheDocument();

    await fireEvent.click(confirm('Replace and import'));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(importFile).toHaveBeenLastCalledWith('Home', file, { format: 'json', replace: true });
  });
});

describe('ImportDialog with another kind of file', () => {
  it('says what it takes and sends nothing', async () => {
    const importFile = vi.fn();
    open('notes.txt', importFile);
    expect(await screen.findByRole('alert')).toHaveTextContent('Choose a .csv or .json file');
    expect(importFile).not.toHaveBeenCalled();
    expect(confirm('Import')).toBeDisabled();
  });
});
