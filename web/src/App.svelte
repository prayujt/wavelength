<script lang="ts">
  import { onMount } from 'svelte';
  import { ArrowRight, AudioLines, RefreshCw } from '@lucide/svelte';
  import { session } from './lib/session.svelte';
  import Shell from './components/Shell.svelte';
  onMount(() => {
    void session.load();
  });
</script>

{#if session.status === 'signed_in'}<Shell />{:else}<main class="welcome">
    <section class="login-card">
      <a class="login-brand" href="/"
        ><AudioLines size={32} /><span>Wavelength</span></a
      >
      <h1>Your music library</h1>
      <p>Sign in to browse, listen, and download music.</p>
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
            ><RefreshCw size={14} /> Try again</button
          >
        </div>{:else}<a
          class="button primary sign-in"
          href="/auth/login?return_to=%2F">Sign in <ArrowRight size={14} /></a
        ><span class="sign-in-note">Continue with your connected account.</span
        >{/if}
    </section>
  </main>{/if}
