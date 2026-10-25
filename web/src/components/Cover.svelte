<script lang="ts">
  import { Disc3 } from '@lucide/svelte';
  import { initials } from '../lib/format';
  let {
    name,
    artwork = '',
    small = false,
  }: { name: string; artwork?: string; small?: boolean } = $props();
  let failedArtwork = $state('');
</script>

<div class:small class="cover">
  {#if artwork && artwork !== failedArtwork}<img
      src={artwork}
      alt=""
      loading="lazy"
      referrerpolicy="no-referrer"
      onerror={() => (failedArtwork = artwork)}
    />
  {:else}<Disc3 size={small ? 22 : 68} strokeWidth={0.7} /><span
      >{initials(name)}</span
    >{/if}
</div>
