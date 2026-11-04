import { accentInk, resolveAccent, type Theme } from './accent';
import type { Density, Textures, Theme as ThemeSetting } from './types';

/** What decides how the page looks. */
export interface Look {
  theme: ThemeSetting;
  /** A preset name or #rrggbb. */
  accent: string;
  density: Density;
  textures: Textures;
}

export const DEFAULT_LOOK: Look = { theme: 'system', accent: 'orange', density: 'comfortable', textures: 'on' };

export const LIGHT_QUERY = '(prefers-color-scheme: light)';

/** `system` follows the operating system; the other two are fixed. */
export function resolveTheme(setting: ThemeSetting, systemLight: boolean): Theme {
  if (setting === 'system') return systemLight ? 'light' : 'dark';
  return setting;
}

/**
 * The one place the look reaches the page: data-theme, data-density,
 * data-textures, --acc and --acc-ink on the root element. Returns the theme it
 * settled on. index.html runs a tiny copy of this before first paint (checked
 * against this function in tests/theme.test.ts).
 */
export function applyLook(look: Look, systemLight: boolean, root: HTMLElement = document.documentElement): Theme {
  const theme = resolveTheme(look.theme, systemLight);
  const hex = resolveAccent(look.accent, theme);
  root.dataset.theme = theme;
  root.dataset.density = look.density;
  root.dataset.textures = look.textures;
  root.style.setProperty('--acc', hex);
  root.style.setProperty('--acc-ink', accentInk(hex));
  return theme;
}

export function systemPrefersLight(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia(LIGHT_QUERY).matches;
}

/** Calls `onChange` whenever the operating system switches between light and dark. Returns a stop function. */
export function watchSystemTheme(onChange: (light: boolean) => void): () => void {
  if (typeof window.matchMedia !== 'function') return () => {};
  const query = window.matchMedia(LIGHT_QUERY);
  const listener = (e: MediaQueryListEvent) => onChange(e.matches);
  query.addEventListener('change', listener);
  return () => query.removeEventListener('change', listener);
}

// ----- The last look, kept so the next page load can be painted right at once. -----

/** The key the inline script in index.html reads. */
export const LOOK_KEY = 'waypoint.look';

interface Painted {
  acc: string;
  ink: string;
}

interface Saved {
  theme: ThemeSetting;
  density: Density;
  textures: Textures;
  accent: string;
  dark: Painted;
  light: Painted;
}

function painted(accent: string, theme: Theme): Painted {
  const acc = resolveAccent(accent, theme);
  return { acc, ink: accentInk(acc) };
}

export function saveLook(look: Look): void {
  const saved: Saved = {
    ...look,
    dark: painted(look.accent, 'dark'),
    light: painted(look.accent, 'light'),
  };
  try {
    localStorage.setItem(LOOK_KEY, JSON.stringify(saved));
  } catch {
    // storage is blocked or full: the next load just starts from the defaults
  }
}

const THEMES = ['system', 'light', 'dark'];
const DENSITIES = ['comfortable', 'compact'];
const TEXTURES = ['on', 'off'];

export function loadLook(): Look | null {
  try {
    const raw = localStorage.getItem(LOOK_KEY);
    if (raw === null) return null;
    const v: unknown = JSON.parse(raw);
    if (typeof v !== 'object' || v === null) return null;
    const { theme, accent, density, textures } = v as Record<string, unknown>;
    if (typeof accent !== 'string' || !THEMES.includes(theme as string)) return null;
    if (!DENSITIES.includes(density as string) || !TEXTURES.includes(textures as string)) return null;
    return { theme: theme as ThemeSetting, accent, density: density as Density, textures: textures as Textures };
  } catch {
    return null;
  }
}
