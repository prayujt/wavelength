import { afterEach, describe, expect, it, vi } from 'vitest';
import { Code, ConnectError } from '@connectrpc/connect';
import { createAPI } from '../../src/lib/api';
import { OAuthSession } from '../../src/core/auth';

function auth() {
  const session = new OAuthSession(
    {
      async get() {
        return null;
      },
      async set() {},
      async remove() {},
    },
    { random: () => '', challenge: async () => '' },
    globalThis.fetch,
  );
  session.grant = {
    server: 'https://music.example',
    clientId: 'public-test-client',
    accessToken: 'private-test-token',
    refreshToken: 'private-test-refresh',
    expiresAt: Date.now() + 3600000,
    scope: 'wavelength:read wavelength:write wavelength:stream wavelength:ingest',
  };
  return session;
}
afterEach(() => vi.unstubAllGlobals());
describe('authenticated native Connect transport', () => {
  it('calls existing RPC paths with a bearer header, omitting cookies and redirects', async () => {
    const request = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe(
        'https://music.example/wavelength.v1.UserDataService/GetCurrentUser',
      );
      expect(new Headers(init?.headers).get('Authorization')).toBe('Bearer private-test-token');
      expect(init?.credentials).toBe('omit');
      expect(init?.redirect).toBe('error');
      return new Response(JSON.stringify({ user: { id: 'listener', name: 'Listener' } }), {
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', request);
    const api = createAPI(auth());
    expect((await api.user.getCurrentUser({})).user?.id).toBe('listener');
    expect(request).toHaveBeenCalledTimes(1);
  });
  it('refreshes and retries an unauthenticated unary request once', async () => {
    const session = auth();
    vi.spyOn(session, 'token').mockImplementation(async (force) =>
      force ? 'rotated-test-token' : 'private-test-token',
    );
    const request = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ code: 'unauthenticated', message: 'expired' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      .mockImplementationOnce(async (_input: RequestInfo | URL, init?: RequestInit) => {
        expect(new Headers(init?.headers).get('Authorization')).toBe('Bearer rotated-test-token');
        return new Response(JSON.stringify({ tracks: [], total: 0 }), {
          headers: { 'Content-Type': 'application/json' },
        });
      });
    vi.stubGlobal('fetch', request);
    expect((await createAPI(session).library.listTracks({})).total).toBe(0);
    expect(session.token).toHaveBeenCalledWith(true);
    expect(request).toHaveBeenCalledTimes(2);
  });
  it('prevents stale requests from an old account context or another server', async () => {
    const session = auth(),
      api = createAPI(session),
      request = vi.fn();
    vi.stubGlobal('fetch', request);
    await session.clear();
    await expect(api.library.listTracks({})).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect(request).not.toHaveBeenCalled();
    const next = auth(),
      old = createAPI(next);
    next.grant = { ...next.grant!, server: 'https://other.example' };
    await expect(old.library.listTracks({})).rejects.toBeInstanceOf(ConnectError);
    expect(request).not.toHaveBeenCalled();
  });
});
