<script lang="ts">
  import Cover from './Cover.svelte';
  let {
    name,
    trackIds,
    small = false,
  }: { name: string; trackIds: string[]; small?: boolean } = $props();
  const first = $derived(trackIds.slice(0, 4));
  const artwork = (id: string) =>
    `/media/tracks/${encodeURIComponent(id)}/artwork`;
</script>

<div
  class="playlist-cover"
  class:small
  class:split={first.length === 2}
  class:mosaic={first.length >= 3}
>
  {#if first.length < 2}<Cover
      {name}
      artwork={first[0] ? artwork(first[0]) : ''}
    />{:else}
    {#each first as id, index (`${id}-${index}`)}<Cover
        {name}
        artwork={artwork(id)}
      />{/each}
    {#if first.length === 3}<div
        class="empty-tile"
        aria-hidden="true"
      ></div>{/if}
  {/if}
</div>

<style>
  .playlist-cover {
    display: grid;
    width: 100%;
    aspect-ratio: 1;
    overflow: hidden;
    border: 1px solid var(--border);
    border-radius: 5px;
    background: var(--surface);
    margin-bottom: 11px;
  }
  .playlist-cover.small {
    width: 48px;
    height: 48px;
    flex-shrink: 0;
    margin: 0;
  }
  .split {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .mosaic {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: repeat(2, minmax(0, 1fr));
  }
  .playlist-cover :global(.cover) {
    width: 100%;
    height: 100%;
    min-height: 0;
    aspect-ratio: auto;
    border: 0;
    border-radius: 0;
    margin: 0;
  }
  .playlist-cover :global(.cover > span) {
    display: none;
  }
  .playlist-cover :global(.cover > svg) {
    width: 35%;
    height: 35%;
  }
  .empty-tile {
    background: var(--surface);
  }
</style>
