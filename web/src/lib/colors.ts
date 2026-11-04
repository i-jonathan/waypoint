import { PRESETS, resolveAccent } from './accent';

/** A CSS colour that follows the page's colour-scheme, so no component needs to know the theme. */
function pair(dark: string, light: string): string {
  return `light-dark(${light}, ${dark})`; // light-dark() takes the light value first
}

/** The accent a project resolves to, as a CSS colour for a swatch. */
export function accentColor(accent: string): string {
  return pair(resolveAccent(accent, 'dark'), resolveAccent(accent, 'light'));
}

// Group colours are free text in the board file. These names get a dark and a
// light value tuned against the canvas; a #rrggbb value is used as is.
const GROUPS: Record<string, [dark: string, light: string]> = {
  blue: ['#6fa8ff', '#2f6fd6'],
  amber: ['#e8c35a', '#a9801f'],
  green: ['#7fd1a0', '#3f8f5f'],
  sage: ['#8bbf9f', '#5c9e74'],
  rose: ['#ff8fa8', '#d0647a'],
  sand: ['#d9b36a', '#a9801f'],
  violet: ['#b89cff', '#7c5ad6'],
  teal: ['#4fd1c5', '#0f8f86'],
  orange: ['#ff9b5e', '#d9621f'],
  red: ['#ff7b6b', '#c8321e'],
  sky: ['#7cc8ff', '#2a7fb8'],
};

const HEX = /^#[0-9a-f]{6}$/i;

/** The colour of a group's route line. An unknown name falls back to the muted text colour. */
export function groupColor(color: string): string {
  const key = color.trim().toLowerCase();
  if (Object.hasOwn(GROUPS, key)) {
    const [dark, light] = GROUPS[key]!;
    return pair(dark, light);
  }
  if (HEX.test(key)) return key;
  if (Object.hasOwn(PRESETS, key)) return accentColor(key);
  return 'var(--muted)';
}
