<script lang="ts">
  import { onMount } from 'svelte';
  import { ArrowRight, AudioLines, RefreshCw } from '@lucide/svelte';
  import { session } from './lib/session.svelte';
  import Shell from './components/Shell.svelte';
  onMount(() => {
    void session.load();
  });
</script>

{#if session.status === 'signed_in'}<Shell />
{:else}<main class="welcome">
    <a class="brand" href="/"><AudioLines size={29} /><span>wavelength</span></a
    >
    <div class="welcome-content">
      <p class="eyebrow">A little space for all your music</p>
      <h1>Stay for<br />the <em>music.</em></h1>
      <p>
        Your collection. Your discoveries.<br />Everything you love, in one
        place.
      </p>
      {#if session.status === 'loading'}<div
          class="welcome-loading"
          role="status"
        >
          <span class="spinner"></span> Checking your session…
        </div>{:else if session.status === 'error'}<div
          class="welcome-error"
          role="alert"
        >
          <p>{session.error}</p>
          <button class="button secondary" onclick={() => session.load()}
            ><RefreshCw size={16} /> Try again</button
          >
        </div>{:else}<a
          class="button primary sign-in"
          href="/auth/login?return_to=%2F">Sign in <ArrowRight size={18} /></a
        ><span class="sign-in-note">Continue with your connected account.</span
        >{/if}
    </div>
    <div class="welcome-art" aria-hidden="true">
      <div class="vinyl">
        <div class="vinyl-label">
          <AudioLines size={48} /><span>SIDE A · WAVELENGTH</span>
        </div>
      </div>
      <span class="art-caption">For the love of listening.</span>
    </div>
    <footer class="welcome-footer">
      Your music, on your terms.<span>WAVELENGTH / 01</span>
    </footer>
  </main>{/if}
