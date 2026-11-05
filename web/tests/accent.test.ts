import { describe, expect, it } from 'vitest';
import { PRESETS, accentInk, contrast, resolveAccent } from '../src/lib/accent';

describe('accentInk', () => {
  const colours: [string, string][] = [];
  for (const [name, p] of Object.entries(PRESETS)) {
    colours.push([`${name} dark`, p.dark], [`${name} light`, p.light]);
  }
  colours.push(['yellow', '#ffff00'], ['navy', '#000080']);

  it.each(colours)('reaches 4.5:1 on %s', (_label, hex) => {
    expect(contrast(accentInk(hex), hex)).toBeGreaterThanOrEqual(4.5);
  });

  it('picks only the two allowed inks', () => {
    expect(accentInk('#ffff00')).toBe('#0f1714');
    expect(accentInk('#000080')).toBe('#ffffff');
  });
});

describe('contrast', () => {
  it('is 21 for black on white and 1 for identical colours', () => {
    expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 5);
    expect(contrast('#336699', '#336699')).toBeCloseTo(1, 5);
  });
});

describe('resolveAccent', () => {
  it('maps a preset to its theme colour', () => {
    expect(resolveAccent('cobalt', 'light')).toBe('#2f5bff');
    expect(resolveAccent('cobalt', 'dark')).toBe('#7d9bff');
  });

  it('uses a custom hex for both themes', () => {
    expect(resolveAccent('#123456', 'dark')).toBe('#123456');
    expect(resolveAccent('#123456', 'light')).toBe('#123456');
  });

  it('falls back to orange for unknown values', () => {
    expect(resolveAccent('chartreuse', 'dark')).toBe('#ff7a3d');
    expect(resolveAccent('', 'light')).toBe('#e8601c');
    expect(resolveAccent('#12345', 'dark')).toBe('#ff7a3d');
  });
});
