<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { trapFocus } from '../lib/trap';

  interface Props {
    title: string;
    /** Plain text under the title. */
    message?: string;
    /** Anything more to show under the message. */
    children?: Snippet;
    confirmLabel: string;
    cancelLabel?: string;
    /** The confirm button warns: it deletes something. */
    danger?: boolean;
    /** The confirm button cannot be used yet, for example while something is being checked. */
    confirmDisabled?: boolean;
    /** Where focus starts: the safe button, or the dialog itself for a long read. */
    focus?: 'cancel' | 'dialog';
    wide?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
    /** Esc and a click outside. They cancel unless this says otherwise. */
    onDismiss?: () => void;
  }

  let { title, message, children, confirmLabel, cancelLabel = 'Cancel', danger = false, confirmDisabled = false, focus = 'cancel', wide = false, onConfirm, onCancel, onDismiss }: Props = $props();

  const dismiss = () => (onDismiss ?? onCancel)();

  let dialog: HTMLDivElement | undefined = $state();
  let cancelButton: HTMLButtonElement | undefined = $state();
  const titleId = `dlg-${Math.random().toString(36).slice(2, 8)}`;

  onMount(() => {
    const before = document.activeElement;
    (focus === 'cancel' ? cancelButton : dialog)?.focus();
    return () => {
      if (before instanceof HTMLElement && before.isConnected) before.focus();
    };
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    dismiss();
  }
</script>

<button class="scrim" tabindex="-1" aria-label="Close" onclick={dismiss}></button>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  class="dialog"
  class:wide
  role="alertdialog"
  aria-modal="true"
  aria-labelledby={titleId}
  tabindex="-1"
  bind:this={dialog}
  use:trapFocus
  onkeydown={onKeydown}
>
  <h2 id={titleId}>{title}</h2>
  {#if message}<p class="msg">{message}</p>{/if}
  {#if children}{@render children()}{/if}
  <div class="actions">
    <button class="btn" bind:this={cancelButton} onclick={onCancel}>{cancelLabel}</button>
    <button class="btn" class:primary={!danger} class:danger disabled={confirmDisabled} onclick={onConfirm}>{confirmLabel}</button>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    z-index: 80;
    inset: 0;
    border: 0;
    background: color-mix(in srgb, var(--bg) 72%, transparent);
  }

  .dialog {
    position: fixed;
    z-index: 81;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(400px, calc(100vw - 32px));
    max-height: calc(100dvh - 32px);
    overflow-y: auto;
    padding: 18px 20px 16px;
    background: var(--card);
    border: 1.5px solid var(--edge);
    border-radius: var(--r-card);
    box-shadow: 3px 3px 0 var(--edge);
  }

  .dialog.wide {
    width: min(760px, calc(100vw - 32px));
  }

  .dialog:focus {
    outline: none;
  }

  h2 {
    margin: 0 0 6px;
    font-size: 15px;
    font-weight: 600;
  }

  .msg {
    margin: 0 0 4px;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }

  .btn.danger {
    border-color: var(--red);
    background: var(--red);
    color: var(--bg);
  }
</style>
