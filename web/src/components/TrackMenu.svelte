<script lang="ts">
  import { create } from '@bufbuild/protobuf';
  import {
    Download,
    Ellipsis,
    RefreshCw,
    Share2,
    Trash2,
  } from '@lucide/svelte';
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import {
    ExternalTrackSchema,
    type SourceCandidate,
    type ExternalTrack,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { api } from '../lib/session.svelte';
  import { go } from '../lib/navigation';
  import { errorMessage } from '../lib/api';
  import Modal from './Modal.svelte';
  import AccessLinkDialog from './AccessLinkDialog.svelte';
  let sharing = $state(false);
  let deleting = $state(false);
  import SourcePicker from './SourcePicker.svelte';
  import Notice from './Notice.svelte';
  let {
    id = '',
    title,
    downloadOptions,
  }: { id?: string; title: string; downloadOptions?: () => void } = $props();
  let menu: HTMLDivElement;
  let open = $state(false);
  let choosing = $state<ExternalTrack>();
  let busy = $state(false);
  let error = $state('');
  let requestId = '';
  async function deleteTrack() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await api.library.deleteTrack({ id, confirm: true });
      deleting = false;
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function replace() {
    open = false;
    busy = true;
    error = '';
    try {
      const [local, provenance] = await Promise.all([
        api.library.getTrack({ id }),
        api.ingestion.lookupImports({ trackIds: [id] }),
      ]);
      const ref = provenance.tracks.find(
        (match) => match.trackId === id,
      )?.metadataRef;
      if (!local.track || !ref)
        throw new Error('This song has no download source to replace.');
      const track: Track = local.track;
      choosing = create(ExternalTrackSchema, {
        ref,
        title: track.title,
        artist: track.artist,
        album: track.album,
        durationSeconds: track.durationSeconds,
      });
      requestId = crypto.randomUUID();
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function choose(candidate: SourceCandidate) {
    if (!choosing || busy) return;
    busy = true;
    error = '';
    try {
      const job = await api.ingestion.createImport({
        requestId,
        items: [
          {
            metadataRef: choosing.ref,
            sourceRef: candidate.ref,
            replaceTrackId: id,
          },
        ],
      });
      choosing = undefined;
      go(`imports/${encodeURIComponent(job.id)}`);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window
  onpointerdown={(event) => {
    if (open && !menu?.contains(event.target as Node)) open = false;
  }}
  onkeydown={(event) => {
    if (event.key === 'Escape') open = false;
  }}
/>
<div class="track-menu" bind:this={menu}>
  <button
    class="icon-button"
    aria-label={`More options for ${title}`}
    aria-expanded={open}
    disabled={busy}
    onclick={() => (open = !open)}><Ellipsis size={17} /></button
  >
  {#if open}<div class="track-menu-options">
      {#if id}<button
          onclick={() => {
            open = false;
            sharing = true;
          }}><Share2 size={14} /> Share song</button
        ><button onclick={replace}><RefreshCw size={14} /> Replace audio</button
        ><button
          class="danger"
          onclick={() => {
            open = false;
            error = '';
            deleting = true;
          }}><Trash2 size={14} /> Delete from library</button
        >{:else if downloadOptions}<button
          onclick={() => {
            open = false;
            downloadOptions();
          }}><Download size={14} /> Download options</button
        >{/if}
    </div>{/if}
  {#if !choosing && !deleting}<Notice
      message={error}
      dismiss={() => (error = '')}
    />{/if}
</div>
{#if choosing}<SourcePicker
    track={choosing}
    {busy}
    {error}
    action="Replace audio"
    close={() => {
      if (!busy) choosing = undefined;
    }}
    {choose}
  />{/if}

{#if sharing}<AccessLinkDialog
    name={title}
    trackIds={[id]}
    close={() => (sharing = false)}
  />{/if}

{#if deleting}<Modal
    label="Delete song from library"
    close={() => {
      if (!busy) deleting = false;
    }}
  >
    <h2>Delete “{title}”?</h2>
    <p>
      This permanently deletes the local audio and lyrics for every listener,
      removes the song from playlists and queues, and revokes links containing
      it. You can download it again later.
    </p>
    <Notice message={error} />
    <div class="dialog-actions">
      <button
        class="button secondary"
        disabled={busy}
        onclick={() => (deleting = false)}>Cancel</button
      >
      <button class="button danger" disabled={busy} onclick={deleteTrack}
        >{busy ? 'Deleting…' : 'Delete song and files'}</button
      >
    </div>
  </Modal>{/if}
