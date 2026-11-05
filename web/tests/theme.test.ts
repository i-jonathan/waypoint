// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { accentInk, resolveAccent } from '../src/lib/accent';
import { applyLook, DEFAULT_LOOK, loadLook, LOOK_KEY, resolveTheme, saveLook, watchSystemTheme, type Look } from '../src/lib/theme';

function fresh(): HTMLElement {
  return document.createElement('html');
}

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
  const root = document.documentElement;
  root.removeAttribute('style');
  for (const key of ['theme', 'density', 'textures']) delete root.dataset[key];
});

describe('resolveTheme', () => {
  it('follows the system for system, and is fixed otherwise', () => {
    expect(resolveTheme('system', true)).toBe('light');
    expect(resolveTheme('system', false)).toBe('dark');
    expect(resolveTheme('dark', true)).toBe('dark');
    expect(resolveTheme('light', false)).toBe('light');
  });
});

describe('applyLook', () => {
  it('sets every attribute and both accent properties in one call', () => {
    const root = fresh();
    const theme = applyLook({ theme: 'system', accent: 'cobalt', density: 'compact', textures: 'off' }, true, root);
    expect(theme).toBe('light');
    expect(root.dataset.theme).toBe('light');
    expect(root.dataset.density).toBe('compact');
    expect(root.dataset.textures).toBe('off');
    expect(root.style.getPropertyValue('--acc')).toBe('#2f5bff');
    expect(root.style.getPropertyValue('--acc-ink')).toBe(accentInk('#2f5bff'));
  });

  it('takes the dark value of a preset in the dark theme', () => {
    const root = fresh();
    applyLook({ ...DEFAULT_LOOK, theme: 'dark' }, true, root);
    expect(root.style.getPropertyValue('--acc')).toBe('#ff7a3d');
    expect(root.style.getPropertyValue('--acc-ink')).toBe('#0f1714');
  });

  it('uses a custom hex in both themes, with ink that reaches 4.5:1', () => {
    const root = fresh();
    for (const light of [false, true]) {
      applyLook({ ...DEFAULT_LOOK, accent: '#123456' }, light, root);
      expect(root.style.getPropertyValue('--acc')).toBe('#123456');
      expect(root.style.getPropertyValue('--acc-ink')).toBe('#ffffff');
    }
  });
});

describe('watchSystemTheme', () => {
  it('reports changes until stopped', () => {
    const listeners = new Set<(e: { matches: boolean }) => void>();
    vi.stubGlobal('matchMedia', () => ({
      matches: false,
      addEventListener: (_: string, fn: never) => listeners.add(fn),
      removeEventListener: (_: string, fn: never) => listeners.delete(fn),
    }));
    window.matchMedia = globalThis.matchMedia;
    const seen: boolean[] = [];
    const stop = watchSystemTheme((l) => seen.push(l));
    for (const fn of listeners) fn({ matches: true });
    stop();
    expect(listeners.size).toBe(0);
    expect(seen).toEqual([true]);
  });
});

describe('the saved look', () => {
  it('round trips', () => {
    const look: Look = { theme: 'light', accent: '#abcdef', density: 'compact', textures: 'off' };
    saveLook(look);
    expect(loadLook()).toEqual(look);
  });

  it('is ignored when it is damaged', () => {
    localStorage.setItem(LOOK_KEY, '{"theme":"neon"}');
    expect(loadLook()).toBeNull();
    localStorage.setItem(LOOK_KEY, 'not json');
    expect(loadLook()).toBeNull();
  });
});

// public/look.js paints the saved look before first paint. It must land on the
// same page as applyLook. It is a file because the Content-Security-Policy allows
// no inline script.
describe('the look script', () => {
  const html = readFileSync(join(process.cwd(), 'index.html'), 'utf8');
  const script = readFileSync(join(process.cwd(), 'public/look.js'), 'utf8');

  it('is loaded by index.html, which has no inline script', () => {
    expect(html).toContain('<script src="/look.js"></script>');
    expect(html).not.toMatch(/<script>/);
  });

  function paint(): HTMLElement {
    const root = document.documentElement;
    new Function(script!)();
    return root;
  }

  function systemIs(light: boolean) {
    vi.stubGlobal('matchMedia', () => ({ matches: light }));
  }

  function attrs(root: HTMLElement) {
    return {
      theme: root.dataset.theme,
      density: root.dataset.density,
      textures: root.dataset.textures,
      acc: root.style.getPropertyValue('--acc'),
      ink: root.style.getPropertyValue('--acc-ink'),
    };
  }

  it('exists', () => {
    expect(script).toBeTruthy();
  });

  const looks: Look[] = [
    { theme: 'system', accent: 'orange', density: 'comfortable', textures: 'on' },
    { theme: 'light', accent: 'cobalt', density: 'compact', textures: 'off' },
    { theme: 'dark', accent: '#2dd4bf', density: 'comfortable', textures: 'on' },
  ];

  for (const look of looks) {
    for (const systemLight of [false, true]) {
      it(`matches applyLook for ${look.theme} ${look.accent} with a ${systemLight ? 'light' : 'dark'} system`, () => {
        saveLook(look);
        systemIs(systemLight);
        const painted = attrs(paint());
        const expected = fresh();
        applyLook(look, systemLight, expected);
        expect(painted).toEqual(attrs(expected));
        expect(painted.acc).toBe(resolveAccent(look.accent, painted.theme as 'dark' | 'light'));
      });
    }
  }

  it('still follows the system when nothing is saved', () => {
    systemIs(true);
    expect(paint().dataset.theme).toBe('light');
    document.documentElement.removeAttribute('data-theme');
    systemIs(false);
    expect(paint().dataset.theme).toBe('dark');
  });
});
