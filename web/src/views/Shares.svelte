<script lang="ts">
  import { onMount } from 'svelte';
  import { Link } from '@lucide/svelte';
  import type { AccessLink } from '../gen/wavelength/v1/sharing_pb';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Notice from '../components/Notice.svelte';
  let links = $state<AccessLink[]>([]);
  let loading = $state(true);
  let busy = $state('');
  let error = $state('');
  let offset = $state(0);
  let request = 0;
  onMount(() => {
    void load();
    return () => {
      request++;
    };
  });
  async function load(next = 0, background = false) {
    const generation = ++request;
    if (!background) loading = true;
    error = '';
    try {
      const response = await api.sharing.listAccessLinks({
        kind: 'share',
        limit: 50,
        offset: next,
      });
      if (generation === request) {
        links = response.links;
        offset = next;
      }
    } catch (cause) {
      if (generation === request) error = errorMessage(cause);
    } finally {
      if (generation === request) loading = false;
    }
  }
  function status(item: AccessLink) {
    if (item.expiresAt) {
      const date = new Date(Number(item.expiresAt.seconds) * 1000);
      return date.getTime() <= Date.now()
        ? 'Expired'
        : `Expires ${date.toLocaleString()}`;
    }
    return 'Until revoked';
  }
  async function revoke(id: string) {
    busy = id;
    error = '';
    try {
      await api.sharing.revokeAccessLink({ id });
      links = links.filter((link) => link.id !== id);
      await load(!links.length && offset ? offset - 50 : offset, true);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = '';
    }
  }
</script>

<header class="page-heading">
  <div>
    <h1>Links</h1>
    <p class="subtitle">Manage access to music you’ve shared.</p>
  </div>
</header>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    Loading links…
  </div>{:else if !links.length}<div class="empty-state">
    <Link size={28} />
    <h2>No shares yet</h2>
    <p>Share a song, album, or playlist to create a link.</p>
  </div>{:else}<div class="access-links">
    {#each links as item}<div class="access-link-row">
        <div>
          <strong>{item.name}</strong>
          <p class="muted">
            {item.trackIds.length}
            {item.trackIds.length === 1 ? 'song' : 'songs'} · {status(item)}
          </p>
        </div>
        <button
          class="button secondary compact"
          disabled={!!busy}
          onclick={() => revoke(item.id)}>Revoke</button
        >
      </div>{/each}
  </div>{/if}
{#if offset || links.length === 50}<div class="pagination">
    <button
      class="button secondary"
      disabled={!offset || loading}
      onclick={() => load(offset - 50)}>Previous</button
    ><button
      class="button secondary"
      disabled={links.length < 50 || loading}
      onclick={() => load(offset + 50)}>Next</button
    >
  </div>{/if}
