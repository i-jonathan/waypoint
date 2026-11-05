export interface ServerEvent {
  type: string;
  project: string;
}

export interface Toast {
  id: number;
  /** Plain text. Render it as text, never as HTML. */
  msg: string;
  kind: 'error' | 'info';
}

/** The same message is shown once per this many milliseconds. */
const TOAST_DEDUPE_MS = 2000;

let items = $state.raw<Toast[]>([]);
let nextToastId = 1;
const lastShown = new Map<string, number>();

/** The messages on screen. Dismissing them on a timer is up to the component. */
export const toasts = {
  get items(): Toast[] {
    return items;
  },
  /** Shows a message, unless the same one was shown in the last 2 seconds. */
  push(msg: string, kind: 'error' | 'info' = 'error'): void {
    const now = Date.now();
    for (const [key, at] of lastShown) if (now - at >= TOAST_DEDUPE_MS) lastShown.delete(key);
    const key = `${kind}\n${msg}`;
    if (lastShown.has(key)) return;
    lastShown.set(key, now);
    items = [...items, { id: nextToastId++, msg, kind }];
  },
  dismiss(id: number): void {
    items = items.filter((t) => t.id !== id);
  },
  /** Removes every toast and forgets what was shown. */
  clear(): void {
    items = [];
    lastShown.clear();
  },
};

/** Waits before retry 1, 2, 3, ... in milliseconds; the last value repeats. */
const BACKOFF = [500, 1000, 2000, 4000, 8000, 10000];

/**
 * Opens the server's one event stream and passes each event to onEvent. When
 * the stream breaks it is closed and opened again after a growing wait. A
 * stream that opens may have missed events, whether it is the first (a change
 * between the page's first fetch and the subscription) or a reconnect, so each
 * open sends one synthetic `changed` event for every project (`project: '*'`).
 *
 * `status` starts as 'open', so a page does not flash a warning while the
 * first connection is made; a failure turns it to 'reconnecting'.
 */
export function connectEvents(onEvent: (e: ServerEvent) => void): { readonly status: 'open' | 'reconnecting'; close(): void } {
  let status = $state<'open' | 'reconnecting'>('open');
  let source: EventSource | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let closed = false;
  let failures = 0;

  function deliver(message: MessageEvent) {
    if (closed) return;
    try {
      const e: unknown = JSON.parse(String(message.data));
      if (typeof e === 'object' && e !== null && typeof (e as ServerEvent).type === 'string') {
        const project = (e as ServerEvent).project;
        onEvent({ type: (e as ServerEvent).type, project: typeof project === 'string' ? project : '' });
      }
    } catch {
      // not an event of ours
    }
  }

  function open() {
    if (closed || document.hidden) return;
    const es = new EventSource('/api/events');
    source = es;
    let announced = false; // a stream opens once; this guards a repeated call
    es.onopen = () => {
      if (closed || source !== es) return;
      if (announced) return;
      announced = true;
      failures = 0;
      status = 'open';
      // A change made before this stream opened, or while an earlier one was down,
      // was never announced, so every open asks for a fresh look at everything.
      onEvent({ type: 'changed', project: '*' });
    };
    es.onerror = () => {
      if (closed || source !== es) return;
      es.close();
      source = null;
      status = 'reconnecting';
      const wait = BACKOFF[Math.min(failures, BACKOFF.length - 1)]!;
      failures++;
      timer = setTimeout(open, wait);
    };
    // The server names its events, and a named event does not reach onmessage.
    es.addEventListener('changed', deliver as EventListener);
    es.addEventListener('projects', deliver as EventListener);
    es.onmessage = deliver;
  }

  // On plain HTTP a browser allows six connections per server, and each stream holds one. So a
  // tab nobody is looking at gives its connection back, or a few open tabs would stall every
  // write. The open that follows asks for a fresh look, so nothing is missed.
  function onVisibility() {
    clearTimeout(timer);
    if (document.hidden) {
      source?.close();
      source = null;
    } else if (source === null) {
      open();
    }
  }
  document.addEventListener('visibilitychange', onVisibility);

  open();

  return {
    get status() {
      return status;
    },
    close() {
      closed = true;
      document.removeEventListener('visibilitychange', onVisibility);
      clearTimeout(timer);
      source?.close();
      source = null;
    },
  };
}
