<script lang="ts">
  import ArrowRight from 'phosphor-svelte/lib/ArrowRight';
  import Check from 'phosphor-svelte/lib/Check';
  import CheckSquare from 'phosphor-svelte/lib/CheckSquare';
  import DotsSixVertical from 'phosphor-svelte/lib/DotsSixVertical';
  import Prohibit from 'phosphor-svelte/lib/Prohibit';
  import type { CardView } from '../lib/types';

  interface Props {
    card: CardView;
    /** The card is in a done column. */
    done: boolean;
    /** The card's priority is the board's highest, which is drawn as a filled chip. */
    top: boolean;
    /** A CSS colour for the group's route line, when the card has a group. */
    color?: string;
    /** The one card that is in the tab order. */
    tabbable: boolean;
    /** This card is being dragged: its place stays, faded. */
    dragging: boolean;
    onOpen: (id: string) => void;
    onPointerDown: (e: PointerEvent, id: string, el: HTMLElement) => void;
  }

  let { card, done, top, color, tabbable, dragging, onOpen, onPointerDown }: Props = $props();

  const blockers = $derived(card.blockers.map((b) => (b.missing ? `${b.id} (missing)` : b.id)));

  const label = $derived(
    `${card.id} ${card.title}, ${card.column}` +
      (blockers.length > 0 ? `, blocked by ${blockers.join(', ')}` : '') +
      (card.next ? ', next up' : ''),
  );

  const hasMeta = $derived(card.group !== '' || card.checklist.total > 0);
</script>

<!-- Enter and Space open the card from the board's key handler, which every card bubbles to. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="card"
  class:next={card.next}
  class:done
  class:ghost={dragging}
  role="button"
  tabindex={tabbable ? 0 : -1}
  data-card-id={card.id}
  aria-label={label}
  aria-keyshortcuts="M"
  onpointerdown={(e) => onPointerDown(e, card.id, e.currentTarget)}
  onclick={() => onOpen(card.id)}
>
  {#if card.next}
    <span class="tag"><ArrowRight size={11} weight="bold" aria-hidden="true" />NEXT</span>
  {/if}

  <div class="id mono">
    {#if done}<Check size={12} weight="bold" aria-hidden="true" />{/if}
    <span>{card.id}</span>
    {#if card.priority !== ''}<span class={top ? 'p0' : 'p1'}>{card.priority}</span>{/if}
    <span class="grip" data-drag-handle aria-hidden="true"><DotsSixVertical size={16} weight="bold" /></span>
  </div>

  <div class="t" class:struck={done}>{card.title}</div>

  {#if hasMeta}
    <div class="m">
      {#if card.group !== ''}
        <span class="route" style:background={color ?? 'var(--muted)'} aria-hidden="true"></span>
        <span class="group">{card.group}</span>
      {/if}
      {#if card.checklist.total > 0}
        <span class="check" title="Checklist items done">
          <CheckSquare size={14} aria-hidden="true" />
          <span class="mono">{card.checklist.done}/{card.checklist.total}</span>
        </span>
      {/if}
    </div>
  {/if}

  {#if blockers.length > 0}
    <div class="blk"><Prohibit size={14} weight="bold" aria-hidden="true" />Blocked by {blockers.join(', ')}</div>
  {/if}
</div>

<style>
  .card {
    position: relative;
    margin: 0 3px 9px 0;
    padding: var(--card-pad-y) var(--card-pad-x) calc(var(--card-pad-y) + 1px);
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-card);
    box-shadow: 3px 3px 0 var(--edge);
    cursor: grab;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    /* Scrolling still works by touch: a long press (or the grip) is what picks a card up. */
    touch-action: pan-x pan-y;
    transition: box-shadow 150ms ease-out, transform 150ms ease-out;
  }

  .card:focus-visible {
    /* The shared ring, kept beside the card's hard edge instead of replacing it. */
    box-shadow:
      3px 3px 0 var(--edge),
      0 0 0 2px var(--text);
  }

  .card.next {
    border-color: var(--acc);
    box-shadow: 4px 4px 0 var(--acc);
    transform: rotate(-0.6deg);
  }

  .card.next:focus-visible {
    box-shadow:
      4px 4px 0 var(--acc),
      0 0 0 2px var(--text);
  }

  .card.done {
    background: color-mix(in srgb, var(--card) 60%, transparent);
    border-color: color-mix(in srgb, var(--edge) 45%, transparent);
    box-shadow: none;
  }

  .card.done:focus-visible {
    box-shadow: 0 0 0 2px var(--text);
  }

  /* Its place stays while the card is up: a quiet outline of where it was. */
  .card.ghost {
    opacity: 0.4;
    box-shadow: none;
    border-style: dashed;
    cursor: grabbing;
  }

  .tag {
    position: absolute;
    top: -9px;
    right: 10px;
    display: flex;
    gap: 4px;
    align-items: center;
    padding: 2px 7px 1px;
    border-radius: var(--r-chip);
    background: var(--acc);
    color: var(--acc-ink);
    font-family: var(--font-sign);
    font-stretch: 75%;
    font-weight: 800;
    font-size: 11px;
    letter-spacing: 0.06em;
    line-height: 1;
  }

  .id {
    display: flex;
    gap: 8px;
    align-items: center;
    font-size: 11px;
    color: var(--muted);
  }

  .id :global(svg) {
    flex: none;
  }

  .done .id :global(svg:first-child) {
    color: var(--acc);
  }

  .p0 {
    padding: 1px 5px;
    border-radius: var(--r-chip);
    background: var(--head);
    color: var(--acc);
    font-size: 10.5px;
    font-weight: 600;
  }

  .p1 {
    padding: 0 5px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    color: var(--text);
    font-size: 10.5px;
  }

  .t {
    margin: 5px 0 8px;
    font-size: 14px;
    font-weight: 600;
    line-height: 1.3;
    letter-spacing: -0.005em;
    overflow-wrap: anywhere;
  }

  .t.struck {
    color: var(--muted);
    text-decoration: line-through;
    text-decoration-thickness: 1.5px;
    text-decoration-color: var(--muted);
  }

  .m {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    align-items: center;
    font-size: 12px;
    color: var(--muted);
  }

  .route {
    flex: none;
    width: 16px;
    height: 4px;
    margin-right: -4px;
    border-radius: 2px;
  }

  .check {
    display: inline-flex;
    gap: 4px;
    align-items: center;
  }

  .check .mono {
    font-size: 11px;
  }

  .blk {
    display: flex;
    gap: 6px;
    align-items: center;
    margin-top: 8px;
    color: var(--red);
    font-size: 12px;
    font-weight: 600;
  }

  /* The grip is for touch: a mouse drags from anywhere on the card. */
  .grip {
    display: none;
    margin-left: auto;
    padding: 4px;
    margin-block: -6px;
    margin-right: -6px;
    color: var(--muted);
    touch-action: none;
  }

  @media (pointer: coarse) {
    .grip {
      display: inline-flex;
    }
  }
</style>
