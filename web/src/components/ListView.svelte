<script lang="ts">
  import ArrowRight from 'phosphor-svelte/lib/ArrowRight';
  import MapPin from 'phosphor-svelte/lib/MapPin';
  import Prohibit from 'phosphor-svelte/lib/Prohibit';
  import type { BoardStore } from '../lib/board.svelte';
  import { groupColor } from '../lib/colors';
  import { go } from '../lib/link';
  import { href, navigate } from '../lib/router.svelte';
  import type { Blocker, CardView } from '../lib/types';
  import { blockedGroups, nextUp } from '../lib/views';
  import Card from './Card.svelte';
  import Notice from './Notice.svelte';

  interface Props {
    store: BoardStore;
    project: string;
    kind: 'next' | 'blocked';
    /** The empty board's action. */
    onNewCard: () => void;
  }

  let { store, project, kind, onNewCard }: Props = $props();

  // The whole board, whatever the filter, so a filter set on the board cannot hide work.
  const whole = $derived(store.baseline ?? store.view);
  const next = $derived(whole ? nextUp(whole) : null);
  const groups = $derived(whole ? blockedGroups(whole) : []);
  const blockedTotal = $derived(groups.reduce((n, g) => n + g.cards.length, 0));

  const doneColumns = $derived(new Set((whole?.settings.columns ?? []).filter((c) => c.done).map((c) => c.name)));
  const colors = $derived(new Map((whole?.settings.groups ?? []).map((g) => [g.name, groupColor(g.color)])));
  const topPriority = $derived(whole?.settings.priorities[0] ?? 'P0');

  const boardHref = $derived(href({ name: 'board', project, search: '' }));
  const cardHref = (id: string) => href({ name: 'card', project, id, search: '' });

  function open(id: string) {
    navigate(cardHref(id));
  }

  /** Cards are not in the tab order here: each has an Open button. Enter and Space still open a focused one. */
  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const id = e.target instanceof HTMLElement ? e.target.dataset.cardId : undefined;
    if (id === undefined) return;
    e.preventDefault();
    open(id);
  }

  // Focus lands on the heading when the view opens, and again when the other list view replaces it.
  let heading: HTMLElement | undefined = $state();
  $effect(() => {
    void kind;
    heading?.focus({ preventScroll: true });
  });

  const title = $derived(kind === 'next' ? 'Next up' : 'Blocked');
</script>

