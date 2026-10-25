export const SCOPES = 'wavelength:read wavelength:write wavelength:stream wavelength:ingest';
const GRANT_KEY = 'wavelength.oauth.grant.v1';
const PENDING_KEY = 'wavelength.oauth.pending.v1';
export class SessionExpired extends Error {}

export interface Vault {
  get(key: string, state?: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string, state?: string): Promise<void>;
  lock?<T>(run: () => Promise<T>): Promise<T>;
}
export interface OAuthCrypto {
  random(): string;
  challenge(verifier: string): Promise<string>;
}
export type Grant = {
  server: string;
  clientId: string;
  accessToken: string;
  refreshToken: string;
  expiresAt: number;
  scope: string;
  accountId?: string;
};
type Pending = {
  server: string;
  clientId: string;
  redirectUri: string;
  state: string;
  verifier: string;
  expiresAt: number;
};
type TokenResponse = {
  access_token: string;
  refresh_token?: string;
  expires_in: number;
  token_type: string;
  scope?: string;
};

export function serverURL(input: string): string {
  let url: URL;
  try {
    url = new URL(input.trim());
  } catch {
    throw new Error('Enter a valid server address.');
  }
  const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
  if (
    (url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback)) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    (url.pathname !== '/' && url.pathname !== '')
  ) {
    throw new Error('Use an HTTPS server address without a path, query, or credentials.');
  }
  return url.origin;
}

export function authenticatedURL(server: string, path: string): string {
  const base = serverURL(server);
  const url = new URL(path, base);
  if (url.origin !== base || url.username || url.password || url.hash)
    throw new Error('The server returned an unsafe media address.');
  return url.toString();
}

export class OAuthSession {
  grant: Grant | null = null;
  private refreshing?: Promise<string>;
  private completing?: Promise<Grant>;
  private generation = 0;
  private credentialWrites: Promise<unknown> = Promise.resolve();
  get version() {
    return this.generation;
  }

  constructor(
    private vault: Vault,
    private crypto: OAuthCrypto,
    private request: typeof fetch,
    private changed: (grant: Grant | null) => void = () => {},
    private now = Date.now,
  ) {}

  async restore(): Promise<Grant | null> {
    const raw = await this.vault.get(GRANT_KEY);
    if (!raw) {
      if (this.grant) {
        this.generation++;
        this.grant = null;
        this.changed(null);
      }
      return null;
    }
    try {
      const grant = JSON.parse(raw) as Grant;
      serverURL(grant.server);
      if (
        !grant.accessToken ||
        !grant.refreshToken ||
        !grant.clientId ||
        !Number.isFinite(grant.expiresAt)
      )
        throw new Error();
      if (this.grant && this.grant.clientId !== grant.clientId) this.generation++;
      this.grant = grant;
      this.changed(grant);
      return grant;
    } catch {
      await this.clear();
      return null;
    }
  }

