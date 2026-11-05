<script lang="ts">
  import type { Component, Snippet } from 'svelte';

  interface Props {
    icon: Component<{ size?: number; weight?: 'bold' | 'regular' | 'fill'; 'aria-hidden'?: 'true' }>;
    title: string;
    children?: Snippet;
    actions?: Snippet;
  }

  let { icon: Icon, title, children, actions }: Props = $props();
</script>

<section class="notice">
  <div class="icon"><Icon size={28} weight="bold" aria-hidden="true" /></div>
  <h2>{title}</h2>
  {#if children}<div class="text">{@render children()}</div>{/if}
  {#if actions}<div class="actions">{@render actions()}</div>{/if}
</section>

<style>
  .notice {
    max-width: 440px;
    margin: 8vh auto 0;
    padding: 24px 24px 22px;
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-card);
    box-shadow: 3px 3px 0 var(--edge);
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }

  .icon {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    margin-bottom: 4px;
    border-radius: var(--r-sign);
    background: var(--acc);
    color: var(--acc-ink);
  }

  h2 {
    margin: 0;
    font-size: 22px;
    line-height: 1.15;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .text {
    font-size: 14px;
    line-height: 1.5;
    color: var(--muted);
    overflow-wrap: anywhere;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 10px;
  }
</style>
