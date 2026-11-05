import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { contrast, PRESETS } from '../src/lib/accent';

// The focus ring is two-tone: an accent outline outside and a --text ring
// inside it. The accent alone is not enough (light orange on the light canvas
// is under 3:1), so the --text ring has to carry the 3:1 against every surface
// the ring can touch, in both themes. These read the real token values.

const tokens = readFileSync(join(process.cwd(), 'src/styles/tokens.css'), 'utf8');
const base = readFileSync(join(process.cwd(), 'src/styles/base.css'), 'utf8');

function themeTokens(theme: 'dark' | 'light'): Record<string, string> {
  const block = new RegExp(`:root\\[data-theme='${theme}'\\]\\s*\\{([^}]*)\\}`).exec(tokens)![1]!;
  return Object.fromEntries([...block.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)].map((m) => [m[1]!, m[2]!]));
}

describe('focus ring', () => {
  it('is drawn as an accent outline with an inner --text ring', () => {
    const rule = /:focus-visible\s*\{([^}]*)\}/.exec(base)![1]!;
    expect(rule).toMatch(/outline:\s*2px solid var\(--acc\)/);
    expect(rule).toMatch(/box-shadow:\s*0 0 0 2px var\(--text\)/);
  });

  for (const theme of ['dark', 'light'] as const) {
    const t = themeTokens(theme);

    it(`has the inner ring at 3:1 or more against every surface in ${theme}`, () => {
      for (const surface of ['bg', 'card']) {
        expect(t[surface], surface).toBeDefined();
        expect(contrast(t.text!, t[surface]!), `${surface} in ${theme}`).toBeGreaterThanOrEqual(3);
      }
    });

    it(`gets 3:1 for every accent preset in ${theme}, which the accent alone does not always give`, () => {
      let accentAloneFails = 0;
      for (const [name, pair] of Object.entries(PRESETS)) {
        const acc = pair[theme];
        const ring = Math.max(contrast(acc, t.bg!), contrast(t.text!, t.bg!));
        expect(ring, `${name} on bg`).toBeGreaterThanOrEqual(3);
        const onCard = Math.max(contrast(acc, t.card!), contrast(t.text!, t.card!));
        expect(onCard, `${name} on card`).toBeGreaterThanOrEqual(3);
        if (contrast(acc, t.bg!) < 3) accentAloneFails++;
      }
      if (theme === 'light') expect(accentAloneFails).toBeGreaterThan(0); // light orange is 2.92:1
    });
  }
});
