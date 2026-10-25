<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  let {
    label,
    close,
    children,
  }: { label: string; close: () => void; children: Snippet } = $props();
  let dialog: HTMLDialogElement;
  onMount(() => {
    dialog.showModal();
    return () => dialog.close();
  });
</script>

<dialog
  bind:this={dialog}
  class="modal"
  aria-label={label}
  oncancel={(event) => {
    event.preventDefault();
    close();
  }}
  onclick={(event) => {
    if (event.target === dialog) {
      const rect = dialog.getBoundingClientRect();
      if (
        event.clientX < rect.left ||
        event.clientX > rect.right ||
        event.clientY < rect.top ||
        event.clientY > rect.bottom
      )
        close();
    }
  }}
  onclose={close}
>
  {@render children()}
</dialog>
