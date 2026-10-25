import { createHash, randomBytes } from 'node:crypto';
import { describe, expect, it, vi } from 'vitest';
import { OAuthSession, authenticatedURL, serverURL, type Vault } from '../../src/core/auth';

function setup() {
  const records = new Map<string, string>();
  const vault: Vault = {
    async get(k) {
      return records.get(k) ?? null;
    },
    async set(k, v) {
      records.set(k, v);
    },
    async remove(k) {
      records.delete(k);
    },
  };
  const calls: { url: string; body: Record<string, string> }[] = [];
  let now = 100000;
  const request = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
    const address = String(url),
      body =
        init?.headers &&
        (init.headers as Record<string, string>)['Content-Type'] === 'application/json'
          ? JSON.parse(String(init.body))
          : Object.fromEntries(new URLSearchParams(String(init?.body)));
    calls.push({ url: address, body });
    return new Response(
      JSON.stringify(
        address.endsWith('/oauth/register')
          ? { client_id: 'test-public-client' }
          : {
              access_token: 'private-test-access-' + calls.length,
              refresh_token: 'private-test-refresh-' + calls.length,
              token_type: 'Bearer',
              expires_in: 3600,
              scope: 'wavelength:read wavelength:write wavelength:stream wavelength:ingest',
            },
      ),
      { status: 200 },
    );
  });
  const session = new OAuthSession(
    vault,
    {
      random: () => randomBytes(32).toString('base64url'),
      challenge: async (value) => createHash('sha256').update(value).digest('base64url'),
    },
    request as typeof fetch,
    () => {},
    () => now,
  );
  async function begin() {
    return new URL(
      await session.begin('https://music.example', 'exp://192.168.1.2:8081/--/oauth/callback'),
    );
  }
  function callback(url: URL) {
    return `exp://192.168.1.2:8081/--/oauth/callback?code=one-time-test-code&iss=https%3A%2F%2Fmusic.example&state=${url.searchParams.get('state')}`;
  }
  return {
    session,
    records,
    vault,
    request,
    calls,
    begin,
    callback,
    advance: (ms: number) => {
      now += ms;
    },
  };
}
describe('server and media trust boundaries', () => {
  it('accepts secure servers and loopback development, normalizing their origin', () => {
    expect(serverURL(' https://music.example/ ')).toBe('https://music.example');
    expect(serverURL('http://127.0.0.1:8080')).toBe('http://127.0.0.1:8080');
  });
  it.each([
    'http://music.example',
    'https://user:password@music.example',
    'https://music.example/path',
    'javascript:alert(1)',
    'https://music.example?token=x',
    'https://music.example/#token',
  ])('rejects unsafe server %s', (value) => expect(() => serverURL(value)).toThrow());
  it('never attaches account credentials to a foreign artwork or media origin', () => {
    expect(authenticatedURL('https://music.example', '/artwork/id')).toBe(
      'https://music.example/artwork/id',
    );
    for (const url of [
      'https://other.example/a',
      '//other.example/a',
      'https://u:p@music.example/a',
    ])
      expect(() => authenticatedURL('https://music.example', url)).toThrow();
  });
});
describe('public client PKCE', () => {
  it('registers without a bundled secret and requests every required scope', async () => {
    const f = setup(),
      url = await f.begin();
    expect(f.calls[0].body.token_endpoint_auth_method).toBe('none');
    expect(f.calls[0].body).not.toHaveProperty('client_secret');
    expect(url.searchParams.get('code_challenge_method')).toBe('S256');
    expect(url.searchParams.get('scope')).toContain('wavelength:stream');
    const grant = await f.session.complete(f.callback(url));
    expect(grant.server).toBe('https://music.example');
    const verifier = f.calls.at(-1)!.body.code_verifier;
    expect(createHash('sha256').update(verifier).digest('base64url')).toBe(
      url.searchParams.get('code_challenge'),
    );
    expect(await f.vault.get('wavelength.oauth.pending.v1')).toBeNull();
  });
  it.each(['state', 'iss'])(
    'rejects wrong or repeated %s without exchanging a code',
    async (key) => {
      const f = setup(),
        url = await f.begin(),
        callback = new URL(f.callback(url));
      callback.searchParams.set(key, 'not-this-flow');
      await expect(f.session.complete(callback.toString())).rejects.toThrow('does not belong');
      expect(f.calls).toHaveLength(1);
      callback.searchParams.append(key, url.searchParams.get('state')!);
      await expect(f.session.complete(callback.toString())).rejects.toThrow();
      expect(f.calls).toHaveLength(1);
    },
  );
  it('rejects duplicate authorization codes and expired flows', async () => {
    const f = setup(),
      url = await f.begin();
    await expect(f.session.complete(f.callback(url) + '&code=another')).rejects.toThrow('missing');
    f.advance(600001);
    await expect(f.session.complete(f.callback(url))).rejects.toThrow('expired');
    expect(f.calls).toHaveLength(1);
  });
  it('deduplicates the browser event and callback route completing the same flow', async () => {
    const f = setup(),
      url = await f.begin();
    const results = await Promise.all([
      f.session.complete(f.callback(url)),
      f.session.complete(f.callback(url)),
    ]);
    expect(results[0]).toEqual(results[1]);
    expect(f.calls.filter((c) => c.url.endsWith('/oauth/token'))).toHaveLength(1);
  });
  it('refreshes once for concurrent requests and persists the rotated refresh token', async () => {
    const f = setup(),
      url = await f.begin();
    await f.session.complete(f.callback(url));
    f.advance(3600000);
    const tokens = await Promise.all([f.session.token(), f.session.token(), f.session.token()]);
    expect(new Set(tokens).size).toBe(1);
    expect(f.calls.filter((c) => c.body.grant_type === 'refresh_token')).toHaveLength(1);
    const next = setup();
    for (const [k, v] of f.records) await next.vault.set(k, v);
    const restored = await next.session.restore();
    expect(restored?.refreshToken).toBe(f.session.grant?.refreshToken);
  });
  it('clears local credentials and invalidates old client contexts on sign-out', async () => {
    const f = setup(),
      url = await f.begin();
    await f.session.complete(f.callback(url));
    const version = f.session.version;
    await f.session.logout();
    expect(f.session.grant).toBeNull();
    expect(f.records.size).toBe(0);
    expect(f.session.version).toBeGreaterThan(version);
    expect(f.calls.at(-1)?.url).toMatch('/oauth/revoke');
  });
  it('does not restore malformed credentials', async () => {
    const f = setup();
    await f.vault.set('wavelength.oauth.grant.v1', '{}');
    expect(await f.session.restore()).toBeNull();
  });
  it('an invalid refresh grant returns the app to sign-in without retaining credentials', async () => {
    const f = setup(),
      url = await f.begin();
    await f.session.complete(f.callback(url));
    f.advance(3600000);
    f.request.mockResolvedValueOnce(new Response('{}', { status: 401 }));
    await expect(f.session.token()).rejects.toThrow('sign in again');
    expect(f.session.grant).toBeNull();
    expect(f.records.size).toBe(0);
  });
  it('a refresh finishing after sign-out cannot resurrect the old session', async () => {
    const f = setup(),
      url = await f.begin();
    await f.session.complete(f.callback(url));
    f.advance(3600000);
    let finish!: (response: Response) => void;
    f.request.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const refreshing = f.session.token();
    await f.session.clear();
    finish(
      new Response(
        JSON.stringify({
          access_token: 'late-token',
          refresh_token: 'late-refresh',
          expires_in: 3600,
          token_type: 'Bearer',
        }),
      ),
    );
    await expect(refreshing).rejects.toThrow('changed');
    expect(f.session.grant).toBeNull();
    expect(f.records.size).toBe(0);
  });
});
