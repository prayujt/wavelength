<script lang="ts">
  import { Check } from '@lucide/svelte';
  let {
    saved,
    total,
    checking = false,
  }: { saved: number; total: number; checking?: boolean } = $props();
</script>

{#if total > 0}<span
    class="library-summary"
    class:complete={!checking && saved === total}
    role="status"
    aria-label={checking
      ? 'Checking library'
      : saved === total
        ? `Fully in library (${saved} of ${total} songs)`
        : `${saved} of ${total} songs in library`}
    title={`${saved} of ${total} songs in library`}
  >
    {#if checking}Checking library…{:else if saved === total}<Check size={12} /> Fully
      in library{:else}{saved} of {total} songs in library{/if}
  </span>{/if}
