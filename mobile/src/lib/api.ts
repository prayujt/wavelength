import { createClient, Code, ConnectError, type Interceptor } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { fetch as nativeFetch } from 'expo/fetch';
import { LibraryService, UserDataService } from '../gen/wavelength/v1/library_pb';
import { DiscoveryService, IngestionService } from '../gen/wavelength/v1/ingestion_pb';
import { SharingService } from '../gen/wavelength/v1/sharing_pb';
import { ListeningService } from '../gen/wavelength/v1/listening_pb';
import { RealtimeService } from '../gen/wavelength/v1/realtime_pb';
import type { OAuthSession } from '../core/auth';
import { createPreviewFetch } from '../preview/store';

export function createAPI(auth: OAuthSession, preview = false, version = auth.version) {
  const server = auth.grant?.server;
  const authenticate: Interceptor = (next) => async (request) => {
    if (preview) return next(request);
    if (auth.version !== version || auth.grant?.server !== server)
      throw new ConnectError('Sign-in changed.', Code.Unauthenticated);
    request.header.set('Authorization', 'Bearer ' + (await auth.token()));
    try {
      return await next(request);
    } catch (error) {
      if (ConnectError.from(error).code !== Code.Unauthenticated) throw error;
      try {
        request.header.set('Authorization', 'Bearer ' + (await auth.token(true)));
        return await next(request);
      } catch (retryError) {
        if (ConnectError.from(retryError).code === Code.Unauthenticated) await auth.clear();
        throw retryError;
      }
    }
  };
  const options = {
    baseUrl: preview ? 'https://preview.wavelength.invalid' : auth.grant!.server,
    useBinaryFormat: false,
    interceptors: [authenticate],
    fetch: preview
      ? createPreviewFetch()
      : (((input, init) =>
          nativeFetch(input, { ...init, credentials: 'omit', redirect: 'error' })) as typeof fetch),
  };
  const transport = createConnectTransport({ ...options, defaultTimeoutMs: 45000 });
  return {
    library: createClient(LibraryService, transport),
    user: createClient(UserDataService, transport),
    discovery: createClient(DiscoveryService, transport),
    ingestion: createClient(IngestionService, transport),
    sharing: createClient(SharingService, transport),
    listening: createClient(ListeningService, transport),
    realtime: createClient(RealtimeService, createConnectTransport(options)),
  };
}
export type API = ReturnType<typeof createAPI>;
export function errorMessage(error: unknown) {
  if (error instanceof ConnectError) {
    if (error.code === Code.PermissionDenied)
      return 'Your connection needs permission for this. Reconnect from Settings.';
    if (error.code === Code.Unavailable)
      return 'Wavelength is taking a moment. Check your connection and try again.';
    if (error.code === Code.Unauthenticated) return 'Your session ended. Please sign in again.';
    return error.rawMessage;
  }
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}
