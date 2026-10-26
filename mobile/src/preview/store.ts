/** An opt-in, entirely in-memory collection. Never intercepts a real server's requests. */
type Row = Record<string, unknown>;
const now = () => new Date().toISOString();
const albumSeeds = [
  ['The quiet between', 'Rowan Hale', 'Folk'],
  ['Blue hour', 'Mira Sol', 'Alternative'],
  ['Soft focus', 'Northline', 'Electronic'],
  ['Somewhere quieter', 'Oliver Vale', 'Indie'],
  ['Field notes', 'Rowan Hale', 'Folk'],
  ['A lighter kind of blue', 'Mira Sol', 'Jazz'],
  ['Open water', 'Northline', 'Electronic'],
  ['Nothing stays still', 'Oliver Vale', 'Alternative'],
];
const titles = [
  'Weightless',
  'A little further',
  'Where the light goes',
  'Coming home',
  'After the rain',
];

export function createPreviewFetch(): typeof fetch {
  let counter = 50;
  const tracks: Row[] = [],
    albums: Row[] = [],
    artists: Row[] = [];
  for (let a = 0; a < albumSeeds.length; a++) {
    const [name, artist, genre] = albumSeeds[a];
    const artistId = `artist-${a % 4}`,
      albumId = `album-${a}`;
    albums.push({
      id: albumId,
      name,
      artist,
      artistId,
      genre,
      year: 2024 + (a % 3),
      trackCount: 5,
      durationSeconds: 1090,
      createdAt: now(),
    });
    if (a < 4) artists.push({ id: artistId, name: artist, albumCount: 2 });
    for (let n = 0; n < 5; n++)
      tracks.push({
        id: `track-${a}-${n}`,
        title:
          a === 0
            ? titles[n]
            : ['First light', 'Slow motion', 'In the distance', 'Daydream', 'Still here'][n],
        artist,
        album: name,
        albumArtist: artist,
        artistId,
        albumId,
        genre,
        year: 2024 + (a % 3),
        durationSeconds: 180 + n * 17,
        trackNumber: n + 1,
        discNumber: 1,
        contentType: 'audio/mpeg',
        bitRateKbps: 256,
        sizeBytes: '4200000',
        createdAt: now(),
        modifiedAt: now(),
        artworkPath: '',
      });
  }
  let playlists: Row[] = [
    {
      id: 'playlist-1',
      name: 'The slow hours',
      comment: 'For a softer landing.',
      trackIds: ['track-0-0', 'track-1-2', 'track-3-1', 'track-0-4'],
    },
    {
      id: 'playlist-2',
      name: 'Windows down',
      comment: 'Somewhere with no particular destination.',
      trackIds: ['track-2-0', 'track-6-1', 'track-1-0'],
    },
    {
      id: 'playlist-3',
      name: 'Everything in between',
      comment: '',
      trackIds: ['track-4-0', 'track-5-1', 'track-7-2'],
    },
  ].map((p) => ({
    ...p,
    ownerId: 'preview-user',
    public: false,
    createdAt: now(),
    changedAt: now(),
  }));
  const external = {
    ref: { provider: 'itunes', id: 'preview-release' },
    title: 'Night swimming',
    artist: 'June Atlas',
    artistRef: { provider: 'itunes', id: 'preview-artist' },
    artworkUrl: '',
    tracks: ['Night swimming', 'Half awake', 'Satellites'].map((title, n) => ({
      ref: { provider: 'itunes', id: `preview-song-${n}` },
      title,
      artist: 'June Atlas',
      album: 'Night swimming',
      albumArtist: 'June Atlas',
      durationSeconds: 210 + n * 10,
      trackNumber: n + 1,
      discNumber: 1,
      genre: 'Alternative',
      releaseDate: '2026-07-10',
      artworkUrl: '',
      releaseRef: { provider: 'itunes', id: 'preview-release' },
      artistRef: { provider: 'itunes', id: 'preview-artist' },
    })),
  };
  const source = (id: string) => external.tracks.find((t) => t.ref.id === id) || external.tracks[0];
  const imported = new Map<string, string>();
  let queue: Row = { trackIds: [], positionMs: '0' };
  let bookmarks: Row[] = [],
    links: Row[] = [];
  const history: Row[] = [
    {
      id: 'history-preview',
      track: tracks[0],
      artistId: 'artist-0',
      albumId: 'album-0',
      origin: 'direct',
      outcome: 'completed',
      listenedMs: '165000',
      positionMs: '180000',
      startedAt: now(),
      updatedAt: now(),
      endedAt: now(),
      playing: false,
    },
  ];
  const jobs: Row[] = [
    {
      id: 'preview-import',
      state: 'needs_selection',
      createdAt: now(),
      updatedAt: now(),
      items: [
        {
          metadataRef: external.tracks[0].ref,
          metadata: external.tracks[0],
          state: 'needs_selection',
          error: '',
          warnings: [],
        },
      ],
    },
  ];
  const progressJobs = () => {
    for (const job of jobs) {
      if (job.state !== 'running') continue;
      if (Date.now() - Number(job.started) < 1800) continue;
      for (const item of job.items as Row[]) {
        const ref = item.metadataRef as { provider: string; id: string };
        const metadata = source(ref.id);
        const track = {
          id: 'download-' + ref.id,
          title: metadata.title,
          artist: metadata.artist,
          album: metadata.album,
          albumArtist: metadata.albumArtist,
          trackNumber: metadata.trackNumber,
          durationSeconds: metadata.durationSeconds,
          genre: metadata.genre,
          artworkPath: '',
          artistId: 'artist-preview-june',
          albumId: 'album-preview-night',
          contentType: 'audio/mpeg',
        };
        if (!albums.some((a) => a.id === 'album-preview-night')) {
          albums.unshift({
            id: 'album-preview-night',
            name: metadata.album,
            artist: metadata.artist,
            artistId: 'artist-preview-june',
            trackCount: 3,
            durationSeconds: 660,
            year: 2026,
          });
          artists.push({ id: 'artist-preview-june', name: metadata.artist, albumCount: 1 });
        }
        if (!tracks.some((t) => t.id === track.id)) tracks.push(track);
        imported.set(ref.provider + ':' + ref.id, track.id);
        item.state = 'completed';
        item.trackId = track.id;
      }
      job.state = 'completed';
      job.updatedAt = now();
    }
  };
  const page = (items: Row[], input: Row) => ({
    items: items.slice(
      Number(input.offset || 0),
      Number(input.offset || 0) + Number(input.limit || 100),
    ),
    total: items.length,
  });
  const find = (items: Row[], id: unknown) => items.find((i) => i.id === id);
  const matches = (items: Row[], query: unknown) =>
    items.filter(
      (x) => !query || JSON.stringify(x).toLowerCase().includes(String(query).toLowerCase()),
    );
  const response = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

  return (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url,
    );
    if (url.origin !== 'https://preview.wavelength.invalid')
      throw new Error('Preview requests cannot leave the preview collection.');
    const parts = url.pathname.split('/');
    const service = parts.at(-2)!,
      method = parts.at(-1)!;
    const body = init?.body;
    const args = JSON.parse(
      typeof body === 'string' ? body : body ? await new Response(body).text() : '{}',
    ) as Row;
    progressJobs();
    let result: unknown;
    switch (method) {
      case 'GetCurrentUser':
        result = {
          user: { id: 'preview-user', name: 'Alex Morgan', email: 'preview@wavelength.example' },
        };
        break;
      case 'ListTracks': {
        const p = page(matches(tracks, args.query), args);
        result = { tracks: p.items, total: p.total };
        break;
      }
      case 'GetTrack': {
        const track = find(tracks, args.id);
        if (!track)
          return response(
            { code: 'not_found', message: 'This song is not in the preview collection.' },
            404,
          );
        result = { track };
        break;
      }
      case 'ListAlbums': {
        const p = page(
          matches(
            albums.filter((a) => !args.artistId || a.artistId === args.artistId),
            args.query,
          ),
          args,
        );
        result = { albums: p.items, total: p.total };
        break;
      }
      case 'GetAlbum':
        result = {
          album: find(albums, args.id),
          tracks: tracks.filter((t) => t.albumId === args.id),
        };
        break;
      case 'ListArtists': {
        const p = page(matches(artists, args.query), args);
        result = { artists: p.items, total: p.total };
        break;
      }
      case 'GetArtist':
        result = service.includes('Discovery')
          ? { artist: { ref: external.artistRef, name: external.artist, releases: [external] } }
          : {
              artist: find(artists, args.id),
              albums: albums.filter((a) => a.artistId === args.id),
            };
        break;
      case 'ListPlaylists':
        result = { playlists, total: playlists.length };
        break;
      case 'GetPlaylist':
        result = { playlist: find(playlists, args.id) };
        break;
      case 'CreatePlaylist': {
        const p = {
          id: 'playlist-' + ++counter,
          name: args.name,
          trackIds: args.trackIds || [],
          ownerId: 'preview-user',
          comment: '',
          public: false,
          createdAt: now(),
          changedAt: now(),
        };
        playlists.unshift(p);
        result = { playlist: p };
        break;
      }
      case 'UpdatePlaylist': {
        const p = find(playlists, args.id);
        if (!p) return response({ code: 'not_found', message: 'Playlist not found.' }, 404);
        for (const key of ['name', 'comment', 'public']) if (key in args) p[key] = args[key];
        if (args.replaceTracks) p.trackIds = (args.replaceTracks as Row).ids;
        if (args.addTrackIds)
          p.trackIds = [...(p.trackIds as string[]), ...(args.addTrackIds as string[])];
        if (args.removeIndices)
          p.trackIds = (p.trackIds as string[]).filter(
            (_, i) => !(args.removeIndices as number[]).includes(i),
          );
        p.changedAt = now();
        result = { playlist: p };
        break;
      }
      case 'DeletePlaylist':
        playlists = playlists.filter((p) => p.id !== args.id);
        result = {};
        break;
      case 'GetPlayFeed': {
        const blocked = new Set([
          args.seedTrackId,
          ...(Array.isArray(args.queuedTrackIds) ? args.queuedTrackIds : []),
          ...(Array.isArray(args.excludedTrackIds) ? args.excludedTrackIds : []),
        ]);
        const available = tracks.filter((track) => !blocked.has(track.id));
        const start = Math.max(0, Number(args.cursor || 0));
        const selected = available.slice(start, start + Math.min(25, Number(args.pageSize || 10)));
        const next = start + selected.length;
        result = {
          feedId: 'preview-feed',
          items: selected.map((track) => ({ id: 'preview-feed-' + track.id, track })),
          nextCursor: next < available.length ? String(next) : '',
          exhausted: next >= available.length,
        };
        break;
      }
      case 'GetQueue':
        result = { queue };
        break;
      case 'SaveQueue':
        queue = { ...args, changedAt: now(), changedBy: 'native' };
        result = { queue };
        break;
      case 'ListBookmarks':
        result = { bookmarks, total: bookmarks.length };
        break;
      case 'SaveBookmark':
        bookmarks = [
          ...bookmarks.filter((b) => b.trackId !== args.trackId),
          { ...args, createdAt: now(), changedAt: now() },
        ];
        result = {};
        break;
      case 'DeleteBookmark':
        bookmarks = bookmarks.filter((b) => b.trackId !== args.trackId);
        result = {};
        break;
      case 'GetHistory':
        result = {
          entries: history.map((h) => ({
            trackId: (h.track as Row).id,
            plays: 1,
            lastPlayedAt: now(),
          })),
          total: history.length,
        };
        break;
      case 'RecordPlay':
      case 'GetNowPlaying':
        result = {};
        break;
      case 'GetLyrics':
        result = {
          lyrics:
            '[00:00.00]A little room to breathe\n[00:03.00]A little less to carry\n[00:06.00]We follow where the light goes\n[00:10.00]And let the quiet in\n[00:13.00]Somewhere we begin again',
        };
        break;
      case 'GetPlaybackInfo':
        result = { track: find(tracks, args.id), httpPath: '/preview-audio', supportsRanges: true };
        break;
      case 'Search':
        result = {
          tracks: external.tracks,
          releases: [external],
          artists: [{ ref: external.artistRef, name: external.artist, artworkUrl: '' }],
        };
        break;
      case 'GetRelease':
        result = { release: external };
        break;
      case 'GetReleases':
        result = { releases: [external] };
        break;
      case 'LookupImports':
        result = {
          tracks: [...imported].map(([key, trackId]) => {
            const [provider, id] = key.split(':');
            return { metadataRef: { provider, id }, trackId };
          }),
        };
        break;
      case 'FindSources':
        result = {
          candidates: [
            {
              ref: { provider: 'youtube', id: 'preview-official' },
              title: external.tracks[0].title + ' (Official Audio)',
              uploader: 'June Atlas',
              durationSeconds: 210,
              score: 0.981,
              suitable: true,
              reason: 'Matching recording · Preferred source',
            },
            {
              ref: { provider: 'youtube', id: 'preview-alternative' },
              title: external.tracks[0].title + ' (Lyrics)',
              uploader: 'Indie evenings',
              durationSeconds: 211,
              score: 0.932,
              suitable: true,
              reason: 'Matching recording · Alternative source',
            },
            {
              ref: { provider: 'youtube', id: 'preview-live' },
              title: external.tracks[0].title + ' (Live)',
              uploader: 'June Atlas',
              durationSeconds: 256,
              score: 0.21,
              suitable: false,
              reason: 'Different version: live',
            },
          ],
        };
        break;
      case 'CreateImport': {
        const items =
          (args.items as Row[] | undefined) || external.tracks.map((t) => ({ metadataRef: t.ref }));
        const job = {
          id: 'import-' + ++counter,
          state: 'running',
          started: Date.now(),
          items: items.map((item) => ({
            ...item,
            metadata: source((item.metadataRef as Row).id as string),
            state: 'downloading',
            warnings: [],
          })),
          createdAt: now(),
          updatedAt: now(),
        };
        jobs.unshift(job);
        result = job;
        break;
      }
      case 'ListImports': {
        const p = page(jobs, args);
        result = { imports: p.items.map(({ started: _, ...j }) => j), total: p.total };
        break;
      }
      case 'GetImport':
        result = find(jobs, args.id);
        break;
      case 'CancelImport': {
        const job = find(jobs, args.id)!;
        job.state = 'cancelled';
        result = job;
        break;
      }
      case 'RetryImport': {
        const job = find(jobs, args.id)!;
        job.state = 'running';
        job.started = Date.now();
        job.updatedAt = now();
        for (const item of job.items as Row[]) item.state = 'downloading';
        const { started: _, ...wire } = job;
        result = wire;
        break;
      }
      case 'CreatePlaybackLink': {
        const link = {
          id: 'playback-' + ++counter,
          kind: 'playback',
          trackIds: [args.trackId],
          name: '',
          createdAt: now(),
          expiresAt: new Date(Date.now() + 10800000).toISOString(),
        };
        links.push(link);
        result = { link, url: 'https://preview.wavelength.invalid/audio' };
        break;
      }
      case 'CreateShare': {
        const link = {
          id: 'share-' + ++counter,
          kind: 'share',
          trackIds: args.trackIds,
          name: args.name || 'Shared music',
          createdAt: now(),
          expiresAt: new Date(Date.now() + 2592000000).toISOString(),
        };
        links.push(link);
        result = { link, url: 'https://wavelength.example/preview-share' };
        break;
      }
      case 'ListAccessLinks':
        result = { links: links.filter((l) => l.kind === (args.kind || 'share')) };
        break;
      case 'RevokeAccessLink':
        links = links.filter((l) => l.id !== args.id);
        result = {};
        break;
      case 'StartListeningSession': {
        const session = {
          id: args.requestId,
          track: find(tracks, args.trackId),
          origin: args.origin,
          outcome: '',
          listenedMs: '0',
          positionMs: '0',
          startedAt: now(),
          updatedAt: now(),
        };
        history.unshift(session);
        result = session;
        break;
      }
      case 'ReportListeningProgress':
      case 'FinishListeningSession': {
        const session = find(history, args.sessionId)!;
        Object.assign(session, args, { updatedAt: now() });
        delete session.sessionId;
        if (method === 'FinishListeningSession') session.endedAt = now();
        result = session;
        break;
      }
      case 'ListListeningSessions': {
        const p = page(
          history.filter((h) => !args.outcome || h.outcome === args.outcome),
          args,
        );
        result = { sessions: p.items, total: p.total };
        break;
      }
      default:
        return response(
          { code: 'unimplemented', message: 'This action is not available in the preview.' },
          501,
        );
    }
    // Demo-only timing fields are never part of the protobuf contract.
    return response(
      JSON.parse(JSON.stringify(result, (key, value) => (key === 'started' ? undefined : value))),
    );
  }) as typeof fetch;
}