  private async post(server: string, path: string, body: URLSearchParams | object) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 20000);
    try {
      const form = body instanceof URLSearchParams;
      // Browser fetch is a host function: calling it as an OAuthSession method
      // supplies an invalid receiver (native and Node fetch do not reveal this).
      const request = this.request;
      const response = await request(server + path, {
        method: 'POST',
        signal: controller.signal,
        headers: {
          'Content-Type': form ? 'application/x-www-form-urlencoded' : 'application/json',
          Accept: 'application/json',
        },
        body: form ? body.toString() : JSON.stringify(body),
        credentials: 'omit',
        redirect: 'error',
      });
      if (response.status === 400 || response.status === 401)
        throw new SessionExpired('Please sign in again.');
      if (!response.ok) throw new Error('Wavelength could not complete sign-in. Try again.');
      return await response.json();
    } finally {
      clearTimeout(timeout);
    }
  }

  async begin(input: string, returnUri: string, web = false): Promise<string> {
    const server = serverURL(input);
    const redirectUri = web
      ? returnUri
      : server + '/mobile-auth.html?return_uri=' + encodeURIComponent(returnUri);
    if (web && !['https:', 'http:'].includes(new URL(returnUri).protocol))
      throw new Error('Invalid browser return address.');
    const client = await this.post(server, '/oauth/register', {
      client_name: 'Wavelength Mobile',
      redirect_uris: [redirectUri],
      token_endpoint_auth_method: 'none',
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
    });
    if (typeof client.client_id !== 'string' || !client.client_id)
      throw new Error('The server did not register this app.');
    const verifier = this.crypto.random();
    const pending: Pending = {
      server,
      redirectUri,
      clientId: client.client_id,
      state: this.crypto.random(),
      verifier,
      expiresAt: this.now() + 600000,
    };
    await this.vault.set(PENDING_KEY, JSON.stringify(pending));
    const url = new URL(server + '/oauth/authorize');
    url.search = new URLSearchParams({
      response_type: 'code',
      client_id: client.client_id,
      redirect_uri: redirectUri,
      state: pending.state,
      code_challenge: await this.crypto.challenge(verifier),
      code_challenge_method: 'S256',
      resource: server + '/mcp',
      scope: SCOPES,
    }).toString();
    return url.toString();
  }

  complete(callback: string): Promise<Grant> {
    if (this.completing) return this.completing;
    this.completing = this.exchange(callback).finally(() => {
      this.completing = undefined;
    });
    return this.completing;
  }

  private async exchange(callback: string): Promise<Grant> {
    let url: URL;
    try {
      url = new URL(callback.trim());
    } catch {
      throw new Error('Paste the complete return link from your browser.');
    }
    const generation = this.generation;
    const raw = await this.vault.get(PENDING_KEY, url.searchParams.get('state') || undefined);
    if (!raw) throw new Error('Start a new sign-in from this device.');
    const pending = JSON.parse(raw) as Pending;
    if (pending.expiresAt <= this.now()) throw new Error('This sign-in expired. Start a new one.');
    const q = url.searchParams;
    if (
      ['state', 'iss'].some((key) => q.getAll(key).length !== 1) ||
      q.get('state') !== pending.state ||
      q.get('iss') !== pending.server
    )
      throw new Error('This sign-in does not belong to this device or server.');
    if (q.has('error')) throw new Error('Sign-in was not approved. You can try again.');
    if (q.getAll('code').length !== 1 || !q.get('code'))
      throw new Error('The return link is missing its sign-in code.');
    const token = (await this.post(
      pending.server,
      '/oauth/token',
      new URLSearchParams({
        grant_type: 'authorization_code',
        code: q.get('code')!,
        client_id: pending.clientId,
        code_verifier: pending.verifier,
        redirect_uri: pending.redirectUri,
        resource: pending.server + '/mcp',
      }),
    )) as TokenResponse;
    if (generation !== this.generation) throw new Error('Sign-in was cancelled.');
    const grant = this.tokenGrant(pending.server, pending.clientId, token);
    await this.vault.remove(PENDING_KEY, pending.state);
    const commit = async () => {
      if (generation !== this.generation) throw new Error('Sign-in was cancelled.');
      this.generation++;
      await this.save(grant);
    };
    if (this.vault.lock) await this.vault.lock(commit);
    else await commit();
    return grant;
  }

  private tokenGrant(
    server: string,
    clientId: string,
    token: TokenResponse,
    previous?: Grant,
  ): Grant {
    if (
      !token.access_token ||
      token.token_type?.toLowerCase() !== 'bearer' ||
      !Number.isFinite(token.expires_in) ||
      token.expires_in <= 0 ||
      !(token.refresh_token || previous?.refreshToken)
    )
      throw new Error('The server returned an incomplete sign-in.');
    return {
      server,
      clientId,
      accessToken: token.access_token,
      refreshToken: token.refresh_token || previous!.refreshToken,
      expiresAt: this.now() + token.expires_in * 1000,
      scope: token.scope ?? previous?.scope ?? '',
      accountId: previous?.accountId,
    };
  }

  private write<T>(operation: () => Promise<T>): Promise<T> {
    const next = this.credentialWrites.catch(() => {}).then(operation);
    this.credentialWrites = next;
    return next;
  }
  async identifyAccount(accountId: string) {
    if (!this.grant || !accountId) throw new Error('Please sign in again.');
    const identify = async () => {
      const raw = this.vault.lock ? await this.vault.get(GRANT_KEY) : null;
      const current = raw ? (JSON.parse(raw) as Grant) : this.grant;
      if (!current || (this.vault.lock && !raw) || current.clientId !== this.grant?.clientId)
        throw new Error('Sign-in changed.');
      await this.save({ ...current, accountId });
    };
    if (this.vault.lock) await this.vault.lock(identify);
    else await identify();
  }
  private async save(grant: Grant) {
    const generation = this.generation;
    await this.write(async () => {
      if (generation !== this.generation) throw new Error('Sign-in changed.');
      await this.vault.set(GRANT_KEY, JSON.stringify(grant));
      if (generation !== this.generation) throw new Error('Sign-in changed.');
      this.grant = grant;
      this.changed(grant);
    });
  }

  async token(force = false): Promise<string> {
    if (!this.grant) throw new Error('Please sign in to Wavelength.');
    if (!force && this.grant.expiresAt > this.now() + 60000) return this.grant.accessToken;
    if (this.refreshing) return this.refreshing;
    const grant = this.grant,
      generation = this.generation;
    const refresh = async () => {
      // Another window may have rotated this family while we waited for the lock.
      if (this.vault.lock) {
        const raw = await this.vault.get(GRANT_KEY);
        if (!raw) throw new SessionExpired('Please sign in again.');
        const saved = JSON.parse(raw) as Grant;
        if (saved.server !== grant.server || saved.clientId !== grant.clientId)
          throw new Error('Sign-in changed in another window.');
        this.grant = saved;
        if (
          saved.expiresAt > this.now() + 60000 &&
          (!force || saved.accessToken !== grant.accessToken)
        )
          return saved.accessToken;
      }
      const current = this.grant!;
      const token = (await this.post(
        current.server,
        '/oauth/token',
        new URLSearchParams({
          grant_type: 'refresh_token',
          client_id: current.clientId,
          refresh_token: current.refreshToken,
          resource: current.server + '/mcp',
        }),
      )) as TokenResponse;
      if (generation !== this.generation) throw new Error('Sign-in changed.');
      const next = this.tokenGrant(current.server, current.clientId, token, current);
      await this.save(next);
      return next.accessToken;
    };
    this.refreshing = (this.vault.lock ? this.vault.lock(refresh) : refresh())
      .catch(async (error) => {
        if (error instanceof SessionExpired && generation === this.generation) await this.clear();
        throw error;
      })
      .finally(() => {
        this.refreshing = undefined;
      });
    return this.refreshing;
  }

  async clear() {
    const clear = async () => {
      const raw = this.vault.lock ? await this.vault.get(GRANT_KEY) : null;
      const previous = raw ? (JSON.parse(raw) as Grant) : this.grant;
      this.generation++;
      this.grant = null;
      this.changed(null);
      await this.write(() =>
        Promise.all([this.vault.remove(GRANT_KEY), this.vault.remove(PENDING_KEY)]),
      );
      return previous;
    };
    return this.vault.lock ? this.vault.lock(clear) : clear();
  }

  async logout() {
    const grant = await this.clear();
    if (grant) {
      try {
        await this.post(
          grant.server,
          '/oauth/revoke',
          new URLSearchParams({ client_id: grant.clientId, token: grant.refreshToken }),
        );
      } catch {
        /* Local credentials are cleared even when the server is unreachable. */
      }
    }
  }
}
