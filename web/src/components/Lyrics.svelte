<script lang="ts">
  import { X } from '@lucide/svelte';
  import { Code, ConnectError } from '@connectrpc/connect';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { parseLyrics, type LyricLine } from '../lib/lyrics';
  import Notice from './Notice.svelte';
  let { player, close }: { player: PlayerState; close: () => void } = $props();
  let lines = $state<LyricLine[]>([]);
  let loading = $state(false);
  let error = $state('');
  let missing = $state(false);
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
</script>

<aside class="lyrics-panel" aria-label="Lyrics">
  <div class="section-heading">
    <div>
      <h2>Lyrics</h2>
      <p class="muted small-text">{player.track?.title}</p>
    </div>
    <button class="icon-button" aria-label="Close lyrics" onclick={close}
      ><X size={18} /></button
    >
  </div>
  <Notice message={error} />{#if loading}<div
      class="loading-state"
      role="status"
    >
      <span class="spinner"></span> Loading lyrics…
    </div>{:else if missing}<div class="panel-empty">
      <p>No lyrics saved for this track.</p>
    </div>{:else}<div class="lyrics-lines">
      {#each lines as line, index}{#if line.at !== undefined}<button
            class:current={index === active}
            aria-current={index === active ? 'true' : undefined}
            onclick={() => player.seek(line.at!)}>{line.text || '♪'}</button
          >{:else}<p>{line.text || '\u00a0'}</p>{/if}{/each}
    </div>{/if}
</aside>
