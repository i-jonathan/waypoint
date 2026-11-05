<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import ArrowDown from 'phosphor-svelte/lib/ArrowDown';
  import ArrowUp from 'phosphor-svelte/lib/ArrowUp';
  import Plus from 'phosphor-svelte/lib/Plus';
  import Trash from 'phosphor-svelte/lib/Trash';
  import { PRESETS } from '../lib/accent';
  import { api, ApiError } from '../lib/api';
  import type { BoardStore } from '../lib/board.svelte';
  import { groupColor } from '../lib/colors';
  import { BUILTIN, lookOf } from '../lib/look';
  import { preview } from '../lib/preview.svelte';
  import {
    addGroup,
    addPriority,
    buildChange,
    clearWarnings,
    removesNames,
    columnCounts,
    isDirty,
    moveRow,
    toDraft,
    validateDraft,
    type Draft,
  } from '../lib/settingsEdit';
  import type { Appearance, ArchiveMode, CardView, ProjectInfo, Settings } from '../lib/types';
  import AppearanceFields from './AppearanceFields.svelte';
  import ColumnsEditor from './ColumnsEditor.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import UnsavedGuard from './UnsavedGuard.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    info: ProjectInfo;
    /** Reads the app settings, whose appearance is what a project's empty fields inherit. */
    client?: Pick<typeof api, 'settings' | 'archive'>;
  }

  let { store, project, info, client = api }: Props = $props();

  // ----- The form: edited rows, against the settings they were made from. -----
  // `base` and `baseHash` are what the form started from. The store may move on (another
  // tab, an agent editing the file); the form does not, until the owner chooses.
  let base = $state.raw<Settings | null>(null);
  let baseHash = $state('');
  let draft = $state<Draft | null>(null);

  let saving = $state(false);
  let saved = $state(false);
  /** A save met a 409. */
  let conflict = $state(false);
  let failure = $state('');
  /** Group and priority removals waiting for a yes, one line each. */
  let pending = $state.raw<string[] | null>(null);
  let root: HTMLElement | undefined = $state();

  function adopt() {
    const view = store.view;
    if (!view) return;
    base = view.settings;
    baseHash = view.settings_hash;
    draft = toDraft(view.settings);
    conflict = false;
    failure = '';
  }

  $effect(() => {
    if (store.view && untrack(() => base) === null) untrack(adopt);
  });

  const dirty = $derived(base !== null && draft !== null && isDirty(base, draft));
  const stale = $derived(base !== null && store.settingsHash !== null && store.settingsHash !== baseHash);

  // A form nobody has touched follows the file.
  $effect(() => {
    if (stale && !untrack(() => dirty) && !untrack(() => saving)) untrack(adopt);
  });

  const changedElsewhere = $derived(conflict || (stale && dirty));

  const errors = $derived(draft ? validateDraft(draft) : {});
  const counts = $derived(columnCounts(store.baseline?.cards ?? []));

  // ----- The app settings, for what an empty appearance field inherits. -----
  let app = $state.raw<Appearance | null>(null);
  onMount(() => {
    client.settings().then(
      (g) => (app = g.appearance),
      () => {}, // the resolved values from the board stand in
    );
  });

  function inherit(f: keyof Appearance): { value: string; source: 'global' | 'default' } {
    if (app) return app[f] ? { value: app[f], source: 'global' } : { value: BUILTIN[f], source: 'default' };
    const a = store.appearance;
    return { value: a?.[f] ?? BUILTIN[f], source: a?.source[f] === 'default' ? 'default' : 'global' };
  }

  const inherited = $derived<Appearance>({
    theme: inherit('theme').value as Appearance['theme'],
    accent: inherit('accent').value,
    density: inherit('density').value as Appearance['density'],
    swimlane: inherit('swimlane').value as Appearance['swimlane'],
    textures: inherit('textures').value as Appearance['textures'],
  });

  // Try the look on while the appearance is being edited; nothing is kept until Save.
  $effect(() => {
    if (draft && base && JSON.stringify(draft.appearance) !== JSON.stringify(base.appearance)) {
      preview.set(lookOf(draft.appearance, inherited));
    } else {
      preview.clear();
    }
  });
  onDestroy(() => preview.clear());

  // ----- Saving -----
  function focusFirstInvalid() {
    void tick().then(() => root?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());
  }

  async function save() {
    if (!draft || !base || saving || changedElsewhere) return;
    if (Object.keys(errors).length > 0) {
      focusFirstInvalid();
      return;
    }
    // The server clears archived cards too, so the warning has to count them. If they can't be
    // read the count would be wrong, so nothing is saved.
    let archived: CardView[] = [];
    if (removesNames(base, draft)) {
      saving = true;
      failure = '';
      try {
        archived = await client.archive(project);
      } catch (e) {
        failure = e instanceof Error && e.message !== '' ? e.message : 'Something went wrong';
        return;
      } finally {
        saving = false;
      }
      if (!draft || !base) return;
    }
    const lines = clearWarnings(base, draft, store.baseline?.cards ?? [], archived);
    if (lines.length > 0) {
      pending = lines;
      return;
    }
    void commit();
  }

  async function commit() {
    if (!draft || !base) return;
    pending = null;
    saving = true;
    failure = '';
    try {
      await store.updateSettings(buildChange(base, draft));
      adopt();
      saved = true;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) conflict = true;
      else failure = e instanceof Error && e.message !== '' ? e.message : 'Something went wrong';
    } finally {
      saving = false;
    }
  }

  function cancel() {
    if (!base) return;
    draft = toDraft(base);
    failure = '';
    saved = false;
  }

  function reload() {
    adopt();
    saved = false;
  }

  const twoSteps = $derived(/would both become/.test(failure));

  // ----- Groups and priorities -----
  const presetNames = Object.keys(PRESETS);
  const title = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
  /** Group rows whose colour is a typed hex. */
  let custom = $state<string[]>([]);
  const isCustom = (key: string, color: string) => custom.includes(key) || color.startsWith('#');

  function pickColor(key: string, value: string, set: (c: string) => void) {
    if (value === 'custom') {
      custom = [...custom, key];
      set('#');
    } else {
      custom = custom.filter((k) => k !== key);
      set(value);
    }
  }

  function shiftPriority(i: number, to: number) {
    if (draft) draft.priorities = moveRow(draft.priorities, i, to);
  }

  async function addAndFocus(kind: 'group' | 'priority') {
    if (!draft) return;
    let id: string;
    if (kind === 'group') {
      draft.groups = addGroup(draft.groups);
      id = `grp-${draft.groups[draft.groups.length - 1]!.key}-name`;
    } else {
      draft.priorities = addPriority(draft.priorities);
      id = `pri-${draft.priorities[draft.priorities.length - 1]!.key}-name`;
    }
    await tick();
    const input = document.getElementById(id) as HTMLInputElement | null;
    input?.focus();
    input?.select();
  }

  const MODES: Array<[ArchiveMode, string]> = [['off', 'Off'], ['manual', 'Manual'], ['auto', 'Auto']];
