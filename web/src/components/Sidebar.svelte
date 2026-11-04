<script lang="ts">
  import type { Component } from 'svelte';
  import Archive from 'phosphor-svelte/lib/Archive';
  import CircleNotch from 'phosphor-svelte/lib/CircleNotch';
  import GearSix from 'phosphor-svelte/lib/GearSix';
  import Kanban from 'phosphor-svelte/lib/Kanban';
  import MapPin from 'phosphor-svelte/lib/MapPin';
  import NavigationArrow from 'phosphor-svelte/lib/NavigationArrow';
  import Prohibit from 'phosphor-svelte/lib/Prohibit';
  import type { BoardStats } from '../lib/boardStats';
  import { groupColor } from '../lib/colors';
  import { hasGroupTerm } from '../lib/filter';
  import { go } from '../lib/link';
  import { href, type RouteName } from '../lib/router.svelte';
  import type { Group, ProjectInfo } from '../lib/types';
  import ProjectSwitcher from './ProjectSwitcher.svelte';

  interface Props {
    projects: ProjectInfo[];
    /** The project on screen, or null before there is one. */
    current: ProjectInfo | null;
    routeName: RouteName;
    groups: Group[];
    /** Null until a board has loaded. */
    stats: BoardStats | null;
    query: string;
    /** Shows the Reconnecting line. */
    reconnecting: boolean;
    /** Below 720px the sidebar is a drawer; this says whether it is out. */
    open: boolean;
    onToggleGroup: (name: string) => void;
    onNewProject: () => void;
    onOpenExisting: () => void;
  }

  let {
    projects,
    current,
    routeName,
    groups,
    stats,
    query,
    reconnecting,
    open,
    onToggleGroup,
    onNewProject,
    onOpenExisting,
  }: Props = $props();

  interface NavItem {
    route: RouteName;
    label: string;
    icon: Component<{ size?: number; 'aria-hidden'?: 'true' }>;
    count?: number;
  }

  // The card route is the board with a panel open, so it keeps Board lit.
  const active = $derived(routeName === 'card' ? 'board' : routeName);

  const nav: NavItem[] = $derived([
    { route: 'board', label: 'Board', icon: Kanban, ...(stats ? { count: stats.total } : {}) },
    { route: 'next', label: 'Next up', icon: NavigationArrow },
    { route: 'blocked', label: 'Blocked', icon: Prohibit, ...(stats ? { count: stats.blocked } : {}) },
    { route: 'archive', label: 'Archive', icon: Archive },
  ]);

  const settingsHref = $derived(
    current
      ? href({ name: 'projectSettings', project: current.name, search: '' })
      : href({ name: 'settings', search: '' }),
  );
  const settingsActive = $derived(routeName === 'settings' || routeName === 'projectSettings');
</script>

<aside id="sidebar" class="rail" class:open aria-label="Sidebar">
  <div class="brand sign"><MapPin size={20} weight="fill" aria-hidden="true" /><span>Waypoint</span></div>

  <ProjectSwitcher {projects} {current} {onNewProject} {onOpenExisting} />

  {#if current}
    <nav aria-label="Project">
      {#each nav as item (item.route)}
        <a
          class="rl"
          class:on={active === item.route}
          aria-current={active === item.route ? 'page' : undefined}
          href={href({ name: item.route, project: current.name, search: '' })}
          onclick={go}
        >
          <item.icon size={16} aria-hidden="true" />
          <span>{item.label}</span>
          {#if item.count !== undefined}<span class="n mono">{item.count}</span>{/if}
        </a>
      {/each}
    </nav>

    {#if groups.length > 0}
      <section class="routes">
        <h2 class="sec" aria-label="Groups" title="Groups">Routes</h2>
        <ul>
          {#each groups as g (g.name)}
            {@const on = hasGroupTerm(query, g.name)}
            {@const count = stats?.groups[g.name]}
            <li>
              <button class="rl" class:picked={on} aria-pressed={on} title={g.name} onclick={() => onToggleGroup(g.name)}>
                <span class="route" style:background={groupColor(g.color)}></span>
                <span class="gname">{g.name}</span>
                <span class="n mono">{count ? `${count.done}/${count.total}` : ''}</span>
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  {/if}

  <div class="grow"></div>

  <div class="status" role="status" aria-live="polite">
    {#if reconnecting}
      <span class="spin"><CircleNotch size={14} weight="bold" aria-hidden="true" /></span>
      <span>Reconnecting</span>
    {/if}
  </div>

  <a
    class="rl"
    class:on={settingsActive}
    aria-current={settingsActive ? 'page' : undefined}
    href={settingsHref}
    onclick={go}
  >
    <GearSix size={16} aria-hidden="true" /><span>Settings</span>
  </a>
</aside>

<style>
  .rail {
    width: 214px;
    flex: none;
    height: 100%;
    overflow-y: auto;
    padding: 16px 14px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-right: 1px solid var(--line);
    background: var(--rail);
    backdrop-filter: blur(6px);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 22px;
    font-stretch: 72%;
    letter-spacing: 0.005em;
    padding-left: 4px;
  }

  .brand :global(svg) {
    color: var(--acc);
  }

  nav,
  ul {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .rl {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 28px;
    padding: 6px 8px;
    border: 0;
    border-radius: 8px;
    background: none;
    font-size: 13px;
    color: var(--muted);
    text-align: left;
    cursor: pointer;
  }

  .rl:hover {
    background: color-mix(in srgb, var(--text) 8%, transparent);
    color: var(--text);
  }

  .rl.on,
  .rl.on:hover {
    background: var(--acc);
    color: var(--acc-ink);
    font-weight: 600;
  }

  .rl :global(svg) {
    flex: none;
  }

  .n {
    margin-left: auto;
    font-size: 11px;
  }

  .sec {
    margin: 14px 8px 4px;
    font-size: 11px;
    font-weight: 500;
    color: var(--muted);
  }

  .route {
    width: 16px;
    height: 4px;
    border-radius: 2px;
    flex: none;
  }

  .gname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* A group that is in the filter: lit with the text colour, never the accent, which marks where you are. */
  .rl.picked {
    background: color-mix(in srgb, var(--text) 11%, transparent);
    color: var(--text);
    font-weight: 600;
    box-shadow: inset 0 0 0 1px var(--line);
  }

  .grow {
    flex: 1;
    min-height: 12px;
  }

  .status {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    padding: 0 8px;
    font-size: 12px;
    color: var(--muted);
  }

  .spin {
    display: inline-flex;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (max-width: 719px) {
    .rail {
      position: fixed;
      z-index: 30;
      top: 0;
      left: 0;
      bottom: 0;
      width: min(280px, 86vw);
      background: var(--bg);
      box-shadow: 4px 0 0 var(--edge);
      visibility: hidden;
    }

    .rail.open {
      visibility: visible;
    }
  }
</style>
