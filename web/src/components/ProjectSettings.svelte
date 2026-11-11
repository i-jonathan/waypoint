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
  import { exportHref } from '../lib/transfer';
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
  import type { Appearance, ArchiveMode, CardView, ConvertedProject, ImportResult, ProjectInfo, Settings } from '../lib/types';
  import AppearanceFields from './AppearanceFields.svelte';
  import ColumnsEditor from './ColumnsEditor.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import ServerFields, { type ServerSettings } from './ServerFields.svelte';
  import ImportDialog from './ImportDialog.svelte';
  import UnsavedGuard from './UnsavedGuard.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    info: ProjectInfo;
    /** Reads the app settings, whose appearance is what a project's empty fields inherit. */
    client?: Pick<typeof api, 'settings' | 'archive' | 'convertProject' | 'importFile' | 'testDatabase'>;
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

  // ----- Converting the storage -----
  type Kind = 'sqlite' | 'markdown' | 'postgres';
  const KIND_LABEL: Record<Kind, string> = { sqlite: 'SQLite', markdown: 'Markdown', postgres: 'Postgres' };
  const label = (k: string) => KIND_LABEL[k as Kind] ?? k;
  /** The files a project can move to. A project on a server can go to either; a file project to the other kind. */
  const fileTargets = $derived((['sqlite', 'markdown'] as const).filter((k) => k !== info.storage));
  /** Where the server will put the file: the same folder and name with the other extension. */
  const newPath = (to: 'sqlite' | 'markdown') =>
    info.path ? info.path.replace(/\.[^./\\]*$/, '') + (to === 'sqlite' ? '.db' : '.md') : "a file in Waypoint's own folder";
  const where = $derived(info.path ?? `${info.host}:${info.port}/${info.database}`);
  /** The file target waiting for a yes. */
  let confirmingConvert = $state<'sqlite' | 'markdown' | null>(null);
  let choosingServer = $state(false);
  let target = $state<ServerSettings>({ engine: 'postgres', host: '', port: '', user: '', database: '', ssl: '', createDatabase: false });
  let targetReady = $state(false);
  let converting = $state(false);
  let converted = $state.raw<ConvertedProject | null>(null);
  let convertFailure = $state('');

  async function convert(to: Kind) {
    confirmingConvert = null;
    converting = true;
    convertFailure = '';
    converted = null;
    try {
      if (to === 'postgres') {
        const body: Parameters<typeof client.convertProject>[1] = {
          to,
          host: target.host.trim() === '' ? 'localhost' : target.host.trim(),
          port: target.port.trim() === '' ? 5432 : Number(target.port),
          user: target.user.trim(),
        };
        if (target.database.trim() !== '') body.database = target.database.trim();
        if (target.ssl !== '') body.ssl = target.ssl;
        if (target.createDatabase) body.create_database = true;
        converted = await client.convertProject(project, body);
        choosingServer = false;
      } else {
        converted = await client.convertProject(project, { to });
      }
    } catch (err) {
      convertFailure = err instanceof Error && err.message !== '' ? err.message : 'The conversion failed. Nothing was changed.';
    } finally {
      converting = false;
    }
  }

  // ----- Import and export -----
  let picker: HTMLInputElement | undefined = $state();
  let importing = $state.raw<File | null>(null);
  let importNote = $state('');

  function chosen(e: Event & { currentTarget: HTMLInputElement }) {
    const file = e.currentTarget.files?.[0];
    if (!file) return;
    importing = file;
    importNote = '';
  }

  function imported(file: File, r: ImportResult) {
    const n = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;
    importNote =
      r.format === 'csv'
        ? `Imported ${file.name}: added ${n(r.created.length, 'card', 'cards')} and updated ${n(r.updated.length, 'card', 'cards')}.`
        : `Imported ${file.name}: ${n(r.cards, 'card', 'cards')} and ${n(r.archived, 'archived card', 'archived cards')}.`;
    closeImport();
  }

  function closeImport() {
    importing = null;
    if (picker) picker.value = ''; // so the same file can be chosen again
  }

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
        {#if info.path}
          <dt>Path</dt>
          <dd class="mono">{info.path}</dd>
        {:else}
          <dt>Server</dt>
          <dd class="mono">{where}</dd>
          <dt>User</dt>
          <dd class="mono">{info.user}</dd>
        {/if}
        <dt>Type</dt>
        <dd class="mono">{info.storage}</dd>
      </dl>
      <div class="convert">
        <div class="targets">
          {#each fileTargets as to (to)}
            <button type="button" class="btn" disabled={converting || dirty} onclick={() => (confirmingConvert = to)}>Convert to {label(to)}</button>
          {/each}
          {#if !info.host}
            <button type="button" class="btn" disabled={converting || dirty} onclick={() => (choosingServer = true)}>Convert to Postgres</button>
          {/if}
        </div>
        {#if dirty}
          <p class="sf-hint">Save or cancel your changes first.</p>
        {:else}
          <p class="sf-hint">Writes a copy, checks that it matches, then switches the project to it. The old {info.host ? 'database is' : 'files are'} kept.</p>
        {/if}
        {#if converted}
          <div class="sf-note" role="status" aria-label="Conversion result">
            <div>
              <p>Converted to {label(converted.storage)}.</p>
              {#if converted.retired}
                <p>The old database at <span class="mono">{converted.retired}</span> is kept, marked as retired.</p>
              {:else if converted.backups.length > 0}
                <p>The old files are kept as:</p>
                <ul>{#each converted.backups as file (file)}<li class="mono">{file}</li>{/each}</ul>
              {/if}
            </div>
          </div>
        {:else if convertFailure}
          <div class="sf-error" role="alert"><p>{convertFailure}</p></div>
        {/if}
      </div>
      <div class="sf-field prefix">
        <label class="sf-label" for="ps-prefix">Prefix</label>
        <input id="ps-prefix" class="sf-input mono" type="text" value={base.prefix} readonly aria-describedby="ps-prefix-note" />
        <p class="sf-hint" id="ps-prefix-note">Fixed when the project is created.</p>
      </div>
    </section>

    <section class="sf-section" aria-labelledby="ps-transfer">
      <h2 id="ps-transfer">Import and export</h2>
      <p class="sf-hint">Export the whole project, or add cards from a CSV file or a JSON export.</p>
      <div class="transfer">
        <a class="btn" href={exportHref(project, 'csv')} download>Export CSV</a>
        <a class="btn" href={exportHref(project, 'json')} download>Export JSON</a>
        <a class="btn" href={exportHref(project, 'md')} download>Export Markdown</a>
        <button type="button" class="btn" onclick={() => picker?.click()}>Import a file</button>
        <input bind:this={picker} class="picker" type="file" accept=".csv,.json,text/csv,application/json" aria-label="Import file" onchange={chosen} />
      </div>
      {#if importNote}<div class="sf-note" role="status" aria-label="Import result"><p>{importNote}</p></div>{/if}
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

{#if importing}
  <ImportDialog {project} file={importing} {client} onClose={closeImport} onDone={(r) => imported(importing!, r)} />
{/if}

{#if confirmingConvert}
  {@const to = confirmingConvert}
  <ConfirmDialog
    title="Convert this project to {label(to)}?"
    confirmLabel="Convert"
    onConfirm={() => void convert(to)}
    onCancel={() => (confirmingConvert = null)}
  >
    <p class="line-msg">Waypoint will write the board to</p>
    <p class="line-msg mono">{newPath(to)}</p>
    {#if info.host}
      <p class="line-msg">and check that every card and setting matches before it switches over. The old database is kept as it is, marked as retired so nothing writes to it by mistake.</p>
    {:else}
      <p class="line-msg">and check that every card and setting matches before it switches over. The old files are kept next to it, renamed with .bak and today's date.</p>
    {/if}
  </ConfirmDialog>
{/if}

{#if choosingServer}
  <ConfirmDialog
    title="Convert this project to Postgres?"
    confirmLabel="Convert to Postgres"
    confirmDisabled={!targetReady || converting}
    wide
    focus="dialog"
    onConfirm={() => void convert('postgres')}
    onCancel={() => (choosingServer = false)}
  >
    <p class="line-msg">Waypoint will copy the board into an empty database, check that every card and setting matches, then switch the project to it. The old files are kept next to the old board, renamed with .bak and today's date.</p>
    <ServerFields project={info.name} bind:value={target} bind:ready={targetReady} mode="convert" {client} />
    {#if convertFailure}<div class="sf-error" role="alert"><p>{convertFailure}</p></div>{/if}
  </ConfirmDialog>
{/if}

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
  .transfer {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin: 8px 0;
  }

  .picker {
    display: none;
  }

  .targets {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .convert {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    margin: 10px 0 14px;
  }

  .convert ul {
    margin: 4px 0 0;
    padding-left: 18px;
  }

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
