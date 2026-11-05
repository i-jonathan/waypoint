<script lang="ts">
  import Warning from 'phosphor-svelte/lib/Warning';
  import X from 'phosphor-svelte/lib/X';
  import type { KnownCard } from '../lib/draft';

  interface Props {
    value: readonly string[];
    /** The card being edited, which cannot depend on itself. */
    selfId: string;
    /** Every card on the board, archived ones too when they are known. Null until the board has loaded. */
    known: readonly KnownCard[] | null;
    disabled?: boolean;
    onChange: (next: string[]) => void;
  }

  let { value, selfId, known, disabled = false, onChange }: Props = $props();

  const LIMIT = 8;
  const uid = Math.random().toString(36).slice(2, 8);

  let text = $state('');
  let open = $state(false);
  let active = $state(0);
  let problem = $state('');
  let input: HTMLInputElement | undefined = $state();

  const byId = $derived(new Map((known ?? []).map((k) => [k.id.toLowerCase(), k])));

  const suggestions = $derived.by(() => {
    const q = text.trim().toLowerCase();
    if (!known) return [];
    return known
      .filter((k) => k.id !== selfId && !value.includes(k.id) && (q === '' || k.id.toLowerCase().includes(q) || k.title.toLowerCase().includes(q)))
      .slice(0, LIMIT);
  });

  function add(raw: string) {
    const typed = raw.trim();
    if (typed === '') return;
    const hit = byId.get(typed.toLowerCase());
    const id = hit ? hit.id : typed;
    if (id.toLowerCase() === selfId.toLowerCase()) {
      problem = `${selfId} cannot depend on itself`;
      return;
    }
    problem = '';
    text = '';
    open = false;
    active = 0;
    if (!value.includes(id)) onChange([...value, id]);
  }

  function remove(id: string) {
    problem = '';
    onChange(value.filter((v) => v !== id));
    input?.focus();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      open = true;
      active = suggestions.length === 0 ? 0 : (active + 1) % suggestions.length;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      active = suggestions.length === 0 ? 0 : (active - 1 + suggestions.length) % suggestions.length;
    } else if (e.key === 'Enter' || e.key === ',') {
      if (e.key === 'Enter' && e.isComposing) return;
      // A bare Enter with nothing typed belongs to the panel (it saves with Cmd/Ctrl).
      if (text.trim() === '' && e.key === 'Enter') return;
      e.preventDefault();
      add(open && suggestions[active] ? suggestions[active]!.id : text);
    } else if (e.key === 'Escape' && (open || text !== '')) {
      e.preventDefault();
      e.stopPropagation();
      open = false;
      text = '';
      problem = '';
    } else if (e.key === 'Backspace' && text === '' && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  }
</script>

<div class="dep">
  <ul class="toks" aria-label="Depends on">
    {#each value as id (id)}
      {@const k = byId.get(id.toLowerCase())}
      {@const unknown = known !== null && !k}
      <li class="tok" class:bad={unknown}>
        {#if unknown}<Warning size={12} weight="bold" aria-hidden="true" />{/if}
        <span class="id mono" title={k ? k.title : unknown ? 'No card has this id' : undefined}>{id}</span>
        {#if unknown}<span class="sr-only">(no such card)</span>{/if}
        {#if k?.archived}<span class="tag">archived</span>{/if}
        {#if !disabled}
          <button class="rm" aria-label="Remove {id}" title="Remove {id}" onclick={() => remove(id)}><X size={11} weight="bold" aria-hidden="true" /></button>
        {/if}
      </li>
    {/each}
  </ul>
  {#if !disabled}
    <div class="box">
      <input
        bind:this={input}
        bind:value={text}
        type="text"
        role="combobox"
        aria-label="Add a dependency"
        aria-expanded={open && suggestions.length > 0}
        aria-controls="dep-list-{uid}"
        aria-autocomplete="list"
        aria-activedescendant={open && suggestions[active] ? `dep-${uid}-${active}` : undefined}
        autocomplete="off"
        spellcheck="false"
        placeholder="Card id or title"
        onfocus={() => (open = true)}
        onblur={() => setTimeout(() => (open = false), 120)}
        oninput={() => {
          open = true;
          active = 0;
          problem = '';
        }}
        onkeydown={onKeydown}
      />
      {#if open && suggestions.length > 0}
        <ul class="list" id="dep-list-{uid}" role="listbox" aria-label="Cards">
          {#each suggestions as s, i (s.id)}
            <li
              id="dep-{uid}-{i}"
              role="option"
              aria-selected={i === active}
              class:on={i === active}
              onmousedown={(e) => {
                e.preventDefault();
                add(s.id);
              }}
            >
              <span class="mono">{s.id}</span>
              <span class="t">{s.title}</span>
              {#if s.archived}<span class="tag">archived</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
  <p class="problem" role="status">{problem}</p>
</div>

<style>
  .dep {
    min-width: 0;
  }

  .toks {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .toks:not(:empty) {
    margin-bottom: 6px;
  }

  .tok {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 4px 2px 7px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    font-size: 12px;
  }

  .tok.bad {
    border-style: dashed;
    border-color: var(--red);
    color: var(--red);
  }

  .tag {
    color: var(--muted);
    font-size: 11px;
  }

  .rm {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border: 0;
    border-radius: var(--r-chip);
    background: none;
    cursor: pointer;
  }

  .rm:hover {
    background: var(--line);
  }

  .box {
    position: relative;
  }

  input {
    width: 100%;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: transparent;
    font-size: 13px;
  }

  .list {
    position: absolute;
    z-index: 5;
    left: 0;
    right: 0;
    margin: 4px 0 0;
    padding: 4px;
    list-style: none;
    border: 1.5px solid var(--edge);
    border-radius: var(--r-control);
    background: var(--card);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .list li {
    display: flex;
    gap: 8px;
    align-items: baseline;
    padding: 5px 8px;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
  }

  .list li.on {
    background: var(--head);
    color: var(--headtext);
  }

  .t {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .problem {
    margin: 4px 0 0;
    min-height: 0;
    color: var(--red);
    font-size: 12px;
  }

  .problem:empty {
    display: none;
  }
</style>
