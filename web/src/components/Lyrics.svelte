<script lang="ts">
  import { LocateFixed, X } from '@lucide/svelte';
  import { tick, untrack } from 'svelte';
  import { Code, ConnectError } from '@connectrpc/connect';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { parseLyrics, type LyricLine } from '../lib/lyrics';
  import Notice from './Notice.svelte';
  import Cover from './Cover.svelte';
  let { player, close }: { player: PlayerState; close: () => void } = $props();
  let lines = $state<LyricLine[]>([]);
  let loading = $state(false);
  let error = $state('');
  let missing = $state(false);
  let following = $state(true);
  let viewport = $state<HTMLDivElement>();
  const timed = $derived(lines.some((line) => line.at !== undefined));
  const active = $derived.by(() => {
    let index = -1;
    for (let i = 0; i < lines.length; i++)
      if (lines[i].at !== undefined && lines[i].at! <= player.position)
        index = i;
    return index;
  });
  $effect(() => {
    const id = player.track?.id;
    lines = [];
    following = true;
    untrack(() => viewport?.scrollTo({ top: 0, behavior: 'instant' }));
    error = '';
    missing = false;
    if (!id) return;
    const controller = new AbortController();
    loading = true;
    api.library
      .getLyrics({ id }, { signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) {
          missing = !result.lyrics.trim();
          lines = missing ? [] : parseLyrics(result.lyrics);
        }
      })
      .catch((cause) => {
        if (!controller.signal.aborted) {
          if (ConnectError.from(cause).code === Code.NotFound) missing = true;
          else error = errorMessage(cause);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) loading = false;
      });
    return () => controller.abort();
  });
  $effect(() => {
    const index = active;
    if (!following || index < 0) return;
    void tick().then(() => {
      if (!following) return;
      const line = viewport?.querySelector<HTMLElement>(
        '[aria-current="true"]',
      );
      if (!line) return;
      viewport?.scrollTo({
        top: Math.max(
          0,
          line.offsetTop -
            (viewport?.clientHeight || 0) * 0.4 +
            line.offsetHeight / 2,
        ),
        behavior: matchMedia('(prefers-reduced-motion: reduce)').matches
          ? 'instant'
          : 'smooth',
      });
    });
  });
</script>

<aside class="lyrics-panel" aria-label="Lyrics">
  <header class="lyrics-heading">
    <Cover
      name={player.track?.album || 'Lyrics'}
      artwork={player.track?.artworkPath}
      small
    />
    <div>
      <h2>Lyrics</h2>
      <p>{player.track?.title} · {player.track?.artist}</p>
    </div>
    <button class="icon-button" aria-label="Close lyrics" onclick={close}
      ><X size={18} /></button
    >
  </header>
  <Notice message={error} />
  {#if loading}<div class="loading-state" role="status">
      <span class="spinner"></span> Loading lyrics…
    </div>
  {:else if missing}<div class="panel-empty">
      <p>No lyrics saved for this track.</p>
    </div>
  {:else}
    <div class="lyrics-toolbar">
      <span>{timed ? 'Click a line to seek' : 'Saved lyrics'}</span>
      {#if timed}<button
          class="follow-lyrics"
          aria-pressed={following}
          onclick={() => (following = !following)}
          ><LocateFixed size={12} />{following
            ? 'Following lyrics'
            : 'Follow lyrics'}</button
        >{/if}
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (The scroll region needs keyboard access alongside its seek buttons.) -->
    <div
      class="lyrics-scroll"
      bind:this={viewport}
      role="region"
      aria-label="Lyrics text"
      tabindex="0"
      onwheel={() => (following = false)}
      ontouchstart={() => (following = false)}
      onpointerdown={() => (following = false)}
      onkeydown={(event) => {
        if (
          [
            'ArrowUp',
            'ArrowDown',
            'PageUp',
            'PageDown',
            'Home',
            'End',
          ].includes(event.key)
        )
          following = false;
      }}
    >
      <div class="lyrics-lines">
        {#each lines as line, index}
          {#if line.at !== undefined}<button
              class:current={index === active}
              class:past={index < active}
              aria-current={index === active ? 'true' : undefined}
              onclick={() => {
                following = true;
                player.seek(line.at!);
              }}>{line.text || '♪'}</button
            >
          {:else}<p>{line.text || '\u00a0'}</p>{/if}
        {/each}
      </div>
    </div>
  {/if}
</aside>
