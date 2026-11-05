<script lang="ts">
  import { tick, untrack } from 'svelte';
  import FileX from 'phosphor-svelte/lib/FileX';
  import MapPin from 'phosphor-svelte/lib/MapPin';
  import PlugsConnected from 'phosphor-svelte/lib/PlugsConnected';
  import Question from 'phosphor-svelte/lib/Question';
  import Board from './components/Board.svelte';
  import CardPanel from './components/CardPanel.svelte';
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
  import { href, navigate, route } from './lib/router.svelte';
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
  // Counted on the whole board, never the filtered view, so they stay true while a filter is on.
  const stats = $derived<BoardStats | null>(store?.baseline ? summarize(store.baseline) : null);

  const query = $derived(store?.query ?? '');

  function toggleGroup(name: string) {
    store?.setQuery(toggleGroupTerm(query, name));
  }

  // ----- Swimlane mode -----
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

  // ----- Quick add: open in one column at a time -----
  let adding = $state<string | null>(null);

  $effect(() => {
    void store;
    adding = null;
  });

  function addCard(column: string) {
    if (!store || store.readonly) return;
    adding = column;
  }

  /** New card: quick add in the first column. */
  function newCard() {
    const first = store?.view?.settings.columns[0]?.name;
    if (first !== undefined) addCard(first);
  }

  // ----- The card panel -----
  function closeCard() {
    if (route.project === undefined) return;
    navigate(href({ name: 'board', project: route.project, search: route.search }));
  }

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
        {#if route.name === 'home' || route.name === 'board' || route.name === 'card'}
          <Board {store} project={current.name} {lane} onAddCard={addCard} {adding} onCloseAdd={() => (adding = null)} />
        {:else}
          <h1 class="page">{viewTitle[route.name] ?? ''}</h1>
        {/if}
      {/if}
    </main>
  </div>

  {#if route.name === 'card' && route.id !== undefined && current && !current.error && store}
    {#key `${current.name}\n${route.id}`}
      <CardPanel {store} project={current.name} id={route.id} onClose={closeCard} />
    {/key}
  {/if}
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
    /* The board scrolls sideways inside this, and its columns need a little room for their
       focus rings and hard edges: the box is widened by 8px each side and padded back. */
    margin-inline: -8px;
    padding-inline: 8px;
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
