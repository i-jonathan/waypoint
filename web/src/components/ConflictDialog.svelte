<script lang="ts">
  import { FIELD_NAMES, sameField, type FieldName, type Fields } from '../lib/draft';
  import ConfirmDialog from './ConfirmDialog.svelte';

  interface Props {
    /** What is in the panel: the text this person has written. */
    mine: Fields;
    /** The card as it is on disk now. */
    theirs: Fields;
    /** Re-sends the dirty fields against the card on disk. */
    onKeepMine: () => void;
    /** Drops the draft for the card on disk. */
    onUseTheirs: () => void;
    /** Closes the dialog and keeps the draft as it is. */
    onDismiss: () => void;
  }

  let { mine, theirs, onKeepMine, onUseTheirs, onDismiss }: Props = $props();

  const label: Record<FieldName, string> = { title: 'Title', group: 'Group', priority: 'Priority', depends: 'Depends on', body: 'Notes', extra: 'Fields' };

  // Only what differs is shown, so the choice is about something.
  const differing = $derived(FIELD_NAMES.filter((f) => !sameField(f, mine, theirs)));
  const small = $derived(differing.filter((f) => f !== 'body'));

  function show(f: FieldName, v: Fields): string {
    if (f === 'depends') return v.depends.length === 0 ? 'None' : v.depends.join(', ');
    if (f === 'extra') return v.extra.filter((r) => r.key.trim() !== '' && r.value !== '').map((r) => `${r.key}: ${r.value}`).join('; ') || 'None';
    const s = v[f];
    return s === '' ? 'None' : s;
  }
</script>

<ConfirmDialog
  title="This card changed outside Waypoint"
  message="Keep mine sends only what you changed, on top of what is on disk. Use theirs drops your changes."
  confirmLabel="Keep mine"
  cancelLabel="Use theirs"
  focus="dialog"
  wide
  onConfirm={onKeepMine}
  onCancel={onUseTheirs}
  {onDismiss}
>
  <div class="sides">
    {#each [{ name: 'Yours', v: mine }, { name: 'On disk', v: theirs }] as side (side.name)}
      <section class="side" aria-label={side.name}>
        <h3>{side.name}</h3>
        {#if differing.length === 0}
          <p class="same">Both are the same now.</p>
        {/if}
        {#if small.length > 0}
          <dl>
            {#each small as f (f)}
              <dt>{label[f]}</dt>
              <dd>{show(f, side.v)}</dd>
            {/each}
          </dl>
        {/if}
        {#if differing.includes('body')}
          <h4>{label.body}</h4>
          <pre>{side.v.body === '' ? '(empty)' : side.v.body}</pre>
        {/if}
      </section>
    {/each}
  </div>
</ConfirmDialog>

<style>
  .sides {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-top: 10px;
  }

  .side {
    min-width: 0;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-control);
  }

  h3 {
    margin: 0 0 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  h4 {
    margin: 8px 0 4px;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
  }

  dl {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 3px 10px;
    font-size: 13px;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  pre {
    margin: 0;
    max-height: 220px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .same {
    margin: 0;
    font-size: 13px;
    color: var(--muted);
  }

  @media (max-width: 600px) {
    .sides {
      grid-template-columns: 1fr;
    }
  }
</style>
