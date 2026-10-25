<script lang="ts">
  import { untrack } from 'svelte';
  import { timestampFromDate } from '@bufbuild/protobuf/wkt';
  import { Copy, Link, ShieldCheck } from '@lucide/svelte';
  import type { AccessLink } from '../gen/wavelength/v1/sharing_pb';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Modal from './Modal.svelte';
  import Notice from './Notice.svelte';
  let {
    name,
    trackIds,
    kind = 'share',
    close,
  }: {
    name: string;
    trackIds: string[];
    kind?: 'share' | 'playback';
    close: () => void;
  } = $props();
  let expiry = $state(untrack(() => (kind === 'share' ? '0' : '3600')));
  let busy = $state(false);
  let error = $state('');
  let url = $state('');
  let created = $state<AccessLink>();
  let copied = $state(false);
  let revoked = $state(false);
  async function generate() {
    busy = true;
    error = '';
    try {
      const response =
        kind === 'share'
          ? await api.sharing.createShare({
              name,
              trackIds,
              expiresAt: Number(expiry)
                ? timestampFromDate(
                    new Date(Date.now() + Number(expiry) * 1000),
                  )
                : undefined,
            })
          : await api.sharing.createPlaybackLink({
              trackId: trackIds[0],
              expiresInSeconds: Number(expiry),
            });
      url = response.url;
      created = response.link;
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      copied = true;
    } catch {
      error = 'Copy the link from the field above.';
    }
  }
  async function revoke() {
    if (!created) return;
    busy = true;
    error = '';
    try {
      await api.sharing.revokeAccessLink({ id: created.id });
      revoked = true;
      url = '';
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  label={kind === 'share' ? 'Share music' : 'Create playback link'}
  close={() => {
    if (!busy) close();
  }}
>
  <h2>{kind === 'share' ? 'Share music' : 'Playback link'}</h2>
  <p class="subtitle">{name}</p>
  <p class="muted">
    {kind === 'share'
      ? 'Anyone with this link can open a player and listen to these songs. No account needed.'
      : 'A direct audio URL for Sonos or another player. Anyone with the link can fetch this song until it expires.'}
  </p>
  {#if revoked}<p role="status"><ShieldCheck size={16} /> Link revoked.</p>
  {:else if url}<label
      >Link<input
        aria-label="Created link"
        value={url}
        readonly
        onclick={(event) => event.currentTarget.select()}
      /></label
    >
    <p class="muted">
      {created?.expiresAt
        ? `Expires ${new Date(Number(created.expiresAt.seconds) * 1000).toLocaleString()}.`
        : 'Valid until you revoke it.'} Copy this link now; it is only shown when
      created.
    </p>
    <div class="dialog-actions">
      <button class="button secondary" disabled={busy} onclick={revoke}
        >Revoke link</button
      ><button class="button primary" onclick={copy}
        ><Copy size={14} /> {copied ? 'Copied' : 'Copy link'}</button
      >
    </div>
  {:else}<label
      >Expires<select bind:value={expiry} disabled={busy}
        >{#if kind === 'share'}<option value="0">Never</option><option
            value="86400">In 1 day</option
          ><option value="604800">In 7 days</option><option value="2592000"
            >In 30 days</option
          >{:else}<option value="3600">In 1 hour</option><option value="21600"
            >In 6 hours</option
          ><option value="86400">In 24 hours</option>{/if}</select
      ></label
    >
    {#if kind === 'share' && trackIds.length > 1}<p class="muted">
        Shares the current {trackIds.length} songs. Later playlist changes won’t change
        this link.
      </p>{/if}
    <div class="dialog-actions">
      <button class="button secondary" disabled={busy} onclick={close}
        >Cancel</button
      ><button class="button primary" disabled={busy} onclick={generate}
        ><Link size={14} /> {busy ? 'Creating…' : 'Create link'}</button
      >
    </div>
  {/if}
  <Notice message={error} />
</Modal>
