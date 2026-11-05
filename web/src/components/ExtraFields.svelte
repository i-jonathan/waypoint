<script lang="ts">
  import Plus from 'phosphor-svelte/lib/Plus';
  import X from 'phosphor-svelte/lib/X';
  import { tick } from 'svelte';
  import { keyProblem } from '../lib/draft';
  import type { ExtraField } from '../lib/types';

  interface Props {
    rows: readonly ExtraField[];
    disabled?: boolean;
    onChange: (next: ExtraField[]) => void;
  }

  let { rows, disabled = false, onChange }: Props = $props();

  let list: HTMLElement | undefined = $state();
  const uid = Math.random().toString(36).slice(2, 8);

  function problemOf(i: number): string | null {
    const key = rows[i]!.key.trim();
    if (key === '') return rows[i]!.value !== '' ? 'Give the value a name' : null;
    const bad = keyProblem(key);
    if (bad) return bad;
    return rows.findIndex((r) => r.key.trim() === key) !== i ? `${key} appears twice` : null;
  }

  const set = (i: number, patch: Partial<ExtraField>) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  async function add() {
    onChange([...rows, { key: '', value: '' }]);
    await tick();
    list?.querySelectorAll<HTMLInputElement>('input[data-key]').item(rows.length)?.focus();
  }
</script>

<div class="extra" bind:this={list}>
  {#each rows as row, i (i)}
    {@const bad = problemOf(i)}
    <div class="row">
      <input
        class="k mono"
        data-key
        value={row.key}
        readonly={disabled}
        spellcheck="false"
        autocomplete="off"
        placeholder="name"
        aria-label="Field name {i + 1}"
        aria-invalid={bad !== null}
        aria-describedby={bad ? `ex-${uid}-${i}` : undefined}
        oninput={(e) => set(i, { key: e.currentTarget.value })}
      />
      <input
        class="v"
        value={row.value}
        readonly={disabled}
        spellcheck="false"
        autocomplete="off"
        placeholder="value"
        aria-label="Field value {i + 1}"
        oninput={(e) => set(i, { value: e.currentTarget.value })}
      />
      {#if !disabled}
        <button class="rm" aria-label="Remove field {row.key || i + 1}" title="Remove field" onclick={() => onChange(rows.filter((_, j) => j !== i))}>
          <X size={13} weight="bold" aria-hidden="true" />
        </button>
      {/if}
      {#if bad}<p class="bad" id="ex-{uid}-{i}">{bad}</p>{/if}
    </div>
  {/each}
  {#if !disabled}
    <button class="add" onclick={add}><Plus size={12} weight="bold" aria-hidden="true" />Add field</button>
  {/if}
</div>

<style>
  .extra {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: flex-start;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 2fr) auto;
    gap: 6px;
    width: 100%;
  }

  input {
    min-width: 0;
    padding: 6px 9px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: transparent;
    font-size: 13px;
  }

  input[aria-invalid='true'] {
    border-color: var(--red);
  }

  .k {
    font-size: 12px;
  }

  .rm {
    display: grid;
    place-items: center;
    width: 28px;
    border: 0;
    border-radius: var(--r-control);
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .rm:hover {
    color: var(--text);
  }

  .bad {
    grid-column: 1 / -1;
    margin: 0;
    color: var(--red);
    font-size: 12px;
  }

  .add {
    display: inline-flex;
    gap: 5px;
    align-items: center;
    padding: 5px 10px;
    border: 1px dashed var(--line);
    border-radius: var(--r-control);
    background: none;
    color: var(--muted);
    font-size: 12px;
    cursor: pointer;
  }

  .add:hover {
    color: var(--text);
    border-color: var(--muted);
  }
</style>
