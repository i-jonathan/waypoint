<script lang="ts">
  import Warning from 'phosphor-svelte/lib/Warning';

  interface Props {
    error: { line?: number; message: string };
  }

  let { error }: Props = $props();

  // The store gives the bare message (no file path, no line prefix), so the line is said once.
  // A message that ends in a full stop would double it before "Fix the file".
  const where = $derived(error.line === undefined ? '' : `line ${error.line}: `);
  const message = $derived(error.message.replace(/[.\s]+$/, ''));
</script>

<div class="banner" role="alert">
  <Warning size={18} weight="bold" aria-hidden="true" />
  <p>This board can't be read: {where}{message}. Fix the file and Waypoint will reload it.</p>
</div>

<style>
  .banner {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 10px 14px;
    background: var(--card);
    border: 1px solid var(--line);
    border-left: 4px solid var(--red);
    border-radius: var(--r-control);
    font-size: 13px;
    line-height: 1.45;
  }

  .banner :global(svg) {
    flex: none;
    margin-top: 1px;
    color: var(--red);
  }

  p {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
