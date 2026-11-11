<script lang="ts">
  import { api } from '../lib/api';
  import { DEFAULT_PORTS, GLOBAL_PASSWORD_VAR, NON_LOCAL_WARNING, SSL_CHOICES, isLocalHost, passwordVar, type ServerEngine } from '../lib/server';
  import type { DbTestResult } from '../lib/types';

  /** What the owner typed. The password is not here, and never will be. */
  export interface ServerSettings {
    engine: ServerEngine;
    host: string;
    port: string;
    user: string;
    database: string;
    ssl: string;
    createDatabase: boolean;
  }

  interface Props {
    /** The project these settings are for: the password variable is named after it. */
    project: string;
    value: ServerSettings;
    /** True when the connection was tested and everything needed to go on is in order. */
    ready?: boolean;
    /** "create" accepts an empty database or one that holds a Waypoint board; "convert" only an empty one. */
    mode?: 'create' | 'convert';
    client?: Pick<typeof api, 'testDatabase'>;
  }

  let { project, value = $bindable(), ready = $bindable(false), mode = 'create', client = api }: Props = $props();

  const id = `sv-${Math.random().toString(36).slice(2, 7)}`;

  let result = $state.raw<DbTestResult | null>(null);
  let failure = $state('');
  let testing = $state(false);
  let understood = $state(false);

  const host = $derived(value.host.trim() === '' ? 'localhost' : value.host.trim());
  const nonLocal = $derived(!isLocalHost(host));
  const port = $derived(value.port.trim() === '' ? DEFAULT_PORTS[value.engine] : Number(value.port));
  const projectVar = $derived(result?.password_var ?? passwordVar(project));
  const globalVar = $derived(result?.global_password_var ?? GLOBAL_PASSWORD_VAR);

  /** Every edit makes an earlier test say nothing about what is typed now. */
  function touch() {
    result = null;
    failure = '';
  }

  async function test() {
    if (testing) return;
    failure = '';
    if (value.user.trim() === '') {
      failure = 'Enter the database user';
      return;
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      failure = 'The port must be a number from 1 to 65535';
      return;
    }
    testing = true;
    try {
      result = await client.testDatabase({
        engine: value.engine,
        project,
        ...(mode === 'convert' ? { convert: true } : {}),
        host,
        port,
        user: value.user.trim(),
        database: value.database.trim(),
        ssl: value.ssl,
      });
    } catch (err) {
      result = null;
      failure = err instanceof Error && err.message !== '' ? err.message : 'Could not test the connection';
    } finally {
      testing = false;
    }
  }

  /** What the test found, in words, and whether it allows going on. */
  const verdict = $derived.by((): { ok: boolean; text: string } | null => {
    const r = result;
    if (!r) return null;
    if (!r.reachable) return { ok: false, text: r.error ?? 'The server could not be reached' };
    if (!r.database_exists) {
      return value.createDatabase
        ? { ok: true, text: 'Connected. The database does not exist yet and will be created.' }
        : { ok: false, text: 'Connected, but the database does not exist. Tick the box below to have it created.' };
    }
    if (r.has_board) {
      return mode === 'create'
        ? { ok: true, text: 'Connected. This database already holds a Waypoint board, which will be opened as it is.' }
        : { ok: false, text: 'Connected, but this database already holds a Waypoint board. Convert needs an empty database.' };
    }
    if (!r.empty) return { ok: false, text: 'Connected, but this database holds other tables. Choose an empty database.' };
    return { ok: true, text: 'Connected. The database is empty.' };
  });

  // A test, and a tick on the warning, say nothing about a different engine,
  // project or host: forget them when any of those changes.
  let watched = '';
  $effect(() => {
    const key = `${value.engine}|${project}|${host}`;
    if (watched !== '' && key !== watched) {
      result = null;
      failure = '';
      understood = false;
    }
    watched = key;
  });

  $effect(() => {
    ready = verdict?.ok === true && (!nonLocal || understood);
  });
</script>

<div class="server">
  <div class="row">
    <div class="sf-field grow">
      <label class="sf-label" for="{id}-host">Host</label>
      <input id="{id}-host" class="sf-input mono" type="text" placeholder="localhost" spellcheck="false" autocomplete="off" bind:value={value.host} oninput={touch} />
    </div>
    <div class="sf-field port">
      <label class="sf-label" for="{id}-port">Port</label>
      <input id="{id}-port" class="sf-input mono" type="text" inputmode="numeric" placeholder={String(DEFAULT_PORTS[value.engine])} autocomplete="off" bind:value={value.port} oninput={touch} />
    </div>
  </div>

  <div class="row">
    <div class="sf-field grow">
      <label class="sf-label" for="{id}-user">User</label>
      <input id="{id}-user" class="sf-input mono" type="text" spellcheck="false" autocomplete="off" bind:value={value.user} oninput={touch} />
    </div>
    <div class="sf-field grow">
      <label class="sf-label" for="{id}-database">Database</label>
      <input id="{id}-database" class="sf-input mono" type="text" placeholder="the project name" spellcheck="false" autocomplete="off" bind:value={value.database} oninput={touch} />
    </div>
  </div>

  <div class="sf-field">
    <label class="sf-label" for="{id}-ssl">Encryption</label>
    <select id="{id}-ssl" class="sf-input" bind:value={value.ssl} onchange={touch}>
      {#each SSL_CHOICES as c (c.value)}<option value={c.value}>{c.label}</option>{/each}
    </select>
    {#if value.ssl === 'disable' && nonLocal}
      <p class="sf-error">With encryption off, the password and the board cross the network unencrypted.</p>
    {/if}
  </div>

  <label class="sf-check">
    <input type="checkbox" bind:checked={value.createDatabase} />
    <span>Create the database if it does not exist</span>
  </label>

  <div class="sf-note password" role="note">
    <p>
      Waypoint never stores the password. Before you start Waypoint, set <code>{projectVar}</code> to the password of this project's database user, or
      <code>{globalVar}</code> to use one password for every project. The first one wins when both are set.
    </p>
  </div>

  {#if nonLocal}
    <div class="sf-note warn" role="alert">
      <div>
        <p>{NON_LOCAL_WARNING}</p>
        <label class="sf-check">
          <input type="checkbox" bind:checked={understood} />
          <span>I understand</span>
        </label>
      </div>
    </div>
  {/if}

  <div class="test">
    <button type="button" class="btn" disabled={testing} onclick={() => void test()}>{testing ? 'Testing' : 'Test connection'}</button>
    {#if verdict}
      <p class={verdict.ok ? 'sf-hint ok' : 'sf-error'} role={verdict.ok ? 'status' : 'alert'}>{verdict.text}</p>
    {:else if failure}
      <p class="sf-error" role="alert">{failure}</p>
    {:else}
      <p class="sf-hint">Test the connection before you continue.</p>
    {/if}
  </div>
</div>

<style>
  .server {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .row {
    display: flex;
    gap: 10px;
  }

  .grow {
    flex: 1 1 0;
    min-width: 0;
  }

  .port {
    flex: 0 0 96px;
  }

  .password p,
  .warn p {
    margin: 0 0 6px;
  }

  .password code {
    font-family: var(--mono, ui-monospace, monospace);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  .warn {
    border-color: var(--red);
  }

  .test {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .ok {
    color: var(--text);
  }
</style>
