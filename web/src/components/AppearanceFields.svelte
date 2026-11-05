<script lang="ts">
  import Check from 'phosphor-svelte/lib/Check';
  import Warning from 'phosphor-svelte/lib/Warning';
  import { accentWarnings, PRESETS } from '../lib/accent';
  import { accentColor } from '../lib/colors';
  import type { Errors } from '../lib/settingsEdit';
  import { resolveTheme, systemPrefersLight } from '../lib/theme';
  import type { Appearance } from '../lib/types';

  type Field = keyof Appearance;
  type Source = 'global' | 'default';

  interface Props {
    /** The values being edited. An empty one inherits. */
    value: Appearance;
    /** What a field shows when it is empty here, and which level that comes from. */
    inherit: (field: Field) => { value: string; source: Source };
    /** Offers a way back to the inherited value for a field that is set here. */
    resettable: boolean;
    swimlaneLabel: string;
    errors: Errors;
  }

  let { value = $bindable(), inherit, resettable, swimlaneLabel, errors }: Props = $props();

  const uid = Math.random().toString(36).slice(2, 7);

  const CHOICES: Array<{ key: Exclude<Field, 'accent'>; options: Array<[string, string]> }> = [
    { key: 'theme', options: [['system', 'System'], ['dark', 'Dark'], ['light', 'Light']] },
    { key: 'density', options: [['comfortable', 'Comfortable'], ['compact', 'Compact']] },
    { key: 'textures', options: [['on', 'On'], ['off', 'Off']] },
    { key: 'swimlane', options: [['none', 'None'], ['group', 'Group'], ['priority', 'Priority']] },
  ];
  const LABELS = { theme: 'Theme', density: 'Density', textures: 'Textures' };
  const labelOf = (f: Exclude<Field, 'accent'>): string => (f === 'swimlane' ? swimlaneLabel : LABELS[f]);
  const names = Object.keys(PRESETS);

  const effective = (f: Field): string => value[f] || inherit(f).value;
  const sourceText = (f: Field): string | null => {
    if (value[f] !== '') return null;
    return inherit(f).source === 'global' ? 'From app settings' : 'Built-in';
  };

  const title = (name: string) => name.charAt(0).toUpperCase() + name.slice(1);

  const accent = $derived(effective('accent'));
  const custom = $derived(Object.hasOwn(PRESETS, accent) ? '' : accent);
  const theme = $derived(resolveTheme(effective('theme') as 'system' | 'light' | 'dark', systemPrefersLight()));
  const warnings = $derived(accentWarnings(custom, theme));
  const accentError = $derived(errors['accent']);

  function set(f: Field, v: string) {
    (value as Record<Field, string>)[f] = v;
  }
</script>

