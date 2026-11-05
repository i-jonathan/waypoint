export type Theme = 'dark' | 'light';

export const PRESETS: Record<string, { dark: string; light: string }> = {
  orange: { dark: '#ff7a3d', light: '#e8601c' },
  cobalt: { dark: '#7d9bff', light: '#2f5bff' },
  teal: { dark: '#2dd4bf', light: '#0d9488' },
  rose: { dark: '#ff7a9a', light: '#d0365e' },
  green: { dark: '#6ee7a0', light: '#15803d' },
  amber: { dark: '#f5c451', light: '#a16207' },
  sky: { dark: '#7cc8ff', light: '#0369a1' },
  violet: { dark: '#b89cff', light: '#6d28d9' },
};

const HEX = /^#[0-9a-f]{6}$/i;

/** Whether a value is a safe colour to hand to CSS. A hand-edited file can hold anything. */
export const isHex = (value: string): boolean => HEX.test(value);
const DARK_INK = '#0f1714';
const LIGHT_INK = '#ffffff';

/** A preset name gives its colour for the theme; a #rrggbb value is used as is; anything else is orange. */
export function resolveAccent(value: string, theme: Theme): string {
  if (Object.hasOwn(PRESETS, value)) return PRESETS[value][theme];
  if (HEX.test(value)) return value.toLowerCase();
  return PRESETS.orange[theme];
}

function luminance(hex: string): number {
  const channel = (i: number) => {
    const c = parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(0) + 0.7152 * channel(1) + 0.0722 * channel(2);
}

/** WCAG 2 contrast ratio of two #rrggbb colours, from 1 to 21. */
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** The text colour for use on top of an accent: whichever of the two inks contrasts more. */
export function accentInk(hex: string): typeof DARK_INK | typeof LIGHT_INK {
  return contrast(DARK_INK, hex) >= contrast(LIGHT_INK, hex) ? DARK_INK : LIGHT_INK;
}

/** The canvas colour behind the board in each theme (the `bg` token). */
const CANVAS: Record<Theme, string> = { dark: '#0f1714', light: '#e9eee8' };

const MIN_EDGE = 3;
const MIN_TEXT = 4.5;

/** The accent as a custom #rrggbb colour is used for both themes: what is wrong with it in `theme`, if anything. */
export function accentWarnings(hex: string, theme: Theme): string[] {
  if (!HEX.test(hex)) return [];
  const out: string[] = [];
  if (contrast(hex, CANVAS[theme]) < MIN_EDGE) {
    out.push(`This colour is hard to see on the ${theme} background: it does not reach 3:1 contrast.`);
  }
  if (contrast(accentInk(hex), hex) < MIN_TEXT) {
    out.push('Text on this colour does not reach 4.5:1 contrast.');
  }
  return out;
}
