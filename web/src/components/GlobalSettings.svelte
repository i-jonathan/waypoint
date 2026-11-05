<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import ArrowsClockwise from 'phosphor-svelte/lib/ArrowsClockwise';
  import { api, ApiError } from '../lib/api';
  import { BUILTIN, lookOf } from '../lib/look';
  import { preview } from '../lib/preview.svelte';
  import type { Errors } from '../lib/settingsEdit';
  import type { Appearance, GlobalSettings, TlsMode } from '../lib/types';
  import AppearanceFields from './AppearanceFields.svelte';
  import UnsavedGuard from './UnsavedGuard.svelte';

  interface Props {
    client?: Pick<typeof api, 'settings' | 'putSettings'>;
    /** Called after a save, so the page can look again at what the change reaches. */
    onSaved?: () => void;
  }

  let { client = api, onSaved }: Props = $props();

  interface Form {
    port: string;
    open_browser: boolean;
    tls: TlsMode;
    appearance: Appearance;
  }

  const formOf = (g: GlobalSettings): Form => ({
    port: String(g.port),
    open_browser: g.open_browser,
    tls: g.tls,
    appearance: { ...g.appearance },
  });

  let loaded = $state.raw<GlobalSettings | null>(null);
  let loadError = $state('');
  let form = $state<Form | null>(null);
  let saving = $state(false);
  let saved = $state(false);
  let restartSaved = $state(false);
  /** The server's refusal of the last save: which field it is about, if any. */
  let refusal = $state<{ field: 'port' | null; message: string } | null>(null);
  let root: HTMLElement | undefined = $state();

  async function load() {
    try {
      const g = await client.settings();
      loaded = g;
      form = formOf(g);
      loadError = '';
    } catch (e) {
      loadError = e instanceof Error ? e.message : 'Could not load the settings';
    }
  }

  onMount(() => void load());
  onDestroy(() => preview.clear());

  const errors = $derived.by<Errors>(() => {
    const out: Errors = {};
    if (!form) return out;
    const n = Number(form.port);
    if (!/^\d+$/.test(form.port.trim()) || n < 1 || n > 65535) out['port'] = 'Use a whole number from 1 to 65535';
    else if (refusal?.field === 'port') out['port'] = refusal.message;
    const a = form.appearance.accent;
    if (a.startsWith('#') && !/^#[0-9a-f]{6}$/i.test(a)) out['accent'] = 'Use #rrggbb, for example #2f5bff';
    return out;
  });

  const dirty = $derived(
    form !== null &&
      loaded !== null &&
      JSON.stringify(form) !== JSON.stringify(formOf(loaded)),
  );
  const appearanceDirty = $derived(
    form !== null && loaded !== null && JSON.stringify(form.appearance) !== JSON.stringify(loaded.appearance),
  );

  const restartNeeded = $derived(
    restartSaved || (form !== null && loaded !== null && (Number(form.port) !== loaded.port || form.tls !== loaded.tls)),
  );

  // Try the look on while the appearance is being edited; nothing is kept until Save.
  $effect(() => {
    if (form && appearanceDirty) preview.set(lookOf(form.appearance, BUILTIN));
    else preview.clear();
  });

  function firstInvalid() {
    void tick().then(() => root?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());
  }

  async function save() {
    if (!form || saving) return;
    if (Object.keys(errors).length > 0) {
      firstInvalid();
      return;
    }
    saving = true;
    refusal = null;
    try {
      const res = await client.putSettings({
        port: Number(form.port),
        open_browser: form.open_browser,
        tls: form.tls,
        appearance: { ...form.appearance },
      });
      loaded = res;
      form = formOf(res);
      saved = true;
      if (res.restart_required) restartSaved = true;
      onSaved?.();
    } catch (e) {
      const message = e instanceof Error && e.message !== '' ? e.message : 'Something went wrong';
      refusal = { field: e instanceof ApiError && /\bport\b/i.test(message) ? 'port' : null, message };
      if (refusal.field === 'port') firstInvalid();
    } finally {
      saving = false;
    }
  }

  function cancel() {
    if (loaded) form = formOf(loaded);
    refusal = null;
    saved = false;
    preview.clear();
  }

  const builtin = (f: keyof Appearance) => ({ value: BUILTIN[f], source: 'default' as const });
</script>

<div class="sf-page" bind:this={root}>
  <div>
    <h1>App settings</h1>
    <p class="sf-sub">These apply to every project, unless a project sets its own.</p>
  </div>

  {#if loadError}
    <p class="sf-note" role="alert">{loadError} <button type="button" class="btn" onclick={load}>Try again</button></p>
  {:else if form === null}
    <p class="sf-sub" role="status">Loading</p>
  {:else}
    <section class="sf-section" aria-labelledby="gs-look">
      <h2 id="gs-look">Appearance</h2>
      <AppearanceFields bind:value={form.appearance} inherit={builtin} resettable={false} swimlaneLabel="Default swimlane" {errors} />
    </section>

    <section class="sf-section" aria-labelledby="gs-server">
      <h2 id="gs-server">Server</h2>

      <div class="sf-field port">
        <label class="sf-label" for="gs-port">Port</label>
        <input
          id="gs-port"
          class="sf-input mono"
          type="text"
          inputmode="numeric"
          bind:value={form.port}
          oninput={() => (refusal = null)}
          aria-invalid={errors['port'] ? 'true' : undefined}
          aria-describedby={errors['port'] ? 'gs-port-err' : undefined}
        />
        {#if errors['port']}<p class="sf-error" id="gs-port-err">{errors['port']}</p>{/if}
      </div>

      <label class="sf-check">
        <input type="checkbox" bind:checked={form.open_browser} />
        <span>Open the browser when Waypoint starts</span>
      </label>

      <fieldset class="tls">
        <legend class="sf-label">TLS</legend>
        <div class="choices">
          {#each [['auto', 'Auto'], ['on', 'On'], ['off', 'Off']] as [v, label] (v)}
            <label class="sf-check">
              <input type="radio" name="gs-tls" value={v} checked={form.tls === v} onchange={() => form && (form.tls = v as TlsMode)} />
              <span>{label}</span>
            </label>
          {/each}
        </div>
        <p class="sf-hint">Browsers need HTTPS to use HTTP/2, and running "waypoint trust" once enables it.</p>
      </fieldset>

      {#if restartNeeded}
        <p class="sf-note" role="status"><ArrowsClockwise size={14} weight="bold" aria-hidden="true" /><span>Restart "waypoint serve" to apply.</span></p>
      {/if}
    </section>

    {#if refusal && refusal.field === null}
      <p class="sf-error" role="alert">{refusal.message}</p>
    {/if}

    <div class="sf-bar">
      <span class="status" class:dirty role="status">{saving ? 'Saving' : dirty ? 'Unsaved changes' : saved ? 'Saved' : ''}</span>
      <button type="button" class="btn" disabled={!dirty || saving} onclick={cancel}>Cancel</button>
      <button type="button" class="btn primary" disabled={!dirty || saving} onclick={save}>Save</button>
    </div>
  {/if}
</div>

<UnsavedGuard {dirty} />

<style>
  .port {
    max-width: 160px;
  }

  .tls {
    margin: 0;
    padding: 0;
    border: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .tls legend {
    padding: 0;
    margin-bottom: 4px;
  }

  .choices {
    display: flex;
    gap: 18px;
  }
</style>
