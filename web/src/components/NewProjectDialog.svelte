<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { trapFocus } from '../lib/trap';
  import type { NewProject } from '../lib/types';
  import { DEFAULT_PORTS, SERVER_ENGINES, engineLabel, isServerEngine, type ServerEngine } from '../lib/server';
  import ServerFields, { type ServerSettings } from './ServerFields.svelte';

  type Tab = 'create' | 'open';
  type Field = 'name' | 'location' | 'prefix' | 'path' | 'server';

  interface Props {
    client?: Pick<typeof api, 'createProject' | 'testDatabase'>;
    initialTab?: Tab;
    onClose: () => void;
    /** Called with the name the registry gave the project, once it exists. */
    onCreated: (name: string) => void;
  }

  let { client = api, initialTab = 'create', onClose, onCreated }: Props = $props();

  const id = `np-${Math.random().toString(36).slice(2, 7)}`;
  const PREFIX = /^[A-Za-z][A-Za-z0-9]*$/;
  const PREFIX_MESSAGE = 'Use letters and numbers, starting with a letter';
  const KINDS = {
    sqlite: { re: /\.(db|sqlite|sqlite3)$/i, message: 'A SQLite database path must end in .db, .sqlite or .sqlite3', example: '/path/to/board.db' },
    markdown: { re: /\.(md|markdown)$/i, message: 'A Markdown path must end in .md or .markdown', example: '/path/to/board.md' },
  } as const;
  const ANY_BOARD = /\.(md|markdown|db|sqlite|sqlite3)$/i;

  let tab = $state<Tab>(untrack(() => initialTab));

  let name = $state('');
  let location = $state('');
  let prefix = $state('T');
  let storage = $state<'sqlite' | 'markdown' | ServerEngine>('sqlite');
  let server = $state<ServerSettings>({ engine: 'postgres', host: '', port: '', user: '', database: '', ssl: '', createDatabase: false });
  let serverReady = $state(false);
  let path = $state('');
  let openName = $state('');
  let openNameEdited = $state(false);

  let errors = $state<Partial<Record<Field, string>>>({});
  let working = $state(false);
  let dialog: HTMLElement | undefined = $state();

  const stem = (p: string) => {
    const file = p.trim().split(/[\\/]/).pop() ?? '';
    return file.replace(ANY_BOARD, '');
  };

  function onPath(value: string) {
    path = value;
    if (!openNameEdited) openName = stem(value);
    clear('path');
  }

  function switchStorage(next: 'sqlite' | 'markdown' | ServerEngine) {
    storage = next;
    if (isServerEngine(next)) server.engine = next;
    clear('location');
    clear('server');
  }

  function clear(f: Field) {
    if (errors[f] !== undefined) errors = { ...errors, [f]: undefined };
  }

  onMount(() => {
    const before = document.activeElement;
    document.getElementById(`${id}-${tab === 'create' ? 'name' : 'path'}`)?.focus();
    return () => {
      if (before instanceof HTMLElement && before.isConnected) before.focus();
    };
  });

  function focusInvalid() {
    void tick().then(() => dialog?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());
  }

  // ----- Tabs -----
  function pick(next: Tab, focus = false) {
    tab = next;
    errors = {};
    if (focus) void tick().then(() => document.getElementById(`${id}-tab-${next}`)?.focus());
  }

  function onTabKey(e: KeyboardEvent) {
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft' || e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      pick(e.key === 'ArrowRight' || e.key === 'End' ? 'open' : 'create', true);
    }
  }

  // ----- Submitting -----
  function check(): Partial<Record<Field, string>> {
    const out: Partial<Record<Field, string>> = {};
    if (tab === 'create') {
      if (name.trim() === '') out.name = 'Give the project a name';
      if (!isServerEngine(storage) && location.trim() !== '' && !KINDS[storage].re.test(location.trim())) out.location = KINDS[storage].message;
      if (!PREFIX.test(prefix)) out.prefix = PREFIX_MESSAGE;
    } else {
      if (path.trim() === '') out.path = 'Enter the path to a board file';
      else if (!ANY_BOARD.test(path.trim())) out.path = 'The path must end in .md, .markdown, .db, .sqlite or .sqlite3';
      if (openName.trim() === '') out.name = 'Give the project a name';
    }
    return out;
  }

  /** Which field a refusal belongs to, from what it says. */
  function fieldFor(message: string): Field {
    if (/\bprefix\b/i.test(message)) return 'prefix';
    if (/project name|project "[^"]*" already exists/i.test(message)) return 'name';
    if (tab === 'create' && isServerEngine(storage)) return 'server';
    return tab === 'create' ? 'location' : 'path';
  }

  /** The request for a project on a database server. There is no password in it. */
  function serverBody(): NewProject {
    const body: NewProject = { name: name.trim(), prefix, storage: server.engine };
    body.host = server.host.trim() === '' ? 'localhost' : server.host.trim();
    body.port = server.port.trim() === '' ? DEFAULT_PORTS[server.engine] : Number(server.port);
    body.user = server.user.trim();
    if (server.database.trim() !== '') body.database = server.database.trim();
    if (server.ssl !== '') body.ssl = server.ssl;
    if (server.createDatabase) body.create_database = true;
    return body;
  }

  async function submit(e?: Event) {
    e?.preventDefault();
    if (working) return;
    const found = check();
    errors = found;
    if (Object.keys(found).length > 0) {
      focusInvalid();
      return;
    }
    const body: NewProject =
      tab === 'create' && isServerEngine(storage)
        ? serverBody()
        : tab === 'create'
          ? { name: name.trim(), ...(location.trim() !== '' ? { path: location.trim() } : {}), prefix, storage }
          : { name: openName.trim(), path: path.trim(), existing: true };
    working = true;
    try {
      const made = await client.createProject(body);
      onCreated(made.name);
    } catch (err) {
      const message = err instanceof Error && err.message !== '' ? err.message : 'Something went wrong';
      errors = { [fieldFor(message)]: message };
      focusInvalid();
    } finally {
      working = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    onClose();
  }

  const described = (f: Field, hint?: string) => [errors[f] ? `${id}-${f}-err` : '', hint ?? ''].filter(Boolean).join(' ') || undefined;
</script>

<button class="scrim" tabindex="-1" aria-label="Close" onclick={onClose}></button>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="{id}-title" tabindex="-1" bind:this={dialog} use:trapFocus onkeydown={onKeydown}>
  <h2 id="{id}-title">New project</h2>

  <div class="tabs" role="tablist" aria-label="How to add the project">
    <button
      type="button"
      role="tab"
      id="{id}-tab-create"
      aria-selected={tab === 'create'}
      aria-controls="{id}-panel"
      tabindex={tab === 'create' ? 0 : -1}
      onclick={() => pick('create')}
      onkeydown={onTabKey}>Create</button
    >
    <button
      type="button"
      role="tab"
      id="{id}-tab-open"
      aria-selected={tab === 'open'}
      aria-controls="{id}-panel"
      tabindex={tab === 'open' ? 0 : -1}
      onclick={() => pick('open')}
      onkeydown={onTabKey}>Open existing file</button
    >
  </div>

  <div id="{id}-panel" role="tabpanel" aria-labelledby="{id}-tab-{tab}">
  <form onsubmit={submit} novalidate>
    {#if tab === 'create'}
      <div class="sf-field">
        <label class="sf-label" for="{id}-name">Name</label>
        <input
          id="{id}-name"
          class="sf-input"
          type="text"
          bind:value={name}
          oninput={() => clear('name')}
          autocomplete="off"
          aria-invalid={errors.name ? 'true' : undefined}
          aria-describedby={described('name')}
        />
        {#if errors.name}<p class="sf-error" id="{id}-name-err">{errors.name}</p>{/if}
      </div>

      <fieldset class="storage">
        <legend class="sf-label">Storage</legend>
        <label class="sf-check">
          <input type="radio" name="{id}-storage" value="sqlite" checked={storage === 'sqlite'} onchange={() => switchStorage('sqlite')} />
          <span>SQLite database</span>
        </label>
        <label class="sf-check">
          <input type="radio" name="{id}-storage" value="markdown" checked={storage === 'markdown'} onchange={() => switchStorage('markdown')} />
          <span>Markdown file</span>
        </label>
        {#each SERVER_ENGINES as eng (eng.value)}
          <label class="sf-check">
            <input type="radio" name="{id}-storage" value={eng.value} checked={storage === eng.value} onchange={() => switchStorage(eng.value)} />
            <span>Database server ({eng.label})</span>
          </label>
        {/each}
        <p class="sf-hint">
          {storage === 'sqlite'
            ? 'One file that stays fast with many cards. Use Waypoint or its CLI to edit it.'
            : storage === 'markdown'
              ? 'A file you can read and edit by hand, and keep in git.'
              : `A ${engineLabel(storage)} database, on this computer (a Docker container, say) or elsewhere. For one person's data.`}
        </p>
      </fieldset>

      {#if isServerEngine(storage)}
        <ServerFields project={name.trim()} bind:value={server} bind:ready={serverReady} {client} />
        {#if errors.server}<p class="sf-error" id="{id}-server-err" role="alert">{errors.server}</p>{/if}
      {:else}
      <div class="sf-field">
        <label class="sf-label" for="{id}-location">Location</label>
        <input
          id="{id}-location"
          class="sf-input mono"
          type="text"
          placeholder={KINDS[storage].example}
          spellcheck="false"
          autocomplete="off"
          bind:value={location}
          oninput={() => clear('location')}
          aria-invalid={errors.location ? 'true' : undefined}
          aria-describedby={described('location', `${id}-location-hint`)}
        />
        <p class="sf-hint" id="{id}-location-hint">Leave empty to keep it in Waypoint's folder</p>
        {#if errors.location}<p class="sf-error" id="{id}-location-err">{errors.location}</p>{/if}
      </div>
      {/if}

      <div class="sf-field short">
        <label class="sf-label" for="{id}-prefix">Prefix</label>
        <input
          id="{id}-prefix"
          class="sf-input mono"
          type="text"
          bind:value={prefix}
          oninput={(e) => {
            // A prefix is short and strict, so it is checked as it is typed.
            errors = { ...errors, prefix: PREFIX.test(e.currentTarget.value) ? undefined : PREFIX_MESSAGE };
          }}
          spellcheck="false"
          autocomplete="off"
          aria-invalid={errors.prefix ? 'true' : undefined}
          aria-describedby={described('prefix', `${id}-prefix-hint`)}
        />
        <p class="sf-hint" id="{id}-prefix-hint">Card ids start with it, as in T-1. It cannot be changed later.</p>
        {#if errors.prefix}<p class="sf-error" id="{id}-prefix-err">{errors.prefix}</p>{/if}
      </div>
    {:else}
      <div class="sf-field">
        <label class="sf-label" for="{id}-path">Path</label>
        <input
          id="{id}-path"
          class="sf-input mono"
          type="text"
          placeholder="/path/to/board.md"
          spellcheck="false"
          autocomplete="off"
          value={path}
          oninput={(e) => onPath(e.currentTarget.value)}
          aria-invalid={errors.path ? 'true' : undefined}
          aria-describedby={described('path', `${id}-path-hint`)}
        />
        <p class="sf-hint" id="{id}-path-hint">A board that Waypoint made: a SQLite database or a Markdown file. Waypoint may tidy a Markdown file's format when it reads it.</p>
        {#if errors.path}<p class="sf-error" id="{id}-path-err">{errors.path}</p>{/if}
      </div>

      <div class="sf-field">
        <label class="sf-label" for="{id}-name">Name</label>
        <input
          id="{id}-name"
          class="sf-input"
          type="text"
          value={openName}
          oninput={(e) => {
            openName = e.currentTarget.value;
            openNameEdited = true;
            clear('name');
          }}
          autocomplete="off"
          aria-invalid={errors.name ? 'true' : undefined}
          aria-describedby={described('name')}
        />
        {#if errors.name}<p class="sf-error" id="{id}-name-err">{errors.name}</p>{/if}
      </div>
    {/if}

    <div class="actions">
      <button type="button" class="btn" onclick={onClose}>Cancel</button>
      <button type="submit" class="btn primary" disabled={working || (tab === 'create' && isServerEngine(storage) && !serverReady)}>{tab === 'create' ? 'Create project' : 'Open file'}</button>
    </div>
  </form>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    z-index: 80;
    inset: 0;
    border: 0;
    background: color-mix(in srgb, var(--bg) 72%, transparent);
  }

  .dialog {
    position: fixed;
    z-index: 81;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(460px, calc(100vw - 32px));
    max-height: calc(100dvh - 32px);
    overflow-y: auto;
    padding: 18px 20px 16px;
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-card);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .dialog:focus {
    outline: none;
  }

  h2 {
    margin: 0 0 12px;
    font-size: 15px;
    font-weight: 600;
  }

  .tabs {
    display: flex;
    gap: 2px;
    margin-bottom: 14px;
    border-bottom: 1px solid var(--line);
  }

  [role='tab'] {
    padding: 7px 12px;
    margin-bottom: -1px;
    border: 0;
    border-bottom: 2px solid transparent;
    background: none;
    font-size: 13px;
    color: var(--muted);
    cursor: pointer;
  }

  [role='tab'][aria-selected='true'] {
    border-bottom-color: var(--acc);
    color: var(--text);
    font-weight: 600;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .short {
    max-width: 200px;
  }

  .storage {
    margin: 0;
    padding: 0;
    border: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .storage legend {
    padding: 0;
    margin-bottom: 4px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 6px;
  }
</style>
