import type { CardView, Settings } from './types';

// ----- The rules: pure, and shared by the pointer, the keyboard and the menu. -----

/**
 * Why a card cannot go to a column, or null when it can. It checks what the
 * client knows (moves_to and the WIP limit, counted on the whole board);
 * the server stays the authority and refuses what this lets through.
 * Moving within a column only reorders, and is always allowed.
 */
export function canDrop(settings: Pick<Settings, 'columns'>, cards: readonly CardView[], cardId: string, to: string): string | null {
  const card = cards.find((c) => c.id === cardId);
  if (!card) return 'That card is no longer on the board';
  if (card.column === to) return null;
  const dest = settings.columns.find((c) => c.name === to);
  if (!dest) return `There is no column called ${to}`;
  const src = settings.columns.find((c) => c.name === card.column);
  if (!src || !src.moves_to.includes(to)) return `${card.column} cannot move to ${to}`;
  if (dest.wip > 0) {
    let n = 0;
    for (const c of cards) if (!c.archived && c.column === to) n++;
    if (n >= dest.wip) return `${to} is at its WIP limit (${n}/${dest.wip})`;
  }
  return null;
}

/** The ids of a column's live cards, in rank order. */
export function columnIds(cards: readonly CardView[], column: string): string[] {
  const ids: string[] = [];
  for (const c of cards) if (!c.archived && c.column === column) ids.push(c.id);
  return ids;
}

/**
 * The index to send for a drop, in the column's FULL rank order, whatever the
 * board is filtered to. `fullIds` is the whole column, `after` is the visible
 * card just above the drop and `before` the visible card just below.
 *
 * The card lands right after `after` in the full order; with nothing above it,
 * right before `before`; with neither, at the bottom. Like the server's, the
 * index counts the column without the moving card.
 */
export function dropIndex(fullIds: readonly string[], movingId: string, after: string | null, before: string | null): number {
  const rest = fullIds.filter((id) => id !== movingId);
  if (after !== null) {
    const at = rest.indexOf(after);
    if (at >= 0) return at + 1;
  }
  if (before !== null) {
    const at = rest.indexOf(before);
    if (at >= 0) return at;
  }
  return rest.length;
}

// ----- The pointer drag -----

/**
 * The page markup the drag reads. Nothing here is queried while the pointer
 * moves: the board is measured when a drop zone is first entered, and again
 * only after something scrolled.
 *
 *   [data-board]      the board: scrolls sideways, and holds every zone
 *   [data-zone]       a drop zone, with data-column and, in swimlanes, data-lane
 *   [data-list]       inside a zone: the positioned box that holds its cards
 *   [data-scroll-y]   the box that scrolls vertically (the list, or the board)
 *   [data-card-id]    a card
 *   [data-drag-handle] a handle that starts a touch drag at once
 */
export interface DropTarget {
  zone: string;
  column: string;
  /** The lane the zone belongs to; undefined outside swimlanes. */
  lane: string | undefined;
  /** The visible card just above the drop, or null at the top. */
  after: string | null;
  /** The visible card just below the drop, or null at the bottom. */
  before: string | null;
  /** Where the drop would land among the zone's visible cards, not counting the one being dragged. */
  index: number;
  /** Where the placeholder goes, in pixels from the top of the zone's list. */
  slotY: number;
}

export interface DragState {
  cardId: string;
  /** Why a column cannot take the card, for the columns that cannot. */
  blocked: ReadonlyMap<string, string>;
  /** Where the card would land now, or null when it is not over a zone that takes it. */
  target: DropTarget | null;
  /** The zone the pointer is over when that zone refuses the card, so its reason can be shown there. */
  over: string | null;
}

export interface DragOptions {
  /** A reason the card cannot go to the column, or null. */
  canDrop(cardId: string, column: string): string | null;
  onDrop(cardId: string, column: string, index: number, target: DropTarget): void;
  /** Called when the drag starts, ends, or its target changes; never once per pointer move. */
  onChange?(state: DragState | null): void;
}

export interface Drag {
  /** Call from a card's pointerdown. The drag starts once the pointer has moved. */
  begin(e: PointerEvent, cardId: string, el: HTMLElement): void;
  /** Stops a drag (or a pending one) without dropping. */
  cancel(): void;
  readonly active: boolean;
}

export const DRAG_THRESHOLD = 4;
/** A touch has to rest this long to pick a card up; moving before that is a scroll. */
export const LONG_PRESS_MS = 320;
const TOUCH_SLOP = 10;
const EDGE = 56;
const MAX_SCROLL = 18;
const REACH = 8;
const SETTLE_MS = 160;

interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

interface CardBox {
  id: string;
  top: number;
  bottom: number;
}

