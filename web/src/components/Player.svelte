<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ListMusic,
    Mic2,
    Pause,
    Play,
    SkipBack,
    SkipForward,
    Volume2,
    X,
  } from '@lucide/svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { duration } from '../lib/format';
  import SongCredits from './SongCredits.svelte';
  import Cover from './Cover.svelte';
  import Notice from './Notice.svelte';
  import Lyrics from './Lyrics.svelte';
  let { player }: { player: PlayerState } = $props();
  let audio: HTMLAudioElement;
  let showQueue = $state(false);
  let showLyrics = $state(false);
  let volume = $state(0.8);
  onMount(() => {
    player.audio = audio;
    void player.restore();
    return () => player.dispose();
  });
</script>

<!-- Audio uses the range-capable media URL returned by GetPlaybackInfo. -->
<audio
  bind:this={audio}
  preload="metadata"
  bind:volume
  onloadedmetadata={() => player.loaded()}
  onplay={() => player.started()}
  onpause={() => player.paused()}
  onseeking={() => player.seeking()}
  onseeked={() => player.seeking()}
  ontimeupdate={() => player.update()}
  onended={() => player.next(true)}
  onerror={() => {
    if (player.track && audio.hasAttribute('src'))
      player.error =
        'This track could not be played. Its file may be unavailable or unsupported by your browser.';
  }}
></audio>

{#if player.error}<div class="player-notice">
    <Notice message={player.error} dismiss={() => (player.error = '')} />
  </div>{/if}
{#if showQueue}
  <aside class="queue-panel" aria-label="Playback queue">
    <div class="section-heading">
      <h2>Up next <span class="count">{player.tracks.length}</span></h2>
      <button
        class="icon-button"
        aria-label="Close queue"
        onclick={() => (showQueue = false)}><X size={19} /></button
      >
    </div>
    {#if !player.tracks.length}<p class="muted">
        Add a song to start your queue.
      </p>{/if}
    {#each player.tracks as track, index (`${track.id}-${index}`)}<div
        class:active={index === player.index}
        class="queue-item"
      >
        <Cover
          name={track.album || track.title}
          artwork={track.artworkPath}
          small
        /><span
          ><button class="text-button" onclick={() => player.open(index, true)}
            ><strong>{track.title}</strong></button
          ><SongCredits {track} /></span
        >{#if index === player.index}<span class="playing-dot"></span>{/if}
      </div>{/each}
  </aside>
{/if}
{#if showLyrics}<Lyrics {player} close={() => (showLyrics = false)} />{/if}
<footer class="player">
  <div class="now-playing">
    <Cover
      name={player.track?.album || 'Wavelength'}
      artwork={player.track?.artworkPath}
      small
    />
    <div>
      <strong>{player.track?.title || 'Nothing playing yet'}</strong
      >{#if player.track}<SongCredits track={player.track} />{:else}<span
          >Find your next favorite</span
        >{/if}
    </div>
  </div>
  <div class="player-center">
    <div class="playback-buttons">
      <button
        class="icon-button"
        aria-label="Previous track"
        disabled={!player.track}
        onclick={() => player.previous()}
        ><SkipBack size={18} fill="currentColor" /></button
      ><button
        class="play-button"
        aria-label={player.playing ? 'Pause' : 'Play'}
        disabled={!player.track || player.loading}
        onclick={() => player.toggle()}
        >{#if player.playing}<Pause size={18} fill="currentColor" />{:else}<Play
            size={18}
            fill="currentColor"
          />{/if}</button
      ><button
        class="icon-button"
        aria-label="Next track"
        disabled={player.index + 1 >= player.tracks.length}
        onclick={() => player.next()}
        ><SkipForward size={18} fill="currentColor" /></button
      >
    </div>
    <div class="progress">
      <span>{duration(player.position)}</span><input
        aria-label="Playback position"
        type="range"
        min="0"
        max={player.length || 1}
        step="0.1"
        value={player.position}
        disabled={!player.track}
        onchange={(event) => player.seek(Number(event.currentTarget.value))}
      /><span>{duration(player.length)}</span>
    </div>
  </div>
  <div class="player-tools">
    <button
      class="icon-button"
      class:active={showLyrics}
      aria-label="Show lyrics"
      aria-expanded={showLyrics}
      title="Lyrics"
      disabled={!player.track}
      onclick={() => {
        showLyrics = !showLyrics;
        showQueue = false;
      }}><Mic2 size={18} /></button
    >
    <button
      class:active={showQueue}
      class="icon-button"
      aria-label="Show queue"
      aria-expanded={showQueue}
      onclick={() => {
        showQueue = !showQueue;
        showLyrics = false;
      }}><ListMusic size={20} /></button
    ><Volume2 size={18} /><input
      aria-label="Volume"
      type="range"
      min="0"
      max="1"
      step="0.01"
      bind:value={volume}
    />
  </div>
</footer>
