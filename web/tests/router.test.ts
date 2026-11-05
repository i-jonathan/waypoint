// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { guardNavigation, href, navigate, parsePath, route, type Route } from '../src/lib/router.svelte';

const cases: [string, Route][] = [
  ['/', { name: 'home', search: '' }],
  ['/settings', { name: 'settings', search: '' }],
  ['/p/Home%20reno', { name: 'board', project: 'Home reno', search: '' }],
  ['/p/Home%20reno/next', { name: 'next', project: 'Home reno', search: '' }],
  ['/p/Home%20reno/blocked', { name: 'blocked', project: 'Home reno', search: '' }],
  ['/p/Home%20reno/archive', { name: 'archive', project: 'Home reno', search: '' }],
  ['/p/Home%20reno/settings', { name: 'projectSettings', project: 'Home reno', search: '' }],
  ['/p/Home%20reno/card/H-4', { name: 'card', project: 'Home reno', id: 'H-4', search: '' }],
  ['/p/Home%20reno?q=group%3AKitchen', { name: 'board', project: 'Home reno', search: '?q=group%3AKitchen' }],
];

describe('parsePath', () => {
  it.each(cases)('parses %s', (path, expected) => {
    expect(parsePath(path)).toEqual(expected);
  });

  it('decodes the project name in a card path', () => {
    expect(parsePath('/p/Home%20reno/card/H-4')).toMatchObject({ project: 'Home reno', id: 'H-4' });
  });

  it('accepts a trailing slash', () => {
    expect(parsePath('/p/Home%20reno/')).toMatchObject({ name: 'board', project: 'Home reno' });
  });

  it('falls back to home for paths it does not know', () => {
    expect(parsePath('/nope')).toMatchObject({ name: 'home' });
    expect(parsePath('/p')).toMatchObject({ name: 'home' });
    expect(parsePath('/p/x/card')).toMatchObject({ name: 'home' });
    expect(parsePath('/p/x/unknown')).toMatchObject({ name: 'home' });
  });

  it('does not throw on a malformed escape', () => {
    expect(parsePath('/p/100%')).toMatchObject({ name: 'home' });
  });
});

describe('href', () => {
  it.each(cases)('round-trips %s', (path, expected) => {
    expect(href(expected)).toBe(path);
    expect(parsePath(href(expected))).toEqual(expected);
  });

  it.each(['a/b', 'a b', '100%', 'a%2Fb', 'Q&A?#x', 'ünï/ç ode'])('round-trips the project name %j', (name) => {
    for (const r of [
      { name: 'board', project: name, search: '' },
      { name: 'card', project: name, id: 'X-1', search: '?q=a%2Fb' },
      { name: 'projectSettings', project: name, search: '' },
    ] as Route[]) {
      expect(parsePath(href(r))).toEqual(r);
    }
  });

  it('encodes with encodeURIComponent', () => {
    expect(href({ name: 'board', project: 'a/b c%', search: '' })).toBe('/p/a%2Fb%20c%25');
  });
});

describe('navigate', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });

  it('updates the route and the address bar', () => {
    navigate('/p/Home%20reno/card/H-4');
    expect(window.location.pathname).toBe('/p/Home%20reno/card/H-4');
    expect(route).toMatchObject({ name: 'card', project: 'Home reno', id: 'H-4' });
  });

  it('keeps the query string', () => {
    navigate('/p/Home%20reno?q=group%3AKitchen%20%23hot');
    expect(window.location.search).toBe('?q=group%3AKitchen%20%23hot');
    expect(route.name).toBe('board');
    expect(route.search).toBe('?q=group%3AKitchen%20%23hot');
  });

  it('clears the previous route fields', () => {
    navigate('/p/Home%20reno/card/H-4');
    navigate('/settings');
    expect(route.name).toBe('settings');
    expect(route.project).toBeUndefined();
    expect(route.id).toBeUndefined();
    expect(route.search).toBe('');
  });

  it('handles project names with a slash, a space and a percent sign', () => {
    const path = href({ name: 'card', project: 'a/b c%d', id: 'X-1', search: '' });
    navigate(path);
    expect(route).toMatchObject({ name: 'card', project: 'a/b c%d', id: 'X-1' });
    expect(window.location.pathname).toBe('/p/a%2Fb%20c%25d/card/X-1');
  });

  it('does not push a second history entry for the same address', () => {
    navigate('/settings');
    const length = window.history.length;
    navigate('/settings');
    expect(window.history.length).toBe(length);
  });

  it('follows popstate and keeps the query string', () => {
    window.history.pushState(null, '', '/p/a%2Fb%20c%25d/archive?q=tiles');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(route).toMatchObject({ name: 'archive', project: 'a/b c%d', search: '?q=tiles' });
  });
});

describe('parsePath with names that are also Object properties', () => {
  it('does not take an inherited property name for a project view', () => {
    for (const view of ['constructor', 'toString', '__proto__', 'hasOwnProperty', 'valueOf']) {
      expect(parsePath(`/p/X/${view}`), view).toEqual({ name: 'home', search: '' });
    }
  });

  describe('a navigation guard', () => {
    it('can refuse a move, and is asked where to', () => {
      navigate('/settings');
      const asked: string[] = [];
      const off = guardNavigation((to) => {
        asked.push(to);
        return false;
      });
      navigate('/p/Home');
      off();
      expect(asked).toEqual(['/p/Home']);
      expect(route.name).toBe('settings');
      expect(window.location.pathname).toBe('/settings');
      navigate('/p/Home'); // removed: moves again
      expect(route.name).toBe('board');
    });

    it('puts the page back when a refused Back has already moved the address', () => {
      navigate('/settings');
      navigate('/p/Home');
      const off = guardNavigation(() => false);
      window.history.pushState(null, '', '/settings'); // what the browser did on Back
      window.dispatchEvent(new PopStateEvent('popstate'));
      off();
      expect(window.location.pathname).toBe('/p/Home');
      expect(route.name).toBe('board');
    });

    it('lets the newest guard win and only the owner remove it', () => {
      navigate('/p/Home');
      const a = guardNavigation(() => true);
      const b = guardNavigation(() => false);
      a(); // an old owner's cleanup does not remove the newer guard
      navigate('/settings');
      expect(route.name).toBe('board');
      b();
      navigate('/settings');
      expect(route.name).toBe('settings');
    });
  });
});