{#snippet entry(c: CardView)}
  <Card
    card={c}
    done={doneColumns.has(c.column)}
    top={c.priority !== '' && c.priority === topPriority}
    color={colors.get(c.group)}
    tabbable={false}
    dragging={false}
    drag={false}
    onOpen={open}
  />
{/snippet}

{#snippet blockerLink(b: Blocker)}
  {#if b.missing}
    <span class="mono miss">{b.id} (missing)</span>
  {:else}
    <a class="mono ref" href={cardHref(b.id)} onclick={go}>{b.id}</a>
  {/if}
{/snippet}

<!-- svelte-ignore a11y_no_static_element_interactions -->
<section class="view" aria-busy={whole === null} onkeydown={onKeydown}>
  <header class="head">
    <h2 bind:this={heading} tabindex="-1">{title}</h2>
    {#if kind === 'blocked' && whole && blockedTotal > 0}<span class="count mono">{blockedTotal}</span>{/if}
  </header>

  {#if whole && next}
    {#if next.total === 0}
      <Notice icon={MapPin} title="No cards yet. Add one with N.">
        {#snippet actions()}
          <button class="btn primary" onclick={onNewCard}>New card</button>
        {/snippet}
      </Notice>
    {:else if kind === 'next'}
      {#if next.next || next.queue.length > 0}
        {#if next.next}
          <div class="hero" data-next-card>
            {@render entry(next.next)}
            <div class="acts">
              <button class="btn primary" aria-label="Open {next.next.id}" onclick={() => open(next.next!.id)}>
                Open<ArrowRight size={14} weight="bold" aria-hidden="true" />
              </button>
            </div>
          </div>
        {/if}

        {#if next.queue.length > 0}
          <h3 class="sec">Then in {next.column}</h3>
          <ol class="list" data-queue>
            {#each next.queue as c (c.id)}
              <li>
                {@render entry(c)}
                <div class="acts">
                  <button class="btn" aria-label="Open {c.id}" onclick={() => open(c.id)}>Open</button>
                </div>
              </li>
            {/each}
          </ol>
          {#if next.more > 0}
            <p class="more">
              {next.more} more in {next.column}.
              <a class="ref-link" href={boardHref} onclick={go}>Open the board</a>
            </p>
          {/if}
        {/if}
      {:else}
        <Notice icon={MapPin} title="Nothing is waiting in {next.column ?? 'the first column'}.">
          {#snippet actions()}
            <a class="btn" href={boardHref} onclick={go}>Open the board</a>
          {/snippet}
        </Notice>
      {/if}
    {:else if groups.length === 0}
      <Notice icon={Prohibit} title="Nothing is blocked.">
        {#snippet actions()}
          <a class="btn" href={boardHref} onclick={go}>Open the board</a>
        {/snippet}
      </Notice>
    {:else}
      {#each groups as g (g.blocker.id)}
        <section class="group" data-blocker-group={g.blocker.id} aria-label="Waiting on {g.blocker.id}">
          <h3 class="gh">
            <Prohibit size={14} weight="bold" aria-hidden="true" />
            <span class="lbl">Waiting on</span>
            {@render blockerLink(g.blocker)}
            {#if g.title !== undefined}<span class="gt">{g.title}</span>{/if}
            {#if g.column !== undefined}<span class="gc">{g.column}</span>{/if}
          </h3>
          <ul class="list">
            {#each g.cards as c (c.id)}
              <li>
                {@render entry(c)}
                <div class="acts">
                  <button class="btn" aria-label="Open {c.id}" onclick={() => open(c.id)}>Open</button>
                  {#if c.blockers.length > 1}
                    <span class="also">
                      Also waiting on
                      {#each c.blockers.slice(1) as b, i (b.id)}{#if i > 0}, {/if}{@render blockerLink(b)}{/each}
                    </span>
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
        </section>
      {/each}
    {/if}
  {/if}
</section>

<style>
  .view {
    display: flex;
    flex-direction: column;
    gap: 14px;
    max-width: 640px;
    width: 100%;
    padding: 6px 0 24px;
  }

  .head {
    display: flex;
    gap: 10px;
    align-items: baseline;
  }

  h2 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  h2:focus {
    outline: none;
  }

  h2:focus-visible {
    outline: 2px solid var(--acc);
    outline-offset: 4px;
    box-shadow: none;
  }

  .count {
    font-size: 12px;
    color: var(--muted);
  }

  .sec {
    margin: 6px 0 -4px;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--muted);
  }

  /* The next card, larger, with the room its tag and askew edge need. */
  .hero {
    padding: 10px 10px 2px 2px;
  }

  .hero :global(.card) {
    --card-pad-y: 16px;
    --card-pad-x: 18px;
  }

  .hero :global(.t) {
    font-size: 22px;
    line-height: 1.2;
    letter-spacing: -0.01em;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 12px;
    align-items: center;
    margin: -2px 0 6px;
  }

  .acts .btn {
    min-height: 28px;
    padding: 4px 12px;
    font-size: 12px;
  }

  .hero .acts {
    margin-top: 6px;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 6px;
  }

  .gh {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 8px;
    align-items: center;
    margin: 0;
    font-size: 13px;
    font-weight: 600;
  }

  .gh :global(svg) {
    color: var(--red);
    flex: none;
  }

  .lbl {
    color: var(--muted);
    font-weight: 500;
  }

  .gt {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .gc {
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    font-size: 11px;
    font-weight: 500;
    color: var(--muted);
  }

  .ref {
    font-size: 12px;
    text-decoration: underline;
    text-underline-offset: 2px;
    text-decoration-color: var(--muted);
  }

  .ref:hover {
    text-decoration-color: var(--acc);
  }

  .miss {
    font-size: 12px;
    color: var(--red);
  }

  .also {
    font-size: 12px;
    color: var(--muted);
  }

  .more {
    margin: 0;
    font-size: 13px;
    color: var(--muted);
  }

  .ref-link {
    margin-left: 4px;
    color: var(--text);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
</style>
