<script lang="ts">
  import ArrowClockwise from 'phosphor-svelte/lib/ArrowClockwise';
  import Archive from 'phosphor-svelte/lib/Archive';
  import Copy from 'phosphor-svelte/lib/Copy';
  import DotsThree from 'phosphor-svelte/lib/DotsThree';
  import Eye from 'phosphor-svelte/lib/Eye';
  import PencilSimple from 'phosphor-svelte/lib/PencilSimple';
  import Trash from 'phosphor-svelte/lib/Trash';
  import WarningCircle from 'phosphor-svelte/lib/WarningCircle';
  import X from 'phosphor-svelte/lib/X';
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import type { BoardStore } from '../lib/board.svelte';
  import { toggleItem } from '../lib/checklist';
  import { displayDate } from '../lib/dates';
  import {
    absorb,
    dirtyOf,
    parkDraft,
    rowsProblem,
    saved,
    startDraft,
    takeParked,
    toPatch,
    type DraftState,
    type Fields,
    type KnownCard,
  } from '../lib/draft';
  import { canDrop, columnIds } from '../lib/moveRules';
  import { trapFocus } from '../lib/trap';
  import type { CardView } from '../lib/types';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import ConflictDialog from './ConflictDialog.svelte';
  import DependsField from './DependsField.svelte';
  import ExtraFields from './ExtraFields.svelte';
  import MarkdownBody from './MarkdownBody.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    id: string;
    /** Closes the panel. The caller goes back to the board. */
    onClose: () => void;
    /** The archived cards of the project. Defaults to the API. */
    loadArchive?: () => Promise<CardView[]>;
  }

  let { store, project, id, onClose, loadArchive = () => api.archive(project) }: Props = $props();

  // The route changes before the panel is taken down, and then the props read empty: what teardown needs is kept.
  const ownId = untrack(() => id);
  const ownProject = untrack(() => project);

  // ----- Where the card comes from -----
  // The whole board, whatever the filter. An archived card is not on it, so it is looked up in the archive.
  const whole = $derived(store.baseline ?? store.view);
  const live = $derived(whole?.cards.find((c) => c.id === id) ?? null);
  const settings = $derived(whole?.settings ?? null);
  const missing = $derived(whole !== null && live === null);
  const locked = $derived(store.readonly);

  let archive = $state.raw<CardView[] | null>(null);
  let archived = $state.raw<CardView | null>(null);
  let lookup = $state<'idle' | 'busy' | 'done' | 'failed'>('idle');
  let lookSeq = 0;
  let lookAgain = $state(0);

  async function fetchArchive(): Promise<CardView[] | null> {
    try {
      const list = await loadArchive();
      archive = list;
      return list;
    } catch {
      return null;
    }
  }

  onMount(() => {
    void fetchArchive(); // for the dependency autocomplete
  });

  $effect(() => {
    void lookAgain;
    if (!missing) {
      untrack(() => {
        archived = null;
        lookup = 'idle';
      });
      return;
    }
    const seq = ++lookSeq;
    untrack(() => (lookup = 'busy'));
    void fetchArchive().then((list) => {
      if (seq !== lookSeq) return;
      if (list === null) {
        lookup = 'failed';
        return;
      }
      archived = list.find((c) => c.id === id) ?? null;
      lookup = 'done';
    });
  });

  // ----- The draft -----
  let st = $state.raw<DraftState | null>(null);
  /** The latest card seen: its column, dates, state. */
  let shown = $state.raw<CardView | null>(null);
  let restored = $state(false);
  let discarded = false;

  const dirty = $derived(st ? dirtyOf(st) : []);
  const notFound = $derived(st === null && missing && lookup === 'done' && archived === null);
  const gone = $derived(st !== null && missing && lookup === 'done' && archived === null);
  const isArchived = $derived(shown?.archived ?? false);

  // What this panel's own moves and ticks have done to the card, so their new hash is not taken for a conflict.
  let inflight = 0;
  let ownBody: string | undefined;
  let idleWaiters: Array<() => void> = [];

  async function own<T>(job: () => Promise<T>): Promise<T> {
    inflight++;
    try {
      return await job();
    } finally {
      await tick(); // the store's answer reaches the draft before the write counts as done
      inflight--;
      if (inflight === 0) {
        ownBody = undefined;
        const waiting = idleWaiters;
        idleWaiters = [];
        for (const w of waiting) w();
      }
    }
  }

  async function whenIdle() {
    while (inflight > 0) await new Promise<void>((r) => idleWaiters.push(r));
  }

  function take(c: CardView | null) {
    if (c === null) return;
    shown = c;
    if (st === null) {
      const parked = takeParked(ownProject, ownId);
      if (parked) {
        restored = true;
        st = absorb(parked, c, null);
      } else {
        st = startDraft(c);
      }
      return;
    }
    st = absorb(st, c, inflight > 0 ? { body: ownBody } : null);
  }

  $effect(() => {
    const c = live ?? archived;
    untrack(() => take(c));
  });

  function edit(patch: Partial<Fields>) {
    if (!st) return;
    st = { ...st, draft: { ...st.draft, ...patch } };
    held = false;
    error = '';
    lost = false;
    flash = false;
  }

  // ----- Saving -----
  let saving = $state(false);
  let error = $state('');
  let errorRetry = $state(false);
  let flash = $state(false);
  let flashTimer: ReturnType<typeof setTimeout> | undefined;
  /** The card was not found when saving: it was deleted. The text stays on screen. */
  let lost = $state(false);
  let conflict = $state.raw<CardView | null>(null);
  let confirmClose = $state(false);
  let confirmDelete = $state(false);
  let menuOpen = $state(false);
  /** A conflict was put aside: no autosave until the draft changes again. */
  let held = false;
  let copied = $state(false);

  const blocker = $derived.by(() => {
    if (!st || dirty.length === 0) return null;
    if (dirty.includes('title') && st.draft.title.trim() === '') return 'A card needs a title';
    if (dirty.includes('extra')) return rowsProblem(st.draft.extra);
    return null;
  });

  const dialogOpen = $derived(conflict !== null || confirmClose || confirmDelete);

  /** A save was asked for while one was on its way: it runs when that one is done, if that one went well. */
  let queued = false;

  async function save(ifMatch?: string) {
    if (!st || locked || blocker || dirtyOf(st).length === 0) return;
    if (saving) {
      queued = true;
      return;
    }
    saving = true;
    error = '';
    errorRetry = false;
    try {
      await whenIdle();
      const now = st;
      if (!now) return;
      const names = dirtyOf(now);
      if (names.length === 0) return;
      const sent = now.draft;
      const card = await store.patch(id, toPatch(now, names), ifMatch ?? now.baseHash, { quiet: true });
      if (st) st = saved(st, sent, card);
      shown = card;
      if (live === null) archived = card;
      restored = false;
      held = false;
      clearTimeout(flashTimer);
      flash = true;
      flashTimer = setTimeout(() => (flash = false), 2400);
    } catch (e) {
      queued = false;
      fail(e);
    } finally {
      saving = false;
    }
    if (queued) {
      queued = false;
      if (!conflict && !lost && error === '') void save();
    }
  }

  function fail(e: unknown) {
    if (e instanceof ApiError) {
      if (e.status === 409 && e.current && 'column' in e.current) {
        conflict = e.current as CardView;
        confirmClose = false;
        return;
      }
      if (e.status === 404) {
        lost = true;
        return;
      }
      if (e.status === 0) {
        error = 'Could not reach Waypoint. Your changes are still here.';
        errorRetry = true;
        return;
      }
      error = e.message;
      errorRetry = e.status >= 500;
      return;
    }
    error = e instanceof Error && e.message !== '' ? e.message : 'Something went wrong';
    errorRetry = true;
  }

  /** Blur of the title or the notes: save what is dirty, unless something else is in the way. */
  function autosave() {
    if (dirty.length === 0 || dialogOpen || saving || held || lost || locked) return;
    void save();
  }

  function keepMine() {
    const hash = conflict?.hash;
    conflict = null;
    void save(hash);
  }

  function useTheirs() {
    const c = conflict;
    conflict = null;
    if (!c) return;
    st = startDraft(c);
    shown = c;
    if (live === null) archived = c;
    restored = false;
    error = '';
  }

  function dismissConflict() {
    conflict = null;
    held = true;
  }

  // ----- Moves and ticks -----
  const columnOptions = $derived.by(() => {
    const here = shown?.column ?? '';
    if (!whole || !live) return here === '' ? [] : [here];
    return whole.settings.columns.map((c) => c.name).filter((n) => n === here || canDrop(whole.settings, whole.cards, id, n) === null);
  });

  async function moveTo(column: string) {
    if (!live || !whole || column === live.column) return;
    await own(() => store.move(id, column, columnIds(whole.cards, column).length));
  }

  function onCheck(n: number) {
    if (!st || locked) return;
    // A card that is not on the board, or notes that are not saved, are ticked in the draft: the server's numbering is of what it has.
    if (!live || dirty.includes('body')) {
      const t = toggleItem(st.draft.body, n);
      if (t) edit({ body: t.body });
      return;
    }
    const t = toggleItem(ownBody ?? st.base.body, n);
    if (t) ownBody = t.body;
    void own(() => store.check(id, n)).catch(() => {});
  }

  // ----- Closing -----
  async function finish() {
    discarded = true;
    takeParked(ownProject, ownId);
    onClose();
    await tick();
    for (const el of document.querySelectorAll<HTMLElement>('[data-card-id]')) {
      if (el.dataset.cardId === ownId) {
        el.focus();
        break;
      }
    }
  }

  function requestClose() {
    menuOpen = false;
    if (dirty.length > 0 || saving) confirmClose = true;
    else void finish();
  }

  // A save that was running when Close was clicked may leave nothing to discard.
  $effect(() => {
    if (confirmClose && dirty.length === 0 && !saving) void finish();
  });

  async function remove() {
    confirmDelete = false;
    try {
      await store.remove(ownId, { quiet: true });
      await finish();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) error = 'This card changed outside Waypoint. Look at it again, then delete it.';
      else if (e instanceof ApiError && e.status === 404) await finish();
      else if (e instanceof ApiError && e.status === 0) error = 'Could not reach Waypoint. The card was not deleted.';
      else error = e instanceof Error ? e.message : 'Could not delete the card';
    }
  }

  async function copyText() {
    if (!st) return;
    const text = `${st.draft.title}\n\n${st.draft.body}`;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const area = document.createElement('textarea');
      area.value = text;
      document.body.append(area);
      area.select();
      document.execCommand('copy');
      area.remove();
    }
    copied = true;
  }

  // ----- Keys -----
  let panelEl: HTMLElement | undefined = $state();

  function onPanelKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void save();
    }
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || dialogOpen) return;
    if (menuOpen) {
      menuOpen = false;
      return;
    }
    const t = e.target;
    // Esc belongs to the panel and the board; another field (the filter, a dialog) has its own use for it.
    if (t instanceof HTMLElement && !panelEl?.contains(t) && t !== document.body && !t.closest('[data-board]')) return;
    e.preventDefault();
    requestClose();
  }

  $effect(() => {
    if (dirty.length === 0) return;
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  });

  // On a phone the panel covers the page, so it holds focus like a dialog; beside the board it does not.
  let modal = $state(false);
  onMount(() => {
    panelEl?.focus({ preventScroll: true });
    if (typeof matchMedia !== 'function') return;
    const mq = matchMedia('(max-width: 719px)');
    modal = mq.matches;
    const on = (e: MediaQueryListEvent) => (modal = e.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  });

  onDestroy(() => {
    clearTimeout(flashTimer);
    if (!discarded && st) parkDraft(ownProject, ownId, st);
  });

  // ----- The body: rendered, or edited -----
  let editing = $state(false);
  let bodyInput: HTMLTextAreaElement | undefined = $state();

  async function toggleEdit() {
    editing = !editing;
    if (editing) {
      await tick();
      bodyInput?.focus();
    }
  }

  // ----- Pieces for the form -----
  const knownCards = $derived.by((): KnownCard[] | null => {
    if (!whole) return null;
    const m = new Map<string, KnownCard>();
    for (const c of archive ?? []) m.set(c.id, { id: c.id, title: c.title, archived: true });
    for (const c of whole.cards) m.set(c.id, { id: c.id, title: c.title, archived: false });
    return [...m.values()];
  });

  /** A configured name, plus the card's own value when settings do not list it, so saving never changes it unseen. */
  function withCurrent(names: readonly string[], value: string): Array<{ value: string; label: string }> {
    const out = names.map((n) => ({ value: n, label: n }));
    if (value !== '' && !names.includes(value)) out.push({ value, label: `${value} (not in settings)` });
    return out;
  }

  const groupOptions = $derived(withCurrent((settings?.groups ?? []).map((g) => g.name), st?.draft.group ?? ''));
  const priorityOptions = $derived(withCurrent(settings?.priorities ?? [], st?.draft.priority ?? ''));
  const created = $derived(displayDate(shown?.created ?? null));
  const done = $derived(displayDate(shown?.done ?? null));
</script>

<svelte:window onkeydown={onWindowKeydown} />

<!-- The panel takes Cmd/Ctrl+Enter from any field inside it. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="panel"
  class:modal
  role="dialog"
  aria-modal={modal}
  aria-label="Card {id}"
  tabindex="-1"
  bind:this={panelEl}
  use:trapFocus={modal}
  onkeydown={onPanelKeydown}
>
  <header>
    <span class="id mono">{id}</span>
    {#if isArchived}<span class="chip"><Archive size={11} weight="bold" aria-hidden="true" />Archived</span>{/if}
    <span class="grow"></span>
    {#if st && !gone && !locked}
      <div class="more">
        <button class="icon" aria-label="More actions" title="More actions" aria-haspopup="menu" aria-expanded={menuOpen} onclick={() => (menuOpen = !menuOpen)}>
          <DotsThree size={18} weight="bold" aria-hidden="true" />
        </button>
        {#if menuOpen}
          <button class="menu-scrim" tabindex="-1" aria-label="Close menu" onclick={() => (menuOpen = false)}></button>
          <div class="menu" role="menu" aria-label="Actions for {id}">
            <button
              role="menuitem"
              class="danger"
              onclick={() => {
                menuOpen = false;
                confirmDelete = true;
              }}
            >
              <Trash size={14} weight="bold" aria-hidden="true" />Delete card
            </button>
          </div>
        {/if}
      </div>
    {/if}
    <button class="icon" aria-label="Close" title="Close (Esc)" onclick={requestClose}><X size={16} weight="bold" aria-hidden="true" /></button>
  </header>

  {#if st}
    <div class="scroll">
      {#if locked}
        <p class="note" role="status"><WarningCircle size={14} weight="bold" aria-hidden="true" />The board file has an error, so this card is read-only until it is fixed.</p>
      {/if}
      {#if gone || lost}
        <div class="note warn" role="alert">
          <WarningCircle size={14} weight="bold" aria-hidden="true" />
          <span>This card is gone from the board, deleted outside Waypoint. Your text is still here.</span>
          <button class="btn" onclick={copyText}><Copy size={13} weight="bold" aria-hidden="true" />{copied ? 'Copied' : 'Copy text'}</button>
        </div>
      {/if}
      {#if restored}
        <p class="note" role="status">Your unsaved changes from earlier are back.</p>
      {/if}

      <input
        class="title"
        aria-label="Title"
        value={st.draft.title}
        readonly={locked}
        spellcheck="true"
        oninput={(e) => edit({ title: e.currentTarget.value })}
        onblur={autosave}
        onkeydown={(e) => {
          if (e.key === 'Enter' && !e.isComposing) {
            e.preventDefault();
            void save();
          }
        }}
      />

      {#if created !== '' || done !== ''}
        <p class="dates mono">
          {#if created !== ''}<span>Created {created}</span>{/if}
          {#if done !== ''}<span>Done {done}</span>{/if}
        </p>
      {/if}

      <div class="fields">
        <label for="f-column">Column</label>
        <select id="f-column" value={shown?.column ?? ''} disabled={locked || !live} onchange={(e) => moveTo(e.currentTarget.value)}>
          {#each columnOptions as name (name)}<option value={name}>{name}</option>{/each}
        </select>

        <label for="f-group">Group</label>
        <select id="f-group" value={st.draft.group} disabled={locked} onchange={(e) => edit({ group: e.currentTarget.value })}>
          <option value="">No group</option>
          {#each groupOptions as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
        </select>

        <label for="f-priority">Priority</label>
        <select id="f-priority" value={st.draft.priority} disabled={locked} onchange={(e) => edit({ priority: e.currentTarget.value })}>
          <option value="">None</option>
          {#each priorityOptions as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
        </select>

        <span class="lab" id="f-depends">Depends on</span>
        <DependsField value={st.draft.depends} selfId={id} known={knownCards} disabled={locked} onChange={(next) => edit({ depends: next })} />

        <span class="lab" id="f-extra">Fields</span>
        <ExtraFields rows={st.draft.extra} disabled={locked} onChange={(next) => edit({ extra: next })} />
      </div>

      <section class="body" aria-label="Notes">
        <div class="body-head">
          <h2>Notes</h2>
          {#if !locked}
            <button class="btn small" aria-pressed={editing} onclick={toggleEdit}>
              {#if editing}<Eye size={13} weight="bold" aria-hidden="true" />Preview{:else}<PencilSimple size={13} weight="bold" aria-hidden="true" />Edit{/if}
            </button>
          {/if}
        </div>
        {#if editing}
          <textarea
            bind:this={bodyInput}
            class="editor"
            aria-label="Notes in Markdown"
            spellcheck="true"
            value={st.draft.body}
            oninput={(e) => edit({ body: e.currentTarget.value })}
            onblur={autosave}
          ></textarea>
        {:else if st.draft.body.trim() === ''}
          <p class="empty">No notes yet.</p>
        {:else}
          <MarkdownBody body={st.draft.body} disabled={locked} {onCheck} />
        {/if}
      </section>
    </div>

    <footer>
      <div class="status">
        {#if conflict === null && error}
          <p class="err" role="alert">
            {error}
            {#if errorRetry}<button class="btn small" onclick={() => save()}><ArrowClockwise size={12} weight="bold" aria-hidden="true" />Retry</button>{/if}
          </p>
        {:else if blocker}
          <p class="err" role="status">{blocker}</p>
        {:else if saving}
          <p class="muted" role="status">Saving</p>
        {:else if flash && dirty.length === 0}
          <p class="muted" role="status">Saved</p>
        {:else if dirty.length > 0}
          <p class="muted" role="status">Unsaved changes</p>
        {/if}
      </div>
      <button class="btn primary" disabled={dirty.length === 0 || saving || locked || blocker !== null} aria-keyshortcuts="Control+Enter Meta+Enter" onclick={() => save()}>Save</button>
    </footer>
  {:else if notFound}
    <div class="scroll">
      <h2 class="nf">Card not found</h2>
      <p class="muted">There is no card {id} on this board or in its archive.</p>
      <button class="btn" onclick={requestClose}>Back to the board</button>
    </div>
  {:else if lookup === 'failed'}
    <div class="scroll">
      <h2 class="nf">Could not look for {id}</h2>
      <p class="muted">The server did not answer.</p>
      <button class="btn" onclick={() => lookAgain++}><ArrowClockwise size={13} weight="bold" aria-hidden="true" />Try again</button>
    </div>
  {:else if store.parseError && whole === null}
    <div class="scroll"><p class="muted">The board file has an error, so the card cannot be shown yet.</p></div>
  {:else}
    <div class="scroll"><p class="muted" role="status">Loading {id}</p></div>
  {/if}
</div>

{#if conflict && st}
  <ConflictDialog mine={st.draft} theirs={startDraft(conflict).draft} onKeepMine={keepMine} onUseTheirs={useTheirs} onDismiss={dismissConflict} />
{/if}

{#if confirmClose}
  <ConfirmDialog
    title="Discard unsaved changes?"
    message="What you changed in {id} has not been saved."
    confirmLabel="Discard"
    cancelLabel="Keep editing"
    danger
    onConfirm={finish}
    onCancel={() => (confirmClose = false)}
  />
{/if}

{#if confirmDelete}
  <ConfirmDialog
    title="Delete {id}?"
    message={`"${st?.base.title ?? ''}" will be removed from the board.${dirty.length > 0 ? ' Your unsaved changes go with it.' : ''} This cannot be undone.`}
    confirmLabel="Delete card"
    danger
    onConfirm={remove}
    onCancel={() => (confirmDelete = false)}
  />
{/if}

<style>
  .panel {
    position: relative;
    flex: none;
    width: 440px;
    height: 100dvh;
    display: flex;
    flex-direction: column;
    background: var(--card);
    border-left: 1.5px solid var(--edge);
    animation: slide 160ms ease-out;
  }

  .panel:focus {
    outline: none;
  }

  @keyframes slide {
    from {
      transform: translateX(28px);
      opacity: 0;
    }
  }

  header {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px 8px 16px;
    border-bottom: 1px solid var(--line);
  }

  .id {
    font-size: 12.5px;
    font-weight: 600;
    color: var(--muted);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 6px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    font-size: 11px;
    color: var(--muted);
  }

  .grow {
    flex: 1;
  }

  .icon {
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

  .icon:hover {
    color: var(--text);
    background: color-mix(in srgb, var(--line) 60%, transparent);
  }

  .more {
    position: relative;
  }

  .menu-scrim {
    position: fixed;
    inset: 0;
    z-index: 60;
    border: 0;
    background: none;
  }

  .menu {
    position: absolute;
    z-index: 61;
    top: 100%;
    right: 0;
    min-width: 170px;
    padding: 4px;
    border: 1.5px solid var(--edge);
    border-radius: var(--r-control);
    background: var(--card);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .menu button {
    display: flex;
    gap: 8px;
    align-items: center;
    width: 100%;
    padding: 7px 10px;
    border: 0;
    border-radius: 6px;
    background: none;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .menu button:hover {
    background: color-mix(in srgb, var(--line) 60%, transparent);
  }

  .menu .danger {
    color: var(--red);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 18px;
  }

  .note {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
    margin: 0 0 12px;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    font-size: 12.5px;
    line-height: 1.4;
  }

  .note.warn {
    border-color: var(--red);
  }

  .note span {
    flex: 1;
    min-width: 160px;
  }

  .title {
    width: 100%;
    margin: 0;
    padding: 4px 6px;
    margin-left: -7px;
    border: 1px solid transparent;
    border-radius: var(--r-control);
    background: transparent;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
    line-height: 1.25;
  }

  .title:hover {
    border-color: var(--line);
  }

  .title:focus {
    border-color: var(--acc);
    outline: none;
  }

  .title:focus-visible {
    box-shadow: none;
  }

  .dates {
    display: flex;
    gap: 14px;
    margin: 6px 0 0;
    color: var(--muted);
    font-size: 11.5px;
  }

  .fields {
    display: grid;
    grid-template-columns: 84px minmax(0, 1fr);
    gap: 10px 10px;
    align-items: start;
    margin: 16px 0 18px;
  }

  .fields > label,
  .fields > .lab {
    padding-top: 7px;
    color: var(--muted);
    font-size: 12px;
  }

  select {
    width: 100%;
    padding: 6px 9px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
  }

  select:disabled {
    opacity: 0.6;
  }

  .body-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  h2 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .nf {
    margin: 4px 0 8px;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text);
    text-transform: none;
  }

  .btn.small {
    min-height: 26px;
    padding: 3px 10px;
    font-size: 12px;
  }

  .editor {
    display: block;
    width: 100%;
    min-height: 220px;
    resize: vertical;
    padding: 10px 11px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: color-mix(in srgb, var(--bg) 50%, var(--card));
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 1.55;
    field-sizing: content;
  }

  .empty,
  .muted {
    margin: 0;
    color: var(--muted);
    font-size: 13px;
  }

  footer {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 16px;
    border-top: 1px solid var(--line);
  }

  .status {
    flex: 1;
    min-width: 0;
  }

  .status p {
    margin: 0;
    font-size: 12.5px;
    line-height: 1.4;
  }

  .err {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    color: var(--red);
  }

  /* Under 720px the panel is a page of its own, and holds focus like a dialog. */
  @media (max-width: 719px) {
    .panel {
      position: fixed;
      z-index: 40;
      inset: 0;
      width: auto;
      border-left: 0;
    }

    .fields {
      grid-template-columns: 1fr;
      gap: 4px;
    }

    .fields > label,
    .fields > .lab {
      padding-top: 6px;
    }
  }
</style>
