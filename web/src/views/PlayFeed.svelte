<script lang="ts">
  import { onMount } from 'svelte';
  import { Pause, Play, SkipForward, Sparkles } from '@lucide/svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import Cover from '../components/Cover.svelte';
  import SongCredits from '../components/SongCredits.svelte';
  let { player }: { player: PlayerState } = $props();
  onMount(() => {
    void player.startFeed();
  });
</script>

<section class="play-feed">
  <header class="section-heading">
    <div>
      <p class="eyebrow">MADE FOR YOU</p>
      <h1>Your play feed</h1>
      <p class="muted">Picked from your listening history. More as you go.</p>
    </div>
    <button
      class="button secondary"
      disabled={player.startingFeed}
      onclick={() => player.startFeed(true)}
      ><Sparkles size={16} /> Start a new feed</button
    >
  </header>
  {#if player.startingFeed}
    <div class="empty-state" role="status">
      <span class="spinner"></span>
      <p>Choosing your first songs…</p>
    </div>
  {:else if player.track}
    <div class="feed-current">
      <div class="feed-art">
        <Cover
          name={player.track.album || player.track.title}
          artwork={player.track.artworkPath}
        />
      </div>
      <div>
        <p class="eyebrow">NOW PLAYING</p>
        <h2>{player.track.title}</h2>
        <SongCredits track={player.track} />
        <div class="entity-actions">
          <button class="button primary" onclick={() => player.toggle()}
            >{#if player.playing}<Pause size={16} /> Pause feed{:else}<Play
                size={16}
              /> Play feed{/if}</button
          >
          <button class="button secondary" onclick={() => player.next()}
            ><SkipForward size={16} /> Next song</button
          >
        </div>
      </div>
    </div>
    <h2>Coming up</h2>
    <div class="feed-upcoming">
      {#each player.tracks.slice(player.index + 1, player.index + 3) as track, at}
        <button
          class="feed-song"
          onclick={() => player.open(player.index + 1 + at, true)}
          ><Cover
            name={track.album || track.title}
            artwork={track.artworkPath}
            small
          /><span
            ><strong>{track.title}</strong><span class="muted"
              >{track.artist}</span
            ></span
          ></button
        >
      {/each}
    </div>
    {#if player.playFeedLoading}<p class="muted" role="status">
        Choosing what comes next…
      </p>{/if}
  {:else}
    <div class="empty-state">
      <h2>Your next song is out there</h2>
      <p>Start listening from your history. No queue needed.</p>
      <button class="button primary" onclick={() => player.startFeed(true)}
        ><Play size={16} /> Try play feed again</button
      >
    </div>
  {/if}
</section>

<style>
  .play-feed {
    max-width: 1000px;
    margin: 0 auto;
  }
  .feed-current {
    display: flex;
    align-items: center;
    gap: 28px;
    padding: 24px 0 40px;
  }
  .feed-art {
    width: 190px;
    flex-shrink: 0;
  }
  .feed-current h2 {
    font-size: 28px;
    margin-bottom: 8px;
  }
  .entity-actions {
    margin-top: 20px;
  }
  .feed-upcoming {
    display: grid;
    gap: 10px;
    margin-top: 16px;
  }
  .feed-song {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 12px;
    text-align: left;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--surface);
  }
  .feed-song > span {
    display: grid;
    gap: 5px;
  }
  @media (max-width: 600px) {
    .feed-current {
      align-items: flex-start;
      flex-direction: column;
    }
    .feed-art {
      width: 150px;
    }
  }
</style>
