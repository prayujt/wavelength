import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { describe, expect, it, vi } from 'vitest';
import { LibraryService, UserDataService } from '../../src/gen/wavelength/v1/library_pb';
import { DiscoveryService, IngestionService } from '../../src/gen/wavelength/v1/ingestion_pb';
import { ListeningService } from '../../src/gen/wavelength/v1/listening_pb';
import { SharingService } from '../../src/gen/wavelength/v1/sharing_pb';
import { createPreviewFetch } from '../../src/preview/store';

function clients() {
  const transport = createConnectTransport({
    baseUrl: 'https://preview.wavelength.invalid',
    useBinaryFormat: false,
    fetch: createPreviewFetch(),
  });
  return {
    library: createClient(LibraryService, transport),
    user: createClient(UserDataService, transport),
    discovery: createClient(DiscoveryService, transport),
    ingestion: createClient(IngestionService, transport),
    listening: createClient(ListeningService, transport),
    sharing: createClient(SharingService, transport),
  };
}
describe('opt-in preview uses the actual generated wire contract', () => {
  it('serves library metadata and keeps mutations isolated per preview session', async () => {
    const a = clients(),
      b = clients();
    expect((await a.library.listTracks({ limit: 500 })).tracks).toHaveLength(40);
    const created = await a.user.createPlaylist({
      name: 'Test mix',
      trackIds: ['track-0-0', 'track-0-0', 'track-1-0'],
    });
    await a.user.updatePlaylist({
      id: created.playlist!.id,
      replaceTracks: { ids: ['track-1-0', 'track-0-0'] },
      comment: 'Notes',
      public: true,
    });
    const result = await a.user.getPlaylist({ id: created.playlist!.id });
    expect(result.playlist?.trackIds).toEqual(['track-1-0', 'track-0-0']);
    expect(result.playlist?.public).toBe(true);
    expect((await b.user.listPlaylists({})).total).toBe(3);
  });
  it('serializes queues and bookmarks with protobuf 64-bit millisecond values', async () => {
    const a = clients();
    await a.user.saveQueue({
      trackIds: ['track-0-0'],
      currentTrackId: 'track-0-0',
      positionMs: 52000n,
    });
    expect((await a.user.getQueue({})).queue?.positionMs).toBe(52000n);
    await a.user.saveBookmark({ trackId: 'track-0-0', positionMs: 3000n });
    expect((await a.user.listBookmarks({})).bookmarks[0].positionMs).toBe(3000n);
  });
  it('adds catalog downloads to the library with exact provenance after completion', async () => {
    const a = clients(),
      search = await a.discovery.search({ query: 'June Atlas' }),
      ref = search.tracks[0].ref!;
    const now = Date.now();
    vi.spyOn(Date, 'now').mockReturnValue(now);
    try {
      const job = await a.ingestion.createImport({
        requestId: 'preview-request-test',
        items: [{ metadataRef: ref }],
      });
      expect(job.state).toBe('running');
      vi.mocked(Date.now).mockReturnValue(now + 2500);
      await a.ingestion.listImports({});
      const found = await a.ingestion.lookupImports({ metadataRefs: [ref] });
      expect(found.tracks).toHaveLength(1);
      expect((await a.library.getTrack({ id: found.tracks[0].trackId })).track?.title).toBe(
        search.tracks[0].title,
      );
    } finally {
      vi.restoreAllMocks();
    }
  });
  it('supports recording selection, history, expiring shares and revocation', async () => {
    const a = clients();
    expect(
      (await a.discovery.findSources({ metadataRef: { provider: 'itunes', id: 'preview-song-0' } }))
        .candidates,
    ).toHaveLength(3);
    const session = await a.listening.startListeningSession({
      trackId: 'track-0-0',
      origin: 'direct',
      requestId: 'preview-listen-test',
    });
    await a.listening.finishListeningSession({
      sessionId: session.id,
      listenedMs: 12000n,
      positionMs: 12000n,
      outcome: 'stopped',
    });
    expect(
      (await a.listening.listListeningSessions({ outcome: 'stopped' })).sessions[0].listenedMs,
    ).toBe(12000n);
    const share = await a.sharing.createShare({ name: 'Test', trackIds: ['track-0-0'] });
    expect(share.link?.expiresAt).toBeDefined();
    await a.sharing.revokeAccessLink({ id: share.link!.id });
    expect((await a.sharing.listAccessLinks({ kind: 'share' })).links).toHaveLength(0);
  });
  it('refuses to forward preview requests to a live or third-party origin', async () => {
    await expect(createPreviewFetch()('https://music.example/api')).rejects.toThrow('cannot leave');
  });
});