{#snippet meta(f: Field)}
  {@const text = sourceText(f)}
  {#if text !== null || (resettable && value[f] !== '')}
    <div class="meta">
      {#if text !== null}<span class="badge">{text}</span>{/if}
      {#if resettable && value[f] !== ''}
        <button type="button" class="link" onclick={() => set(f, '')}>Reset to global</button>
      {/if}
    </div>
  {/if}
{/snippet}

{#snippet choice(f: Exclude<Field, 'accent'>, options: Array<[string, string]>)}
  <fieldset class="field">
    <legend>{labelOf(f)}</legend>
    <div class="seg">
      {#each options as [v, label] (v)}
        <label class="opt">
          <input type="radio" name="{uid}-{f}" value={v} checked={effective(f) === v} onchange={() => set(f, v)} />
          <span>{label}</span>
        </label>
      {/each}
    </div>
    {@render meta(f)}
  </fieldset>
{/snippet}

<div class="fields">
  {@render choice('theme', CHOICES[0]!.options)}

  <fieldset class="field wide">
    <legend>Accent</legend>
    <div class="swatches">
      {#each names as n (n)}
        <label class="sw">
          <input type="radio" name="{uid}-accent" value={n} checked={accent === n} onchange={() => set('accent', n)} />
          <span class="dot" style:background={accentColor(n)}></span>
          <span class="nm">{title(n)}</span>
          <span class="tick"><Check size={12} weight="bold" aria-hidden="true" /></span>
        </label>
      {/each}
    </div>
    <div class="custom">
      <label for="{uid}-custom">Custom colour</label>
      <div class="inline">
        <span class="dot lg" style:background={custom !== '' && accentError === undefined ? custom : 'transparent'}></span>
        <input
          id="{uid}-custom"
          class="mono"
          type="text"
          placeholder="#rrggbb"
          spellcheck="false"
          autocomplete="off"
          value={custom}
          aria-invalid={accentError ? 'true' : undefined}
          aria-describedby={accentError ? `${uid}-custom-err` : undefined}
          oninput={(e) => set('accent', e.currentTarget.value.trim())}
        />
      </div>
      {#if accentError}<p class="err" id="{uid}-custom-err">{accentError}</p>{/if}
      {#each warnings as w (w)}
        <p class="warn"><Warning size={13} weight="bold" aria-hidden="true" /><span>{w}</span></p>
      {/each}
    </div>
    {@render meta('accent')}
  </fieldset>

  {@render choice('density', CHOICES[1]!.options)}
  {@render choice('textures', CHOICES[2]!.options)}
  {@render choice('swimlane', CHOICES[3]!.options)}
</div>

<style>
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 18px 24px;
  }

  .field {
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .field.wide {
    grid-column: 1 / -1;
  }

  legend {
    padding: 0;
    margin-bottom: 6px;
    font-size: 12px;
    font-weight: 600;
  }

  .seg {
    display: inline-flex;
    flex-wrap: wrap;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    overflow: hidden;
    background: var(--card);
  }

  .opt {
    position: relative;
    display: flex;
    align-items: center;
    min-height: 32px;
    padding: 6px 12px;
    font-size: 13px;
    color: var(--muted);
    cursor: pointer;
  }

  .opt + .opt {
    border-left: 1px solid var(--line);
  }

  .opt input,
  .sw input {
    position: absolute;
    opacity: 0;
    inset: 0;
    margin: 0;
    cursor: pointer;
  }

  .opt:has(input:checked) {
    background: var(--head);
    color: var(--headtext);
    font-weight: 600;
  }

  .opt:has(input:focus-visible),
  .sw:has(input:focus-visible) {
    outline: 2px solid var(--acc);
    outline-offset: 2px;
    box-shadow: 0 0 0 2px var(--text);
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .sw {
    position: relative;
    display: flex;
    align-items: center;
    gap: 7px;
    min-height: 32px;
    padding: 5px 10px 5px 8px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
    cursor: pointer;
  }

  .sw:has(input:checked) {
    border: 1.5px solid var(--edge);
    box-shadow: 2px 2px 0 var(--edge);
    font-weight: 600;
  }

  .tick {
    display: none;
  }

  .sw:has(input:checked) .tick {
    display: inline-flex;
  }

  .dot {
    flex: none;
    width: 14px;
    height: 14px;
    border-radius: 4px;
    border: 1px solid color-mix(in srgb, var(--text) 25%, transparent);
  }

  .dot.lg {
    width: 20px;
    height: 20px;
  }

  .custom {
    display: flex;
    flex-direction: column;
    gap: 5px;
    margin-top: 4px;
    font-size: 12px;
    font-weight: 600;
  }

  .inline {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  input[type='text'] {
    width: 140px;
    min-height: 32px;
    padding: 6px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
    background: var(--card);
    font-size: 13px;
  }

  input[aria-invalid='true'] {
    border-color: var(--red);
  }

  .err {
    margin: 0;
    font-size: 12px;
    font-weight: 400;
    color: var(--red);
  }

  .warn {
    display: flex;
    gap: 6px;
    align-items: flex-start;
    max-width: 52ch;
    margin: 0;
    font-size: 12px;
    font-weight: 400;
    line-height: 1.4;
    color: var(--text);
  }

  .warn :global(svg) {
    flex: none;
    margin-top: 1px;
    color: var(--red);
  }

  .meta {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 20px;
  }

  .badge {
    padding: 1px 7px;
    border: 1px solid var(--line);
    border-radius: var(--r-chip);
    font-size: 11px;
    color: var(--muted);
  }

  .link {
    padding: 0;
    border: 0;
    background: none;
    font-size: 12px;
    color: var(--text);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: pointer;
  }
</style>
