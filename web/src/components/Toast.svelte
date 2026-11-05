<script lang="ts">
  import Info from 'phosphor-svelte/lib/Info';
  import Warning from 'phosphor-svelte/lib/Warning';
  import X from 'phosphor-svelte/lib/X';
  import { toasts } from '../lib/events.svelte';

  const SHOWN_MS = { error: 8000, info: 4000 };

  // One timer per toast, set when it appears and cleared when it goes.
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

  $effect(() => {
    const live = new Set<number>();
    for (const t of toasts.items) {
      live.add(t.id);
      if (!timers.has(t.id)) timers.set(t.id, setTimeout(() => toasts.dismiss(t.id), SHOWN_MS[t.kind]));
    }
    for (const [id, timer] of timers) {
      if (!live.has(id)) {
        clearTimeout(timer);
        timers.delete(id);
      }
    }
  });

  $effect(() => () => {
    for (const timer of timers.values()) clearTimeout(timer);
    timers.clear();
  });
</script>

<div class="toasts" role="region" aria-label="Notifications" aria-live="polite">
  {#each toasts.items as t (t.id)}
    <div class="toast" class:error={t.kind === 'error'}>
      {#if t.kind === 'error'}
        <Warning size={16} weight="bold" aria-hidden="true" />
      {:else}
        <Info size={16} weight="bold" aria-hidden="true" />
      {/if}
      <p>{t.msg}</p>
      <button aria-label="Dismiss" title="Dismiss" onclick={() => toasts.dismiss(t.id)}>
        <X size={14} weight="bold" aria-hidden="true" />
      </button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    z-index: 50;
    right: 18px;
    bottom: 18px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    width: min(360px, calc(100vw - 32px));
    pointer-events: none;
  }

  .toast {
    pointer-events: auto;
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 10px 8px 10px 12px;
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-control);
    box-shadow: 3px 3px 0 var(--edge);
    font-size: 13px;
    line-height: 1.4;
  }

  .toast > :global(svg) {
    flex: none;
    margin-top: 1px;
  }

  .toast.error > :global(svg) {
    color: var(--red);
  }

  p {
    flex: 1;
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }

  button {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  button:hover {
    color: var(--text);
    background: color-mix(in srgb, var(--text) 10%, transparent);
  }

  @media (max-width: 719px) {
    .toasts {
      right: 16px;
      left: 16px;
      width: auto;
    }
  }
</style>
