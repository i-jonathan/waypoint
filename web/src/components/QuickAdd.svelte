<script lang="ts">
  import X from 'phosphor-svelte/lib/X';
  import { onMount } from 'svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  interface Props {
    column: string;
    /** Makes one card. It resolves when the card exists, and rejects with the reason it could not be made. */
    onCreate: (title: string) => Promise<unknown>;
    onClose: () => void;
  }

  let { column, onCreate, onClose }: Props = $props();

  /** A paste of more than this many lines gets a longer warning. */
  const MANY = 50;

  let input: HTMLInputElement | undefined = $state();
  let value = $state('');
  let error = $state('');
  let status = $state('');
  /** The lines of a paste, waiting for a yes. */
  let pending = $state.raw<string[] | null>(null);
  let working = $state(false);

  onMount(() => input?.focus());

  const reason = (e: unknown) => (e instanceof Error && e.message !== '' ? e.message : 'Something went wrong');

  function add() {
    const title = value.trim();
    if (title === '') return;
    error = '';
    status = '';
    value = ''; // ready for the next one at once; the store's queue keeps them in order
    onCreate(title).catch((e) => {
      error = `Could not add "${title}": ${reason(e)}`;
      if (value === '') value = title;
    });
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.isComposing) {
      e.preventDefault();
      add();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    }
  }

  function onPaste(e: ClipboardEvent) {
    const text = e.clipboardData?.getData('text') ?? '';
    if (!/[\r\n]/.test(text)) return; // a single line goes in as typed
    e.preventDefault();
    const lines = text.split(/\r\n|\r|\n/).map((l) => l.trim()).filter((l) => l !== '');
    if (lines.length === 0) return;
    if (lines.length === 1) {
      input?.setRangeText(lines[0]!, input.selectionStart ?? value.length, input.selectionEnd ?? value.length, 'end');
      value = input?.value ?? lines[0]!;
      return;
    }
    pending = lines;
  }

  /** One at a time, so the order holds and the first failure stops the rest. */
  async function createAll() {
    const lines = pending;
    pending = null;
    if (!lines) return;
    working = true;
    error = '';
    status = '';
    let made = 0;
    for (const title of lines) {
      try {
        await onCreate(title);
        made++;
      } catch (e) {
        error = `Created ${made} of ${lines.length} cards. Stopped at "${title}": ${reason(e)}`;
        break;
      }
    }
    working = false;
    if (made === lines.length) status = `Created ${made} cards`;
    input?.focus();
  }
</script>

<div class="qa">
  <div class="row">
    <input
      bind:this={input}
      bind:value
      type="text"
      autocomplete="off"
      spellcheck="false"
      placeholder="Card title, Enter to add"
      aria-label="New card title in {column}"
      disabled={working}
      onkeydown={onKeydown}
      onpaste={onPaste}
    />
    <button class="x" aria-label="Close quick add" title="Close quick add" onclick={onClose}><X size={14} weight="bold" aria-hidden="true" /></button>
  </div>
  {#if error}<p class="err" role="alert">{error}</p>{/if}
  <p class="ok" role="status">{status}</p>
</div>

{#if pending}
  <ConfirmDialog
    title="Create {pending.length} cards?"
    message={pending.length > MANY
      ? `That is a lot: more than ${MANY}. They are added to ${column} one after another, and it stops at the first one that fails.`
      : `One card for each line, added to ${column} in order.`}
    confirmLabel="Create {pending.length} cards"
    onConfirm={createAll}
    onCancel={() => {
      pending = null;
      input?.focus();
    }}
  />
{/if}

<style>
  .qa {
    width: calc(100% - 3px);
  }

  .row {
    display: flex;
    gap: 4px;
    align-items: center;
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 8px 11px;
    border: 1.5px solid var(--acc);
    border-radius: var(--r-card);
    background: var(--card);
    font-size: 13px;
  }

  input::placeholder {
    color: var(--muted);
  }

  .x {
    flex: none;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border: 0;
    border-radius: var(--r-control);
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .x:hover {
    color: var(--text);
  }

  .err,
  .ok {
    margin: 6px 2px 0;
    font-size: 12px;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }

  .err {
    color: var(--red);
  }

  .ok {
    color: var(--muted);
  }

  .ok:empty {
    display: none;
  }
</style>
