<script lang="ts">
  import { Share2 } from '@lucide/svelte';
  import AccessLinkDialog from './AccessLinkDialog.svelte';
  let { name, trackIds }: { name: string; trackIds: string[] } = $props();
  let open = $state(false);
</script>

<button
  class="button secondary compact"
  disabled={!trackIds.length}
  onclick={() => (open = true)}><Share2 size={14} /> Share</button
>
{#if open}<AccessLinkDialog
    {name}
    {trackIds}
    close={() => (open = false)}
  />{/if}