</script>

<div class="sf-page" bind:this={root}>
  <div>
    <h1>Project settings</h1>
    <p class="sf-sub">For <span class="mono">{project}</span>.</p>
  </div>

  {#if draft === null || base === null}
    <p class="sf-sub" role="status">Loading</p>
  {:else}
    <section class="sf-section" aria-labelledby="ps-look">
      <h2 id="ps-look">Appearance</h2>
      <AppearanceFields bind:value={draft.appearance} {inherit} resettable swimlaneLabel="Swimlane" {errors} />
    </section>

    <section class="sf-section" aria-labelledby="ps-cols">
      <h2 id="ps-cols">Columns</h2>
      <ColumnsEditor bind:rows={draft.columns} {counts} {errors} />
    </section>

    <section class="sf-section" aria-labelledby="ps-groups">
      <h2 id="ps-groups">Groups</h2>
      <ol class="rows">
        {#each draft.groups as g, i (g.key)}
          {@const nameErr = errors[`grp-${g.key}-name`]}
          {@const colorErr = errors[`grp-${g.key}-color`]}
          {@const hex = isCustom(g.key, g.color)}
          <li>
            <div class="row" role="group" aria-labelledby="grp-{g.key}-head">
              <span class="num mono" id="grp-{g.key}-head">Group {i + 1}</span>
              <div class="sf-field grow">
                <label class="sf-label" for="grp-{g.key}-name">Name</label>
                <input
                  id="grp-{g.key}-name"
                  class="sf-input"
                  type="text"
                  bind:value={g.name}
                  aria-invalid={nameErr ? 'true' : undefined}
                  aria-describedby={nameErr ? `grp-${g.key}-name-err` : undefined}
                />
                {#if nameErr}<p class="sf-error" id="grp-{g.key}-name-err">{nameErr}</p>{/if}
              </div>
              <div class="sf-field colour">
                <label class="sf-label" for="grp-{g.key}-pick">Colour</label>
                <div class="pick">
                  <span class="line" style:background={groupColor(g.color)}></span>
                  <select
                    id="grp-{g.key}-pick"
                    class="sf-input"
                    value={hex ? 'custom' : g.color}
                    onchange={(e) => pickColor(g.key, e.currentTarget.value, (c) => (g.color = c))}
                  >
                    {#each presetNames as n (n)}<option value={n}>{title(n)}</option>{/each}
                    {#if !hex && !presetNames.includes(g.color)}<option value={g.color}>{g.color || 'None'}</option>{/if}
                    <option value="custom">Custom hex</option>
                  </select>
                </div>
              </div>
              {#if hex}
                <div class="sf-field hexf">
                  <label class="sf-label" for="grp-{g.key}-color">Hex colour</label>
                  <input
                    id="grp-{g.key}-color"
                    class="sf-input mono"
                    type="text"
                    placeholder="#rrggbb"
                    spellcheck="false"
                    bind:value={g.color}
                    aria-invalid={colorErr ? 'true' : undefined}
                    aria-describedby={colorErr ? `grp-${g.key}-color-err` : undefined}
                  />
                  {#if colorErr}<p class="sf-error" id="grp-{g.key}-color-err">{colorErr}</p>{/if}
                </div>
              {/if}
              <button
                type="button"
                class="sf-icon-btn drop"
                aria-label="Remove group {g.name.trim() || i + 1}"
                title="Remove group"
                onclick={() => draft && (draft.groups = draft.groups.filter((o) => o.key !== g.key))}
              >
                <Trash size={14} aria-hidden="true" />
              </button>
            </div>
          </li>
        {/each}
      </ol>
      <button type="button" class="btn add" onclick={() => addAndFocus('group')}><Plus size={14} weight="bold" aria-hidden="true" />Add group</button>
    </section>

    <section class="sf-section" aria-labelledby="ps-pri">
      <h2 id="ps-pri">Priorities</h2>
      <p class="sf-hint">Listed from the most urgent to the least.</p>
      <ol class="rows">
        {#each draft.priorities as p, i (p.key)}
          {@const nameErr = errors[`pri-${p.key}-name`]}
          <li>
            <div class="row" role="group" aria-labelledby="pri-{p.key}-head">
              <span class="num mono" id="pri-{p.key}-head">Priority {i + 1}</span>
              <div class="sf-field grow">
                <label class="sf-label" for="pri-{p.key}-name">Name</label>
                <input
                  id="pri-{p.key}-name"
                  class="sf-input mono"
                  type="text"
                  bind:value={p.name}
                  aria-invalid={nameErr ? 'true' : undefined}
                  aria-describedby={nameErr ? `pri-${p.key}-name-err` : undefined}
                />
                {#if nameErr}<p class="sf-error" id="pri-{p.key}-name-err">{nameErr}</p>{/if}
              </div>
              <div class="btns">
                <button type="button" class="sf-icon-btn" aria-label="Move priority {p.name.trim() || i + 1} up" title="Move up" disabled={i === 0} onclick={() => shiftPriority(i, i - 1)}>
                  <ArrowUp size={14} weight="bold" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  class="sf-icon-btn"
                  aria-label="Move priority {p.name.trim() || i + 1} down"
                  title="Move down"
                  disabled={i === draft.priorities.length - 1}
                  onclick={() => shiftPriority(i, i + 1)}
                >
                  <ArrowDown size={14} weight="bold" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  class="sf-icon-btn"
                  aria-label="Remove priority {p.name.trim() || i + 1}"
                  title="Remove priority"
                  onclick={() => draft && (draft.priorities = draft.priorities.filter((o) => o.key !== p.key))}
                >
                  <Trash size={14} aria-hidden="true" />
                </button>
              </div>
            </div>
          </li>
        {/each}
      </ol>
      <button type="button" class="btn add" onclick={() => addAndFocus('priority')}><Plus size={14} weight="bold" aria-hidden="true" />Add priority</button>
    </section>

    <section class="sf-section" aria-labelledby="ps-archive">
      <h2 id="ps-archive">Archive</h2>
      <fieldset class="plain">
        <legend class="sf-label">Archive mode</legend>
        <div class="choices">
          {#each MODES as [v, label] (v)}
            <label class="sf-check">
              <input type="radio" name="ps-archive-mode" value={v} checked={draft.archive.mode === v} onchange={() => draft && (draft.archive.mode = v)} />
              <span>{label}</span>
            </label>
          {/each}
        </div>
        <p class="sf-hint">
          {#if draft.archive.mode === 'off'}Done cards stay on the board until you delete them.
          {:else if draft.archive.mode === 'manual'}Done cards move to the archive when you archive them.
          {:else}Done cards move to the archive by themselves.{/if}
        </p>
      </fieldset>
      {#if draft.archive.mode === 'auto'}
        {@const err = errors['archive-days']}
        <div class="sf-field days">
          <label class="sf-label" for="archive-days">Archive after (days)</label>
          <input
            id="archive-days"
            class="sf-input mono"
            type="text"
            inputmode="numeric"
            bind:value={draft.archive.days}
            aria-invalid={err ? 'true' : undefined}
            aria-describedby={err ? 'archive-days-err' : undefined}
          />
          {#if err}<p class="sf-error" id="archive-days-err">{err}</p>{/if}
        </div>
      {/if}
    </section>

    <section class="sf-section" aria-labelledby="ps-storage">
      <h2 id="ps-storage">Storage</h2>
      <dl class="facts">
        <dt>Path</dt>
        <dd class="mono">{info.path}</dd>
        <dt>Type</dt>
        <dd class="mono">{info.storage}</dd>
      </dl>
      <div class="sf-field prefix">
        <label class="sf-label" for="ps-prefix">Prefix</label>
        <input id="ps-prefix" class="sf-input mono" type="text" value={base.prefix} readonly aria-describedby="ps-prefix-note" />
        <p class="sf-hint" id="ps-prefix-note">Fixed when the project is created.</p>
      </div>
    </section>

    {#if changedElsewhere}
      <div class="sf-note" role="alert">
        <span>These settings changed elsewhere. Reload them?</span>
        <button type="button" class="btn" onclick={reload}>Reload</button>
      </div>
    {:else if failure}
      <div class="sf-error" role="alert">
        <p>{failure}</p>
        {#if twoSteps}<p>Save in two steps: remove the group first, then rename the other.</p>{/if}
      </div>
    {/if}

    <div class="sf-bar">
      <span class="status" class:dirty role="status">{saving ? 'Saving' : dirty ? 'Unsaved changes' : saved ? 'Saved' : ''}</span>
      <button type="button" class="btn" disabled={!dirty || saving} onclick={cancel}>Cancel</button>
      <button type="button" class="btn primary" disabled={!dirty || saving || changedElsewhere} onclick={save}>Save</button>
    </div>
  {/if}
</div>

<UnsavedGuard {dirty} />

{#if pending}
  <ConfirmDialog
    title="Some cards will lose a group or priority"
    confirmLabel="Save and clear"
    danger
    onConfirm={() => void commit()}
    onCancel={() => (pending = null)}
  >
    {#each pending as line (line)}<p class="line-msg">{line}</p>{/each}
  </ConfirmDialog>
{/if}

<style>
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 10px 14px;
    padding: 10px 12px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: color-mix(in srgb, var(--bg) 40%, var(--card));
  }

  .num {
    flex: none;
    width: 76px;
    padding-top: 26px;
    white-space: nowrap;
    font-size: 11px;
    color: var(--muted);
  }

  .grow {
    flex: 1;
    min-width: 160px;
  }

  .colour {
    width: 170px;
  }

  .hexf {
    width: 120px;
  }

  .pick {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
  }

  .line {
    flex: none;
    width: 16px;
    height: 4px;
    border-radius: 2px;
  }

  .drop,
  .btns {
    margin-top: 20px;
  }

  .btns {
    display: flex;
    gap: 6px;
  }

  .add {
    align-self: flex-start;
  }

  .plain {
    margin: 0;
    padding: 0;
    border: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .plain legend {
    padding: 0;
    margin-bottom: 4px;
  }

  .choices {
    display: flex;
    gap: 18px;
  }

  .days,
  .prefix {
    max-width: 180px;
  }

  .facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 6px 16px;
    margin: 0;
    font-size: 13px;
  }

  .facts dt {
    color: var(--muted);
  }

  .facts dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .sf-note {
    justify-content: space-between;
    align-items: center;
  }

  [role='alert'].sf-error p {
    margin: 0;
  }

  .line-msg {
    margin: 4px 0 0;
    font-size: 13px;
    line-height: 1.45;
  }

  @media (max-width: 560px) {
    .num {
      width: 100%;
      padding-top: 0;
    }

    .drop,
    .btns {
      margin-top: 0;
    }
  }
</style>
