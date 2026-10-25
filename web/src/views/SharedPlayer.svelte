<script lang="ts">
  import { onMount } from 'svelte';
  import { AudioLines, Play } from '@lucide/svelte';
  import Cover from '../components/Cover.svelte';
  import { duration } from '../lib/format';
  type SharedTrack = {
    id: string;
    title: string;
    artist: string;
    album: string;
    duration_seconds: number;
    artwork_url: string;
  };
  let { token }: { token: string } = $props();
  let name = $state('Shared music');
  let tracks = $state<SharedTrack[]>([]);
  let loading = $state(true);
  let error = $state('');
  let index = $state(-1);
  let url = $state('');
  let busy = $state(false);
  let audio: HTMLAudioElement;
  const controller = new AbortController();
  const base = $derived(`/s/${encodeURIComponent(token)}`);
  onMount(() => {
    void (async () => {
      try {
        const response = await fetch(`${base}/info`, {
          credentials: 'omit',
          signal: controller.signal,
        });
        if (!response.ok)
          throw new Error(
            'This link has expired, was revoked, or is unavailable.',
          );
        const info = await response.json();
        name = info.name;
        tracks = info.tracks;
      } catch (cause) {
        if (!controller.signal.aborted)
          error =
            cause instanceof Error
              ? cause.message
              : 'Unable to open this link.';
      } finally {
        loading = false;
      }
    })();
    return () => {
      controller.abort();
      audio?.pause();
    };
  });
  async function play(next: number) {
    if (busy || next >= tracks.length) return;
    busy = true;
    error = '';
    try {
      const response = await fetch(
        `${base}/play/${encodeURIComponent(tracks[next].id)}`,
        { method: 'POST', credentials: 'omit', signal: controller.signal },
      );
      if (!response.ok)
        throw new Error(
          'This link has expired, was revoked, or is unavailable.',
        );
      const result = await response.json();
      audio.pause();
      url = result.url;
      index = next;
      audio.src = url;
      await audio.play();
    } catch (cause) {
      if (!controller.signal.aborted)
        error =
          cause instanceof Error ? cause.message : 'Unable to play this song.';
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head
  ><title>{name} · Wavelength</title><meta
    name="referrer"
    content="no-referrer"
  /><meta name="robots" content="noindex,nofollow" /></svelte:head
>
<main class="shared-player">
  <a class="login-brand" href="/"
    ><AudioLines size={24} /><span>Wavelength</span></a
  >
  {#if loading}<p role="status">Opening shared music…</p>{:else}<header
      class="entity-header"
    >
      {#if tracks[0]}<Cover {name} artwork={tracks[0].artwork_url} />{/if}
      <div>
        <p class="entity-type">Shared music</p>
        <h1>{name}</h1>
        <p class="muted">
          {tracks.length}
          {tracks.length === 1 ? 'song' : 'songs'}
        </p>
        {#if tracks.length}<button
            class="button primary"
            disabled={busy}
            onclick={() => play(0)}><Play size={15} /> Play</button
          >{/if}
      </div>
    </header>{/if}
  {#if error}<p class="notice" role="alert">{error}</p>{/if}
  <div class="shared-tracks">
    {#each tracks as track, i}<button
        class="shared-track"
        class:current={index === i}
        disabled={busy}
        onclick={() => play(i)}
        aria-label={`Play ${track.title}`}
        ><Cover name={track.title} artwork={track.artwork_url} small /><span
          ><strong>{track.title}</strong><span class="muted"
            >{track.artist}</span
          ></span
        ><span class="muted">{duration(track.duration_seconds)}</span><Play
          size={16}
        /></button
      >{/each}
  </div>
  <div class="shared-audio">
    <p>
      {index >= 0
        ? `${tracks[index].title} · ${tracks[index].artist}`
        : 'Choose a song to listen'}
    </p>
    <audio
      bind:this={audio}
      controls
      preload="none"
      onended={() => {
        if (index + 1 < tracks.length) void play(index + 1);
      }}
      onerror={() => {
        if (url)
          error =
            'Playback is unavailable. Select the song again to refresh its playback link.';
      }}
    ></audio>
  </div>
</main>