interface Zone {
  el: HTMLElement;
  id: string;
  column: string;
  lane: string | undefined;
  box: Box;
  /** The zone's list: its box and scroll position when measured, so the cards can be placed in its own coordinates. */
  list: HTMLElement | null;
  listTop: number;
  listLeft: number;
  listScroll: number;
  scroller: HTMLElement | null;
  scrollerBox: Box | null;
  /** Measured the first time the pointer enters the zone. */
  cards: CardBox[] | null;
}

function boxOf(el: Element): Box {
  const r = el.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
}

function reducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export function createDrag(opts: DragOptions): Drag {
  type Phase = 'idle' | 'pending' | 'dragging';
  let phase: Phase = 'idle';

  // The gesture
  let pointerId = -1;
  let pointerType = 'mouse';
  let cardId = '';
  let source: HTMLElement | null = null;
  let fromHandle = false;
  let startX = 0;
  let startY = 0;
  let x = 0;
  let y = 0;
  let pressTimer: ReturnType<typeof setTimeout> | undefined;

  // The drag
  let clone: HTMLElement | null = null;
  let baseTransform = '';
  let startBox: Box = { left: 0, top: 0, right: 0, bottom: 0 };
  let board: HTMLElement | null = null;
  let boardBox: Box | null = null;
  let zones: Zone[] = [];
  let blocked = new Map<string, string>();
  let target: DropTarget | null = null;
  let over: string | null = null;
  let hovered: Zone | null = null;
  let dirty = false; // something scrolled: measurements are stale
  let framePending = false;

  const listeners: Array<[EventTarget, string, EventListener, AddEventListenerOptions | boolean | undefined]> = [];

  function listen<K extends string>(t: EventTarget, type: K, fn: (e: never) => void, options?: AddEventListenerOptions | boolean) {
    t.addEventListener(type, fn as EventListener, options);
    listeners.push([t, type, fn as EventListener, options]);
  }

  function unlisten() {
    for (const [t, type, fn, options] of listeners) t.removeEventListener(type, fn, options);
    listeners.length = 0;
  }

  // ----- Measuring -----

  function measureZones() {
    if (!board) return;
    zones = [];
    boardBox = boxOf(board);
    for (const el of board.querySelectorAll<HTMLElement>('[data-zone]')) {
      const list = el.querySelector<HTMLElement>('[data-list]');
      const scroller = el.querySelector<HTMLElement>('[data-scroll-y]') ?? el.closest<HTMLElement>('[data-scroll-y]');
      const listBox = list ? boxOf(list) : null;
      zones.push({
        el,
        id: el.dataset.zone ?? '',
        column: el.dataset.column ?? '',
        lane: el.dataset.lane,
        box: boxOf(el),
        list,
        listTop: listBox?.top ?? 0,
        listLeft: listBox?.left ?? 0,
        listScroll: list?.scrollTop ?? 0,
        scroller,
        scrollerBox: scroller ? boxOf(scroller) : null,
        cards: null,
      });
    }
    dirty = false;
  }

  /** Card positions in the zone's list coordinates (pixels from the top of its content), without the dragged card. */
  function measureCards(z: Zone): CardBox[] {
    const out: CardBox[] = [];
    for (const el of z.el.querySelectorAll<HTMLElement>('[data-card-id]')) {
      const id = el.dataset.cardId ?? '';
      if (id === cardId) continue;
      const b = boxOf(el);
      out.push({ id, top: b.top - z.listTop + z.listScroll, bottom: b.bottom - z.listTop + z.listScroll });
    }
    return out;
  }

  function zoneAt(px: number, py: number): Zone | null {
    for (const z of zones) {
      const b = z.box;
      if (px >= b.left - REACH && px <= b.right + REACH && py >= b.top - REACH && py <= b.bottom + REACH) return z;
    }
    return null;
  }

  // ----- One frame of dragging -----

  function schedule() {
    if (framePending) return;
    framePending = true;
    requestAnimationFrame(frame);
  }

  function frame() {
    framePending = false;
    if (phase !== 'dragging') return;
    if (dirty) {
      measureZones();
      hovered = null;
    }
    if (clone) clone.style.transform = `translate3d(${x - startX}px, ${y - startY}px, 0) ${baseTransform}`;
    retarget();
    autoScroll();
  }

  function retarget() {
    const z = zoneAt(x, y);
    hovered = z;
    let next: DropTarget | null = null;
    if (z && blocked.get(z.column) === undefined) {
      z.cards ??= measureCards(z);
      const cards = z.cards;
      const at = y - z.listTop + z.listScroll;
      let index = 0;
      while (index < cards.length && (cards[index]!.top + cards[index]!.bottom) / 2 < at) index++;
      const above = cards[index - 1];
      const below = cards[index];
      let slotY: number;
      if (above && below) slotY = (above.bottom + below.top) / 2;
      else if (above) slotY = above.bottom + 4;
      else if (below) slotY = below.top - 5;
      else slotY = 4;
      next = {
        zone: z.id,
        column: z.column,
        lane: z.lane,
        after: above?.id ?? null,
        before: below?.id ?? null,
        index,
        slotY,
      };
    }
    const nextOver = z && blocked.has(z.column) ? z.id : null;
    const same =
      nextOver === over &&
      ((next === null && target === null) ||
        (next !== null && target !== null && next.zone === target.zone && next.index === target.index && next.slotY === target.slotY));
    if (same) return;
    target = next;
    over = nextOver;
    emit();
  }

  function autoScroll() {
    if (!board || !boardBox) return;
    let dx = 0;
    let dy = 0;
    const speed = (distance: number) => Math.ceil(MAX_SCROLL * Math.min(1, (EDGE - distance) / EDGE));
    if (x < boardBox.left + EDGE) dx = -speed(Math.max(0, x - boardBox.left));
    else if (x > boardBox.right - EDGE) dx = speed(Math.max(0, boardBox.right - x));
    const sb = hovered?.scrollerBox;
    const scroller = hovered?.scroller;
    if (sb && scroller) {
      if (y < sb.top + EDGE) dy = -speed(Math.max(0, y - sb.top));
      else if (y > sb.bottom - EDGE) dy = speed(Math.max(0, sb.bottom - y));
    }
    let moved = false;
    if (dx !== 0) {
      const before = board.scrollLeft;
      board.scrollLeft = before + dx;
      moved = board.scrollLeft !== before;
    }
    if (dy !== 0 && scroller) {
      const before = scroller.scrollTop;
      scroller.scrollTop = before + dy;
      moved ||= scroller.scrollTop !== before;
    }
    if (moved) {
      dirty = true;
      schedule(); // the pointer is still: keep going until it leaves the edge or the end is reached
    }
  }

  function emit() {
    opts.onChange?.(phase === 'dragging' ? { cardId, blocked, target, over } : null);
  }

  // ----- Starting -----

  function startDrag() {
    if (!source || phase !== 'pending') return;
    clearTimeout(pressTimer);
    board = source.closest<HTMLElement>('[data-board]');
    if (!board) return reset();
    phase = 'dragging';

    // Everything that needs the layout is read here, once.
    startBox = boxOf(source);
    baseTransform = '';
    if (typeof getComputedStyle === 'function') {
      const t = getComputedStyle(source).transform;
      if (t && t !== 'none') baseTransform = t;
    }
    measureZones();

    const columns = new Set(zones.map((z) => z.column));
    blocked = new Map();
    for (const col of columns) {
      const reason = opts.canDrop(cardId, col);
      if (reason !== null) blocked.set(col, reason);
    }

    makeClone();
    document.body.classList.add('wp-dragging');
    listen(window, 'scroll', () => (dirty = true), { capture: true, passive: true });
    listen(window, 'contextmenu', (e: Event) => e.preventDefault());
    emit();
    retarget();
  }

  function makeClone() {
    if (!source) return;
    const c = source.cloneNode(true) as HTMLElement;
    c.removeAttribute('data-card-id');
    c.removeAttribute('tabindex');
    c.removeAttribute('role');
    c.removeAttribute('aria-label');
    c.setAttribute('aria-hidden', 'true');
    c.classList.remove('ghost');
    c.classList.add('drag-clone');
    const s = c.style;
    s.position = 'fixed';
    s.left = `${startBox.left}px`;
    s.top = `${startBox.top}px`;
    s.width = `${startBox.right - startBox.left}px`;
    s.margin = '0';
    s.zIndex = '60';
    s.pointerEvents = 'none';
    s.willChange = 'transform';
    s.transform = baseTransform;
    s.boxShadow = '3px 3px 0 var(--edge)';
    document.body.appendChild(c);
    clone = c;
    // The lift: the shadow grows from 3px to 6px. Under reduced motion it is there at once.
    if (reducedMotion()) {
      s.boxShadow = '6px 6px 0 var(--edge)';
    } else {
      s.transition = `box-shadow ${SETTLE_MS}ms ease-out, transform ${SETTLE_MS}ms ease-out`;
      requestAnimationFrame(() => {
        if (clone === c) c.style.boxShadow = '6px 6px 0 var(--edge)';
      });
      // Only the drop and cancel animate the transform; while dragging it must follow the pointer.
      requestAnimationFrame(() => requestAnimationFrame(() => (c.style.transition = `box-shadow ${SETTLE_MS}ms ease-out`)));
    }
  }

  // ----- Ending -----

  function removeClone(toward: { left: number; top: number } | null) {
    const c = clone;
    clone = null;
    if (!c) return;
    if (toward && !reducedMotion()) {
      const s = c.style;
      s.transition = `transform ${SETTLE_MS}ms ease-out, box-shadow ${SETTLE_MS}ms ease-out`;
      s.transform = `translate3d(${toward.left - startBox.left}px, ${toward.top - startBox.top}px, 0) ${baseTransform}`;
      s.boxShadow = '3px 3px 0 var(--edge)';
      setTimeout(() => c.remove(), SETTLE_MS + 20);
    } else {
      c.remove();
    }
  }

  function reset() {
    clearTimeout(pressTimer);
    unlisten();
    document.body.classList.remove('wp-dragging');
    const wasDragging = phase === 'dragging';
    phase = 'idle';
    source = null;
    board = null;
    boardBox = null;
    zones = [];
    hovered = null;
    target = null;
    over = null;
    blocked = new Map();
    dirty = false;
    if (wasDragging) emit();
  }

  function finish(drop: boolean) {
    if (phase === 'idle') return;
    if (phase === 'pending') return reset();

    const landing = target;
    const zone = landing ? zones.find((z) => z.id === landing.zone) : undefined;
    const id = cardId;

    if (drop && landing && zone) {
      removeClone({ left: zone.listLeft, top: zone.listTop - zone.listScroll + landing.slotY });
    } else {
      removeClone({ left: startBox.left, top: startBox.top });
    }
    reset();
    swallowNextClick();
    if (drop && landing) opts.onDrop(id, landing.column, landing.index, landing);
  }

  /**
   * After a real drag the browser still sends a click to the card; it must not
   * open it. Only a click on a card is stopped, and only until the next press
   * or a moment passes, so it can never eat a later, deliberate click.
   */
  function swallowNextClick() {
    const stop = (e: Event) => {
      if (!(e.target instanceof Element) || e.target.closest('[data-card-id]') === null) return;
      e.stopPropagation();
      e.preventDefault();
      done();
    };
    const done = () => {
      clearTimeout(timer);
      window.removeEventListener('click', stop, true);
      window.removeEventListener('pointerdown', done, true);
    };
    const timer = setTimeout(done, 100);
    window.addEventListener('click', stop, true);
    window.addEventListener('pointerdown', done, true);
  }

  // ----- Pointer events -----

  function onMove(e: PointerEvent) {
    if (e.pointerId !== pointerId) return;
    x = e.clientX;
    y = e.clientY;
    if (phase === 'pending') {
      const moved = Math.hypot(x - startX, y - startY);
      if (pointerType === 'touch' && !fromHandle) {
        // Before the long press, moving is a scroll: let go and let the browser have it.
        if (moved > TOUCH_SLOP) reset();
        return;
      }
      if (moved >= DRAG_THRESHOLD) startDrag();
      return;
    }
    if (phase === 'dragging') schedule();
  }

  function onUp(e: PointerEvent) {
    if (e.pointerId !== pointerId) return;
    x = e.clientX;
    y = e.clientY;
    if (phase === 'dragging') {
      // The last move may not have had its frame yet.
      if (dirty) {
        measureZones();
      }
      retarget();
      finish(true);
    } else {
      reset();
    }
  }

  function onCancel(e: PointerEvent) {
    if (e.pointerId !== pointerId) return;
    finish(false);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || phase === 'idle') return;
    e.preventDefault();
    e.stopPropagation();
    finish(false);
  }

  return {
    begin(e, id, el) {
      if (phase !== 'idle') reset();
      if (e.button !== 0 || !e.isPrimary) return;
      phase = 'pending';
      pointerId = e.pointerId;
      pointerType = e.pointerType || 'mouse';
      cardId = id;
      source = el;
      fromHandle = e.target instanceof Element && e.target.closest('[data-drag-handle]') !== null;
      startX = x = e.clientX;
      startY = y = e.clientY;
      listen(window, 'pointermove', onMove);
      listen(window, 'pointerup', onUp);
      listen(window, 'pointercancel', onCancel);
      listen(window, 'keydown', onKey, true);
      if (pointerType === 'touch') {
        // Once a card is picked up, the finger must not scroll the page.
        listen(window, 'touchmove', (ev: TouchEvent) => phase === 'dragging' && ev.cancelable && ev.preventDefault(), { passive: false });
      }
      if (pointerType === 'touch' && !fromHandle) {
        pressTimer = setTimeout(startDrag, LONG_PRESS_MS);
      }
    },
    cancel() {
      finish(false);
    },
    get active() {
      return phase === 'dragging';
    },
  };
}
