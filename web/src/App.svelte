<script lang="ts">
  import { tick, untrack } from 'svelte';
  import FileX from 'phosphor-svelte/lib/FileX';
  import MapPin from 'phosphor-svelte/lib/MapPin';
  import PlugsConnected from 'phosphor-svelte/lib/PlugsConnected';
  import Question from 'phosphor-svelte/lib/Question';
  import Notice from './components/Notice.svelte';
  import ParseBanner from './components/ParseBanner.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import Toast from './components/Toast.svelte';
  import TopBar from './components/TopBar.svelte';
  import { api, ApiError } from './lib/api';
  import { createBoardStore, type BoardStore } from './lib/board.svelte';
  import { summarize, type BoardStats } from './lib/boardStats';
  import { connectEvents, toasts } from './lib/events.svelte';
  import { toggleGroupTerm } from './lib/filter';
  import { navigate, route } from './lib/router.svelte';
  import { applyLook, DEFAULT_LOOK, loadLook, saveLook, systemPrefersLight, watchSystemTheme, type Look } from './lib/theme';
  import type { ProjectInfo, Swimlane } from './lib/types';

  // ----- Projects -----
  let projects = $state.raw<ProjectInfo[] | null>(null);
  let projectsFailed = $state(false);
  let projectsOffline = $state(false);
  let projectsSeq = 0;

  async function loadProjects() {
    const seq = ++projectsSeq;
    try {
      const list = await api.projects();
      if (seq !== projectsSeq) return;
      projects = list;
      projectsFailed = false;
      projectsOffline = false;
    } catch (e) {
      if (seq !== projectsSeq) return;
      projectsFailed = true;
      projectsOffline = e instanceof ApiError && e.status === 0;
      if (!projectsOffline) toasts.push(e instanceof Error ? e.message : 'Could not load the projects');
    }
  }

  // The route's project; with none in the route, the first project that opens.
  const current = $derived.by(() => {
    if (!projects) return null;
    if (route.project !== undefined) return projects.find((p) => p.name === route.project) ?? null;
    return projects.find((p) => !p.error) ?? projects[0] ?? null;
  });
  const missing = $derived(projects !== null && route.project !== undefined && current === null);
  const activeName = $derived(current && !current.error ? current.name : null);

  // ----- The board store: one per project, made new on a switch. -----
  let store = $state.raw<BoardStore | null>(null);

  $effect(() => {
    const name = activeName;
    if (name === null) {
      store = null;
      return;
    }
    const s = createBoardStore(api, name);
    store = s;
    void s.refresh();
    return () => s.destroy();
  });

  // ----- Live events: one stream for every project. -----
  let events = $state.raw<{ readonly status: 'open' | 'reconnecting' } | null>(null);

  $effect(() => {
    const stream = connectEvents((e) => {
      // A stream that came back may have missed anything, which it reports as project '*'.
      if (e.type === 'projects' || e.project === '*') void loadProjects();
      store?.handleEvent(e);
    });
    events = stream;
    void loadProjects();
    return () => stream.close();
  });

  const reconnecting = $derived(events?.status === 'reconnecting' || (store?.offline ?? false) || projectsOffline);

  // ----- Look: theme, accent, density, textures -----
  let systemLight = $state(systemPrefersLight());
  $effect(() => watchSystemTheme((light) => (systemLight = light)));

  // The look the last board asked for. It stays while another board loads, so a switch does not flash.
  let base = $state<Look>(loadLook() ?? DEFAULT_LOOK);

  $effect(() => {
    const a = store?.appearance;
    if (!a) return;
    const same = untrack(() => a.theme === base.theme && a.accent === base.accent && a.density === base.density && a.textures === base.textures);
    if (!same) base = { theme: a.theme, accent: a.accent, density: a.density, textures: a.textures };
  });

  // The project's accent is known from the project list before its board has loaded.
  const look = $derived<Look>({ ...base, accent: store?.appearance?.accent ?? current?.accent ?? base.accent });

  $effect(() => {
    applyLook(look, systemLight);
    saveLook(look);
  });

  $effect(() => {
    document.title = current ? `${current.name} | Waypoint` : 'Waypoint';
  });

  // ----- Sidebar numbers -----
  // Counts come from the whole board. While a filter is on, the view holds only
  // the matches, so the last unfiltered counts stay.
  let stats = $state.raw<BoardStats | null>(null);

  $effect(() => {
    const s = store;
    const view = s?.view;
    if (!s || !view) {
      stats = null;
      return;
    }
    if (untrack(() => s.query) === '') stats = summarize(view);
  });

  const query = $derived(store?.query ?? '');

  function toggleGroup(name: string) {
    store?.setQuery(toggleGroupTerm(query, name));
  }

  // ----- Swimlane mode (the board is built in the next task; this holds the choice) -----
  let lane = $state<Swimlane>('none');
  const laneSetting = $derived(store?.appearance?.swimlane ?? 'none');
  $effect(() => {
    lane = laneSetting;
  });

  // ----- Narrow screens: the sidebar is a drawer -----
  let navOpen = $state(false);

  $effect(() => {
    void route.name;
    void route.project;
    navOpen = false;
  });

  async function toggleMenu() {
    navOpen = !navOpen;
    if (navOpen) {
      await tick();
      document.querySelector<HTMLElement>('#sidebar button, #sidebar a')?.focus();
    }
  }

  function closeMenu() {
    navOpen = false;
    document.getElementById('menu-button')?.focus();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && navOpen) closeMenu();
  }

  function skipToBoard(e: MouseEvent) {
    e.preventDefault();
    document.getElementById('board')?.focus();
  }

  // The dialogs for these two arrive with the settings screens; until then they open Settings.
  function newProject() {
    navigate('/settings');
  }
  function openExisting() {
    navigate('/settings');
  }
  function newCard() {
    // The card panel arrives with the board.
  }

  const counts = $derived.by(() => {
    const v = store?.view;
    if (!v) return new Map<string, number>();
    const m = new Map<string, number>();
    for (const c of v.cards) m.set(c.column, (m.get(c.column) ?? 0) + 1);
    return m;
  });

  const viewTitle: Partial<Record<string, string>> = { next: 'Next up', blocked: 'Blocked', archive: 'Archive' };
