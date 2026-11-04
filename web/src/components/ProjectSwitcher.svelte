<script lang="ts">
  import { tick } from 'svelte';
  import CaretDown from 'phosphor-svelte/lib/CaretDown';
  import Check from 'phosphor-svelte/lib/Check';
  import FolderOpen from 'phosphor-svelte/lib/FolderOpen';
  import Plus from 'phosphor-svelte/lib/Plus';
  import Warning from 'phosphor-svelte/lib/Warning';
  import { accentColor } from '../lib/colors';
  import { go } from '../lib/link';
  import { href } from '../lib/router.svelte';
  import type { ProjectInfo } from '../lib/types';

  interface Props {
    projects: ProjectInfo[];
    current: ProjectInfo | null;
    onNewProject: () => void;
    onOpenExisting: () => void;
  }

  let { projects, current, onNewProject, onOpenExisting }: Props = $props();

  let open = $state(false);
  let root: HTMLElement | undefined = $state();
  let trigger: HTMLButtonElement | undefined = $state();
  let menu: HTMLElement | undefined = $state();

  function items(): HTMLElement[] {
    return menu ? Array.from(menu.querySelectorAll<HTMLElement>('[role^="menuitem"]')) : [];
  }

  async function show() {
    open = true;
    await tick();
    const all = items();
    (all.find((el) => el.getAttribute('aria-checked') === 'true') ?? all[0])?.focus();
  }

  function close(returnFocus = true) {
    open = false;
    if (returnFocus) trigger?.focus();
  }

  function onMenuKey(e: KeyboardEvent) {
    const all = items();
    const at = all.indexOf(document.activeElement as HTMLElement);
    let next: number | null = null;
    if (e.key === 'ArrowDown') next = (at + 1) % all.length;
    else if (e.key === 'ArrowUp') next = (at - 1 + all.length) % all.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = all.length - 1;
    else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab') close(false);
    if (next !== null) {
      e.preventDefault();
      all[next]?.focus();
    }
  }

  function onTriggerKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' && !open) {
      e.preventDefault();
      void show();
    }
  }

  function pick(e: MouseEvent & { currentTarget: HTMLAnchorElement }) {
    go(e);
    close(false);
  }

  function act(fn: () => void) {
    close(false);
    fn();
  }
</script>

<svelte:window
  onpointerdown={(e) => {
    if (open && root && !root.contains(e.target as Node)) close(false);
  }}
/>

<div class="switcher" bind:this={root}>
  <button
    class="trigger"
    bind:this={trigger}
    aria-haspopup="menu"
    aria-expanded={open}
    aria-label={current ? `Project: ${current.name}. Switch project` : 'Projects'}
    title="Switch project"
    onclick={() => (open ? close(false) : void show())}
    onkeydown={onTriggerKey}
  >
    {#if current}<span class="swatch" style:background={accentColor(current.accent)}></span>{/if}
    <span class="name mono">{current ? current.name : 'No project'}</span>
    <CaretDown size={12} weight="bold" aria-hidden="true" />
  </button>

  {#if open}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div class="menu" role="menu" aria-label="Projects" tabindex="-1" bind:this={menu} onkeydown={onMenuKey}>
      {#each projects as p (p.name)}
        <a
          class="item"
          role="menuitemradio"
          aria-checked={p.name === current?.name}
          href={href({ name: 'board', project: p.name, search: '' })}
          onclick={pick}
        >
          <span class="swatch" style:background={accentColor(p.accent)}></span>
          <span class="label">
            <span class="pname mono">{p.name}</span>
            {#if p.error}
              <span class="perr" title={p.error}><Warning size={12} weight="bold" aria-hidden="true" /><span>{p.error}</span></span>
            {/if}
          </span>
          {#if p.name === current?.name}<Check size={14} weight="bold" aria-hidden="true" />{/if}
        </a>
      {/each}
      {#if projects.length > 0}<hr />{/if}
      <button class="item" role="menuitem" onclick={() => act(onNewProject)}>
        <Plus size={14} weight="bold" aria-hidden="true" /><span class="label">New project</span>
      </button>
      <button class="item" role="menuitem" onclick={() => act(onOpenExisting)}>
        <FolderOpen size={14} aria-hidden="true" /><span class="label">Open existing file</span>
      </button>
    </div>
  {/if}
</div>

<style>
  .switcher {
    position: relative;
    margin: 4px 0 12px;
  }

  .trigger {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 28px;
    padding: 4px 8px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: none;
    font-size: 11px;
    color: var(--muted);
    cursor: pointer;
    text-align: left;
  }

  .trigger:hover,
  .trigger[aria-expanded='true'] {
    border-color: var(--line);
    background: color-mix(in srgb, var(--card) 70%, transparent);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .swatch {
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: 3px;
  }

  .item .swatch {
    align-self: flex-start;
    margin-top: 4px;
  }

  .menu {
    position: absolute;
    z-index: 20;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 1px;
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-control);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 28px;
    padding: 5px 8px;
    border: 0;
    border-radius: 7px;
    background: none;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .item:hover,
  .item:focus-visible {
    background: color-mix(in srgb, var(--text) 9%, transparent);
  }

  .item:focus-visible {
    outline-offset: -2px;
  }

  .label {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .pname {
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .perr {
    display: flex;
    gap: 5px;
    align-items: flex-start;
    font-size: 11px;
    line-height: 1.35;
    color: var(--red);
    overflow-wrap: anywhere;
  }

  /* A long error stays readable in the main area; here three lines are enough, and the title has the rest. */
  .perr span {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
  }

  .perr :global(svg) {
    flex: none;
    margin-top: 1px;
  }

  hr {
    width: 100%;
    margin: 3px 0;
    border: 0;
    border-top: 1px solid var(--line);
  }
</style>
