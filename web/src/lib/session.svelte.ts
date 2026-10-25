import { createAPI, errorMessage } from './api';

type User = { id: string; name: string; email: string };

class Session {
  status = $state<'loading' | 'signed_in' | 'signed_out' | 'error'>('loading');
  user = $state<User | undefined>();
  csrf = $state('');
  error = $state('');
  signingOut = $state(false);

  async load() {
    this.status = 'loading';
    this.error = '';
    try {
      const response = await fetch('/auth/me', {
        credentials: 'same-origin',
        cache: 'no-store',
      });
      if (response.status === 401) {
        this.expire();
        return;
      }
      if (response.status === 404)
        throw new Error(
          'Sign-in is not configured on this server. Configure OIDC to use the web app.',
        );
      if (!response.ok)
        throw new Error('Unable to reach your account. Please try again.');
      const data = await response.json();
      this.user = data.user;
      this.csrf = data.csrf_token;
      this.status = 'signed_in';
    } catch (error) {
      this.error = errorMessage(error);
      this.status = 'error';
    }
  }

  expire() {
    this.user = undefined;
    this.csrf = '';
    this.status = 'signed_out';
  }

  async logout() {
    this.signingOut = true;
    try {
      const response = await fetch('/auth/logout', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': this.csrf },
      });
      if (!response.ok && response.status !== 401)
        throw new Error('Could not sign out. Please try again.');
      this.expire();
    } finally {
      this.signingOut = false;
    }
  }
}

export const session = new Session();
export const api = createAPI(
  window.location.origin,
  () => session.csrf,
  () => session.expire(),
);
