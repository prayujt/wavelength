/* OAuth bridge only: credentials are never sent to a relay service or persisted. */
(() => {
  const callback = new URL(window.location.href);
  // Remove the one-time code from browser history before doing anything else.
  window.history.replaceState(null, '', callback.pathname);
  const q = callback.searchParams;
  const fail = () => {
    document.getElementById('heading').textContent =
      'Start again from the app.';
    document.getElementById('message').textContent =
      'This return link is incomplete or invalid. Open Wavelength and start a new sign-in.';
    document.getElementById('hint').hidden = true;
  };
  try {
    for (const key of ['return_uri', 'state', 'iss'])
      if (q.getAll(key).length !== 1) throw new Error();
    if (q.get('iss') !== callback.origin || !q.get('state')) throw new Error();
    const target = new URL(q.get('return_uri'));
    const native =
      target.protocol === 'wavelength:' &&
      target.hostname === 'oauth' &&
      target.pathname === '/callback';
    const expo =
      ['exp:', 'exps:'].includes(target.protocol) &&
      target.hostname &&
      target.pathname.endsWith('/oauth/callback');
    if (
      (!native && !expo) ||
      target.username ||
      target.password ||
      target.search ||
      target.hash
    )
      throw new Error();
    if (q.has('error')) {
      if (q.getAll('error').length !== 1) throw new Error();
      target.searchParams.set('error', 'access_denied');
      document.getElementById('heading').textContent =
        'No problem. Try again anytime.';
      document.getElementById('message').textContent =
        'Sign-in was not approved. Return to Wavelength when you’re ready to try again.';
    } else {
      if (q.getAll('code').length !== 1 || !q.get('code')) throw new Error();
      target.searchParams.set('code', q.get('code'));
    }
    target.searchParams.set('state', q.get('state'));
    target.searchParams.set('iss', q.get('iss'));
    const link = document.getElementById('return');
    link.href = target.toString();
    link.hidden = false;
    const copy = document.getElementById('copy');
    copy.hidden = false;
    copy.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(target.toString());
        document.getElementById('status').textContent =
          'Return link copied. Paste it into Wavelength on this device.';
      } catch {
        document.getElementById('status').textContent =
          'Your browser could not copy this link. Start sign-in again using your normal browser.';
      }
    });
    // A deliberate tap works in system browsers and Expo Go without custom native modules.
  } catch {
    fail();
  }
})();
