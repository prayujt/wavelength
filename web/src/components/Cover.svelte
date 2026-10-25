<script lang="ts">
  import { Disc3 } from '@lucide/svelte';
  import { hue, initials } from '../lib/format';
  let {
    name,
    artwork = '',
    small = false,
  }: { name: string; artwork?: string; small?: boolean } = $props();
</script>

<div class:small class="cover" style={`--cover-hue: ${hue(name)}`}>
  {#if artwork}<img
      src={artwork}
      alt=""
      loading="lazy"
      referrerpolicy="no-referrer"
    />
  {:else}<Disc3 size={small ? 22 : 68} strokeWidth={0.7} /><span
      >{initials(name)}</span
    >{/if}
</div>
