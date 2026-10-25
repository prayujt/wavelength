<script lang="ts">
  import { timestampFromDate } from '@bufbuild/protobuf/wkt';
  import { Copy, Link, ShieldCheck } from '@lucide/svelte';
  import type { AccessLink } from '../gen/wavelength/v1/sharing_pb';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Dropdown from './Dropdown.svelte';
  import Modal from './Modal.svelte';
  import Notice from './Notice.svelte';
  let {
    name,
    trackIds,
    close,
  }: {
    name: string;
    trackIds: string[];
    close: () => void;
  } = $props();
  let expiry = $state('2592000');
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
      const response = await api.sharing.createShare({
        name,
        trackIds,
        noExpiry: expiry === '0',
        expiresAt: Number(expiry)
          ? timestampFromDate(new Date(Date.now() + Number(expiry) * 1000))
          : undefined,
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
  label="Share music"
  close={() => {
    if (!busy) close();
  }}
>
  <h2>Share music</h2>
  <p class="subtitle">{name}</p>
  <p class="muted">
    Anyone with this link can open a player and listen to these songs. No
    account needed.
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
  {:else}<label for="share-expiry">Expires</label>
    <Dropdown
      id="share-expiry"
      label="Share expiry"
      value={expiry}
      onchange={(value) => (expiry = value)}
      disabled={busy}
      options={[
        { value: '86400', label: 'In 1 day' },
        { value: '604800', label: 'In 7 days' },
        { value: '2592000', label: 'In 30 days' },
        { value: '0', label: 'Never' },
      ]}
    />
    {#if trackIds.length > 1}<p class="muted">
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
