<script lang="ts">
  import { onMount } from 'svelte';
  import { guardNavigation, navigate } from '../lib/router.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  interface Props {
    /** True while the form holds changes that are not saved. */
    dirty: boolean;
  }

  let { dirty }: Props = $props();

  /** Where the owner was heading when the guard stopped them. */
  let leaving = $state<string | null>(null);
  // Set once the owner has said to leave, so the move is not stopped a second time.
  let allow = false;

  onMount(() => {
    const off = guardNavigation((to) => {
      if (allow || !dirty) return true;
      leaving = to;
      return false;
    });
    const onUnload = (e: BeforeUnloadEvent) => {
      if (!dirty) return;
      e.preventDefault();
      e.returnValue = ''; // some browsers only ask when this is set
    };
    window.addEventListener('beforeunload', onUnload);
    return () => {
      off();
      window.removeEventListener('beforeunload', onUnload);
    };
  });

  function leave() {
    const to = leaving;
    leaving = null;
    if (to === null) return;
    allow = true;
    navigate(to);
    allow = false;
  }
</script>

{#if leaving !== null}
  <ConfirmDialog
    title="Leave without saving?"
    message="Your changes to these settings have not been saved."
    confirmLabel="Leave"
    cancelLabel="Stay"
    danger
    onConfirm={leave}
    onCancel={() => (leaving = null)}
  />
{/if}
