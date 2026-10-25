<script lang="ts">
  import { AlertCircle, X } from '@lucide/svelte';
  let { message, dismiss }: { message: string; dismiss?: () => void } =
    $props();
</script>

{#if message}
  <div class="notice" role="alert">
    <AlertCircle size={18} /><span>{message}</span>{#if dismiss}<button
        class="icon-button"
        aria-label="Dismiss message"
        onclick={dismiss}><X size={16} /></button
      >{/if}
  </div>
{/if}
