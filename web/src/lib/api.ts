import {
  Code,
  ConnectError,
  createClient,
  type Interceptor,
} from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import {
  LibraryService,
  UserDataService,
} from '../gen/wavelength/v1/library_pb';
import {
  DiscoveryService,
  IngestionService,
} from '../gen/wavelength/v1/ingestion_pb';

export function createAPI(
  baseUrl: string,
  csrf: () => string,
  expired: () => void,
) {
  const authenticate: Interceptor = (next) => async (request) => {
    request.header.set('X-CSRF-Token', csrf());
    try {
      return await next(request);
    } catch (error) {
      if (ConnectError.from(error).code === Code.Unauthenticated) expired();
      throw error;
    }
  };
  const transport = createConnectTransport({
    baseUrl,
    interceptors: [authenticate],
    fetch: (input, init) =>
      fetch(input, { ...init, credentials: 'same-origin' }),
  });
  return {
    library: createClient(LibraryService, transport),
    user: createClient(UserDataService, transport),
    discovery: createClient(DiscoveryService, transport),
    ingestion: createClient(IngestionService, transport),
  };
}

export function errorMessage(error: unknown): string {
  if (error instanceof ConnectError) return error.rawMessage;
  return error instanceof Error
    ? error.message
    : 'Something went wrong. Please try again.';
}
