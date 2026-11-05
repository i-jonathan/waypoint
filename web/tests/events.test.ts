import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { connectEvents } from '../src/lib/events.svelte';

class FakeEventSource {
  static all: FakeEventSource[] = [];
  url: string;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent) => void) | null = null;
  listeners = new Map<string, Array<(m: MessageEvent) => void>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.all.push(this);
  }
  addEventListener(name: string, fn: (m: MessageEvent) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), fn]);
  }
  close() {
    this.closed = true;
  }
  // test controls
  open() {
    this.onopen?.();
  }
  fail() {
    this.onerror?.();
  }
  send(name: string, data: string) {
    const m = { data } as MessageEvent;
    for (const fn of this.listeners.get(name) ?? []) fn(m);
  }
}

const last = () => FakeEventSource.all[FakeEventSource.all.length - 1]!;

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.all = [];
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('connectEvents', () => {
  it('opens the one event stream', () => {
    connectEvents(() => {});
    expect(FakeEventSource.all).toHaveLength(1);
    expect(last().url).toBe('/api/events');
  });

  it('passes the named events on', () => {
    const got: unknown[] = [];
    connectEvents((e) => got.push(e));
    last().open();
    got.length = 0; // the synthetic event of the first open
    last().send('changed', '{"type":"changed","project":"Home"}');
    last().send('projects', '{"type":"projects","project":""}');
    expect(got).toEqual([
      { type: 'changed', project: 'Home' },
      { type: 'projects', project: '' },
    ]);
  });

  it('ignores a message that is not an event', () => {
    const got: unknown[] = [];
    connectEvents((e) => got.push(e));
    last().send('changed', 'not json');
    last().send('changed', '{"project":"Home"}');
    last().send('changed', 'null');
    expect(got).toEqual([]);
  });

  it('emits one synthetic changed event on the first open, so a change made before the subscription is not missed', () => {
    const got: unknown[] = [];
    connectEvents((e) => got.push(e));
    expect(got).toEqual([]);
    last().open();
    expect(got).toEqual([{ type: 'changed', project: '*' }]);
    last().open(); // one stream opens once
    expect(got).toHaveLength(1);
  });

  it('retries after 0.5s, 1s, 2s, 4s, 8s, then every 10s', () => {
    const events = connectEvents(() => {});
    const waits = [500, 1000, 2000, 4000, 8000, 10000, 10000];
    waits.forEach((wait, i) => {
      expect(FakeEventSource.all).toHaveLength(i + 1);
      last().fail();
      expect(last().closed).toBe(true);
      expect(events.status).toBe('reconnecting');
      vi.advanceTimersByTime(wait - 1);
      expect(FakeEventSource.all).toHaveLength(i + 1);
      vi.advanceTimersByTime(1);
      expect(FakeEventSource.all).toHaveLength(i + 2);
    });
  });

  it('emits one synthetic changed event for all projects when the stream comes back', () => {
    const got: unknown[] = [];
    const events = connectEvents((e) => got.push(e));
    last().open();
    last().fail();
    vi.advanceTimersByTime(500);
    expect(got).toEqual([{ type: 'changed', project: '*' }]); // from the first open
    last().open();
    expect(events.status).toBe('open');
    expect(got).toEqual([
      { type: 'changed', project: '*' },
      { type: 'changed', project: '*' },
    ]);
    last().open(); // a repeated open is not a second reconnect
    expect(got).toHaveLength(2);
  });

  it('starts the backoff over after a successful reopen', () => {
    connectEvents(() => {});
    last().fail();
    vi.advanceTimersByTime(500);
    last().fail();
    vi.advanceTimersByTime(1000);
    last().open();
    last().fail();
    vi.advanceTimersByTime(499);
    const n = FakeEventSource.all.length;
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.all).toHaveLength(n + 1);
  });

  it('stops retrying once closed', () => {
    const events = connectEvents(() => {});
    last().fail();
    events.close();
    vi.advanceTimersByTime(60000);
    expect(FakeEventSource.all).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('closes the stream and drops what it still sends', () => {
    const got: unknown[] = [];
    const events = connectEvents((e) => got.push(e));
    const es = last();
    events.close();
    expect(es.closed).toBe(true);
    es.send('changed', '{"type":"changed","project":"Home"}');
    es.fail();
    vi.advanceTimersByTime(60000);
    expect(got).toEqual([]);
    expect(FakeEventSource.all).toHaveLength(1);
  });
});
