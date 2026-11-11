<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import type { ImportFormat, ImportOptions, ImportResult } from '../lib/types';
  import ConfirmDialog from './ConfirmDialog.svelte';

  interface Props {
    project: string;
    file: File;
    client?: Pick<typeof api, 'importFile'>;
    onClose: () => void;
    /** Called with what the import did, once it is done. */
    onDone: (result: ImportResult) => void;
  }

  let { project, file, client = api, onClose, onDone }: Props = $props();

  const format = $derived<ImportFormat | null>(/\.csv$/i.test(file.name) ? 'csv' : /\.json$/i.test(file.name) ? 'json' : null);

  let replace = $state(false);
  let status = $state<'checking' | 'ready' | 'working' | 'failed'>('checking');
  let result = $state.raw<ImportResult | null>(null);
  let message = $state('');
  /** Only the newest check may answer, when the choice changes while one is running. */
  let latest = 0;

  /** What to ask the server. A CSV import always allows new columns and groups, because the
   *  check below says which they are and the owner confirms before anything is made. */
  function options(dryRun: boolean): ImportOptions {
    const o: ImportOptions = { format: format! };
    if (format === 'csv') o.create = true;
    if (format === 'json' && replace) o.replace = true;
    if (dryRun) o.dryRun = true;
    return o;
  }

  const describe = (e: unknown) => (e instanceof Error && e.message !== '' ? e.message : 'Something went wrong');

  async function check() {
    if (format === null) {
      status = 'failed';
      message = 'Choose a .csv or .json file';
      return;
    }
    const mine = ++latest;
    status = 'checking';
    message = '';
    result = null;
    try {
      const r = await client.importFile(project, file, options(true));
      if (mine !== latest) return;
      result = r;
      status = 'ready';
    } catch (e) {
      if (mine !== latest) return;
      message = describe(e);
      status = 'failed';
    }
  }

  async function run() {
    status = 'working';
    try {
      onDone(await client.importFile(project, file, options(false)));
    } catch (e) {
      message = describe(e);
      status = 'failed';
    }
  }

  onMount(() => void check());

  const cards = (n: number) => `${n} ${n === 1 ? 'card' : 'cards'}`;
  const archived = (n: number) => `${n} archived ${n === 1 ? 'card' : 'cards'}`;

  const adds = $derived(result?.format === 'csv' && (result.new_columns.length > 0 || result.new_groups.length > 0));
  const confirmLabel = $derived(adds ? 'Create and import' : format === 'json' && replace ? 'Replace and import' : 'Import');
</script>

<ConfirmDialog
  title="Import {file.name}"
  {confirmLabel}
  confirmDisabled={status !== 'ready'}
  danger={format === 'json' && replace}
  onConfirm={() => void run()}
  onCancel={onClose}
>
  {#if status === 'checking' || status === 'working'}
    <p class="line" role="status">{status === 'checking' ? 'Checking the file' : 'Importing'}</p>
  {:else if result?.format === 'csv'}
    <p class="line">Adds {cards(result.created.length)} and updates {cards(result.updated.length)}.</p>
    {#if result.new_columns.length > 0}<p class="line">Adds columns: {result.new_columns.join(', ')}</p>{/if}
    {#if result.new_groups.length > 0}<p class="line">Adds groups: {result.new_groups.join(', ')}</p>{/if}
  {:else if result?.format === 'json'}
    <p class="line">{cards(result.cards)} and {archived(result.archived)}.</p>
    {#if replace}<p class="line">This replaces all the cards and settings in this project.</p>{/if}
  {/if}

  {#if status === 'failed'}
    <div class="sf-error" role="alert"><p>{message}</p></div>
  {/if}

  {#if format === 'json'}
    <label class="sf-check">
      <input type="checkbox" checked={replace} disabled={status === 'working'} onchange={(e) => { replace = e.currentTarget.checked; void check(); }} />
      <span>Replace everything in this project</span>
    </label>
  {/if}
</ConfirmDialog>

<style>
  .line {
    margin: 0 0 6px;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .sf-check {
    margin-top: 8px;
  }
</style>