</script>

<svelte:window onkeydown={onKeydown} />

<a class="skip" href="#board" onclick={skipToBoard}>Skip to the board</a>

<div class="shell">
  <Sidebar
    projects={projects ?? []}
    {current}
    routeName={route.name}
    groups={store?.view?.settings.groups ?? []}
    {stats}
    {query}
    {reconnecting}
    open={navOpen}
    onToggleGroup={toggleGroup}
    onNewProject={newProject}
    onOpenExisting={openExisting}
  />
  {#if navOpen}
    <button class="scrim" tabindex="-1" aria-label="Close menu" onclick={closeMenu}></button>
  {/if}

  <div class="stage">
    <TopBar
      active={store !== null}
      {query}
      {lane}
      readonly={store?.readonly ?? true}
      menuOpen={navOpen}
      onQuery={(q) => store?.setQuery(q)}
      onLane={(l) => (lane = l)}
      onNewCard={newCard}
      onMenu={toggleMenu}
    />

    <main id="board" tabindex="-1" aria-busy={projects === null && !projectsFailed}>
      {#if projects === null}
        {#if projectsFailed}
          <Notice icon={PlugsConnected} title="Waypoint can't be reached">
            The server isn't answering. This page will try again by itself.
            {#snippet actions()}
              <button class="btn" onclick={loadProjects}>Try again</button>
            {/snippet}
          </Notice>
        {/if}
      {:else if projects.length === 0}
        <Notice icon={MapPin} title="Start your first board">
          A project is one Markdown file. Make a new one, or open a board file you already have.
          {#snippet actions()}
            <button class="btn primary" onclick={newProject}>New project</button>
            <button class="btn" onclick={openExisting}>Open existing file</button>
          {/snippet}
        </Notice>
      {:else if missing}
        <Notice icon={Question} title="No project called {route.project}">
          Pick a project from the list in the sidebar.
        </Notice>
      {:else if current?.error}
        <Notice icon={FileX} title="{current.name} can't be opened">
          <p class="why">{current.error}</p>
          {#if !current.error.includes(current.path)}<p class="where mono">{current.path}</p>{/if}
        </Notice>
      {:else if route.name === 'settings' || route.name === 'projectSettings'}
        <h1 class="page">Settings</h1>
      {:else if current && store}
        <h1 class="sr-only">{current.name}</h1>
        {#if store.parseError}<ParseBanner error={store.parseError} />{/if}
        {#if route.name === 'board' || route.name === 'card'}
          <!-- A stand-in for the board, which is built in the next task. -->
          <ul class="cols" aria-label="Columns">
            {#each store.view?.settings.columns ?? [] as col (col.name)}
              <li>
                <span class="sign colsign">{col.name}</span>
                <span class="mono count">{counts.get(col.name) ?? 0}</span>
              </li>
            {/each}
          </ul>
        {:else}
          <h1 class="page">{viewTitle[route.name] ?? ''}</h1>
        {/if}
      {/if}
    </main>
  </div>
</div>

<Toast />

<style>
  .skip {
    position: fixed;
    z-index: 100;
    top: 8px;
    left: 8px;
    padding: 8px 14px;
    border-radius: var(--r-control);
    background: var(--acc);
    color: var(--acc-ink);
    font-size: 13px;
    font-weight: 600;
    transform: translateY(-200%);
  }

  .skip:focus-visible {
    transform: none;
  }

  .shell {
    display: flex;
    height: 100dvh;
    overflow: hidden;
  }

  .stage {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px 18px;
  }

  main {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 16px;
    overflow-y: auto;
    overflow-x: hidden;
  }

  main:focus {
    outline: none;
  }

  .scrim {
    position: fixed;
    z-index: 25;
    inset: 0;
    border: 0;
    background: color-mix(in srgb, var(--bg) 70%, transparent);
  }

  .page {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .why,
  .where {
    margin: 0;
  }

  .why {
    color: var(--text);
  }

  .where {
    margin-top: 6px;
    font-size: 12px;
  }

  .cols {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: var(--col-gap);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .cols li {
    display: flex;
    align-items: center;
    padding: 7px 11px 6px;
    border-radius: var(--r-sign);
    background: var(--head);
    color: var(--headtext);
  }

  .colsign {
    font-size: 22px;
  }

  .count {
    margin-left: auto;
    font-size: 11px;
    opacity: 0.75;
  }

  @media (min-width: 720px) {
    .scrim {
      display: none;
    }
  }

  @media (max-width: 719px) {
    .stage {
      padding: 12px 16px;
      gap: 12px;
    }
  }
</style>
