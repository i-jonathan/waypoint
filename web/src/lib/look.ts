import type { Look } from './theme';
import type { Appearance } from './types';

/** What every appearance field is when neither the project nor the app settings say. */
export const BUILTIN: Appearance = { theme: 'system', accent: 'orange', density: 'comfortable', swimlane: 'none', textures: 'on' };

/** The look an appearance gives the page: its own values, then `inherited` for each that is empty. */
export function lookOf(own: Appearance, inherited: Appearance): Look {
  const pick = <K extends keyof Appearance>(k: K): Appearance[K] => (own[k] || inherited[k]) as Appearance[K];
  return {
    theme: pick('theme') as Look['theme'],
    accent: pick('accent'),
    density: pick('density') as Look['density'],
    textures: pick('textures') as Look['textures'],
  };
}
