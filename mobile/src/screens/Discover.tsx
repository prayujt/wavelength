import React, { useState } from 'react';
import { ScrollView, View } from 'react-native';
import { router, useLocalSearchParams } from 'expo-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import * as Linking from 'expo-linking';
import {
  ArrowDownToLine,
  ArrowUpRight,
  Check,
  ChevronRight,
  MoreHorizontal,
  Play,
  Radio,
  Search,
} from 'lucide-react-native';
import type {
  ExternalTrack,
  ProviderRef,
  SourceCandidate,
} from '../gen/wavelength/v1/ingestion_pb';
import { refKey, releaseAvailability, clock, duration } from '../core/queue';
import { useAPI, useSession } from '../lib/session';
import { usePlayer } from '../lib/player';
import { color, colors } from '../lib/theme';
import { AlbumCard, ArtistCard, LibraryBadge, TrackRow } from '../components/media';
import { Artwork } from '../components/artwork';
import {
  Button,
  Empty,
  Failure,
  Header,
  IconButton,
  Loading,
  Pill,
  Press,
  Screen,
  SearchField,
  Section,
  Txt,
  useDebounced,
} from '../components/ui';
import { useFeedback } from '../components/feedback';

function releaseRoute(ref: ProviderRef) {
  router.push({
    pathname: '/release/[provider]/[id]',
    params: { provider: ref.provider, id: ref.id },
  });
}
function artistRoute(ref: ProviderRef) {
  router.push({
    pathname: '/external-artist/[provider]/[id]',
    params: { provider: ref.provider, id: ref.id },
  });
}
function useImported(refs: (ProviderRef | undefined)[]) {
  const api = useAPI(),
    keys = [
      ...new Map(refs.filter((r): r is ProviderRef => !!r).map((r) => [refKey(r), r])).values(),
    ],
    ids = keys.map(refKey).sort();
  return useQuery({
    queryKey: ['imported', ids],
    queryFn: async () => {
      const imported = new Map<string, string>();
      for (let i = 0; i < keys.length; i += 200) {
        const result = await api.ingestion.lookupImports({ metadataRefs: keys.slice(i, i + 200) });
        for (const track of result.tracks) imported.set(refKey(track.metadataRef), track.trackId);
      }
      return imported;
    },
    enabled: keys.length > 0,
  });
}
function ExternalSong({ track, imported }: { track: ExternalTrack; imported?: string }) {
  const api = useAPI(),
    player = usePlayer(),
    feedback = useFeedback(),
    cache = useQueryClient();
  async function add() {
    if (imported) {
      const result = await api.library.getTrack({ id: imported });
      if (result.track) await player.play([result.track]);
      return;
    }
    await api.ingestion.createImport({
      requestId: Crypto.randomUUID(),
      items: [{ metadataRef: track.ref }],
    });
    await cache.invalidateQueries({ queryKey: ['imports'] });
    feedback.notify('Added to Downloads');
  }
  return (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        paddingHorizontal: 24,
        minHeight: 78,
        gap: 13,
      }}
    >
      <Artwork
        uri={track.artworkUrl}
        title={track.album}
        id={track.album || track.title}
        size={52}
        radius={10}
      />
      <View style={{ flex: 1 }}>
        <Press
          label={imported ? `Play ${track.title}` : `Choose recording for ${track.title}`}
          onPress={
            imported
              ? add
              : () =>
                  router.push({
                    pathname: '/sources',
                    params: {
                      provider: track.ref!.provider,
                      id: track.ref!.id,
                      title: track.title,
                    },
                  })
          }
          style={{ gap: 5, paddingVertical: 10 }}
        >
          <Txt size={14} weight="medium" numberOfLines={1}>
            {track.title}
          </Txt>
          <Txt size={12} muted numberOfLines={1}>
            {track.artist} · {clock(track.durationSeconds)}
          </Txt>
          {imported && (
            <Txt size={10} style={{ color: color.accent }}>
              In your library
            </Txt>
          )}
        </Press>
      </View>
      <IconButton
        icon={imported ? Play : ArrowDownToLine}
        label={imported ? `Play saved ${track.title}` : `Download ${track.title}`}
        onPress={add}
        size={20}
      />
      <IconButton
        icon={MoreHorizontal}
        label={`Choose recording for ${track.title}`}
        size={19}
        onPress={() =>
          router.push({
            pathname: '/sources',
            params: { provider: track.ref!.provider, id: track.ref!.id, title: track.title },
          })
        }
      />
    </View>
  );
}
export function DiscoverScreen() {
  const params = useLocalSearchParams<{ q?: string }>(),
    [input, setInput] = useState({ param: params.q, value: params.q || '' }),
    [local, setLocal] = useState(false),
    api = useAPI(),
    player = usePlayer();
  const search = input.param === params.q ? input.value : params.q || '';
  const setSearch = (value: string) => setInput({ param: params.q, value });
  const query = useDebounced(search.trim());
  const catalog = useQuery({
    queryKey: ['discover', query],
    queryFn: ({ signal }) => api.discovery.search({ query, limit: 20 }, { signal }),
    enabled: !local && query.length >= 2,
    staleTime: 120000,
  });
  const library = useQuery({
    queryKey: ['local-search', query],
    queryFn: async ({ signal }) => {
      const options = { signal };
      const [tracks, albums, artists] = await Promise.all([
        api.library.listTracks({ query, limit: 40 }, options),
        api.library.listAlbums({ query, limit: 12 }, options),
        api.library.listArtists({ query, limit: 12 }, options),
      ]);
      return { tracks: tracks.tracks, albums: albums.albums, artists: artists.artists };
    },
    enabled: local && query.length >= 2,
  });
  const releaseRefs =
    catalog.data?.releases
      .slice(0, 8)
      .map((r) => r.ref)
      .filter((r): r is ProviderRef => !!r) || [];
  const releases = useQuery({
    queryKey: ['release-matches', releaseRefs.map(refKey)],
    queryFn: () => api.discovery.getReleases({ refs: releaseRefs }),
    enabled: !local && releaseRefs.length > 0,
    staleTime: 120000,
  });
  const refs = [
    ...(catalog.data?.tracks.map((t) => t.ref) || []),
    ...(releases.data?.releases.flatMap((r) => r.tracks.map((t) => t.ref)) || []),
  ];
  const imported = useImported(refs),
    saved = imported.data || new Map<string, string>();
  const active = local ? library : catalog;
  return (
    <Screen>
      <Header
        title="Find your next favorite."
        eyebrow="DISCOVER"
        subtitle="A song, an artist, a whole new rabbit hole."
      />
      <SearchField value={search} onChange={setSearch} />
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ paddingHorizontal: 24, paddingTop: 16, gap: 8 }}
      >
        <Pill title="The catalog" active={!local} onPress={() => setLocal(false)} icon={Radio} />
        <Pill title="In your library" active={local} onPress={() => setLocal(true)} icon={Check} />
      </ScrollView>
      {query.length < 2 ? (
        <>
          <Section title="Follow a feeling" eyebrow="START SOMEWHERE">
            <View
              style={{ paddingHorizontal: 24, flexDirection: 'row', flexWrap: 'wrap', gap: 12 }}
            >
              {['Indie folk', 'Jazz', 'Ambient', 'Classical'].map((title, i) => (
                <View style={{ width: '48%' }} key={title}>
                  <Press
                    label={`Search ${title}`}
                    onPress={() => setSearch(title)}
                    style={{
                      backgroundColor: colors(title)[0],
                      minHeight: 122,
                      borderRadius: 18,
                      padding: 18,
                      overflow: 'hidden',
                    }}
                  >
                    <View
                      style={{
                        position: 'absolute',
                        right: -15,
                        bottom: -25,
                        transform: [{ rotate: '20deg' }],
                      }}
                    >
                      <Artwork title={title} id={title} size={100} radius={8} label={false} />
                    </View>
                    <Txt weight="display" size={19} style={{ maxWidth: 100, lineHeight: 25 }}>
                      {title}
                    </Txt>
                    <ArrowUpRight color={color.text} size={18} style={{ marginTop: 19 }} />
                  </Press>
                </View>
              ))}
            </View>
          </Section>
          <Empty
            icon={Search}
            title="Good things are a search away."
            text="Look for music you already love, or take a chance on something new."
          />
        </>
      ) : active.isPending ? (
        <Loading rows={6} />
      ) : active.error ? (
        <Failure error={active.error} retry={active.refetch} />
      ) : local ? (
        <>
          {!!library.data?.tracks.length && (
            <Section title="Songs">
              {library.data.tracks.map((track, i) => (
                <TrackRow
                  key={track.id}
                  track={track}
                  onPlay={() => player.play(library.data!.tracks, i)}
                />
              ))}
            </Section>
          )}
          {!!library.data?.albums.length && (
            <Section title="Albums">
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                contentContainerStyle={{ paddingHorizontal: 24, gap: 15 }}
              >
                {library.data.albums.map((a) => (
                  <AlbumCard key={a.id} album={a} />
                ))}
              </ScrollView>
            </Section>
          )}
          {!!library.data?.artists.length && (
            <Section title="Artists">
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                contentContainerStyle={{ paddingHorizontal: 24, gap: 18 }}
              >
                {library.data.artists.map((a) => (
                  <ArtistCard key={a.id} artist={a} />
                ))}
              </ScrollView>
            </Section>
          )}
          {library.data &&
            !library.data.tracks.length &&
            !library.data.albums.length &&
            !library.data.artists.length && (
              <Empty
                title="Not in your collection yet"
                text="Try the catalog to find and download this music."
                action="Search the catalog"
                onPress={() => setLocal(false)}
              />
            )}
        </>
      ) : (
        <>
          {!!catalog.data?.artists.length && (
            <Section title="Artists">
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                contentContainerStyle={{ paddingHorizontal: 24, gap: 18 }}
              >
                {catalog.data.artists.slice(0, 8).map((artist) => (
                  <Press
                    key={refKey(artist.ref)}
                    label={`Discover ${artist.name}`}
                    onPress={() => artistRoute(artist.ref!)}
                    style={{ width: 118, alignItems: 'center', gap: 12 }}
                  >
                    <Artwork
                      title={artist.name}
                      uri={artist.artworkUrl}
                      id={artist.name}
                      size={112}
                      radius={56}
                      label={false}
                    />
                    <Txt size={12} weight="medium" numberOfLines={1}>
                      {artist.name}
                    </Txt>
                  </Press>
                ))}
              </ScrollView>
            </Section>
          )}
          {!!catalog.data?.releases.length && (
            <Section title="Albums">
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                contentContainerStyle={{ paddingHorizontal: 24, gap: 15 }}
              >
                {catalog.data.releases.slice(0, 12).map((release) => {
                  const full = releases.data?.releases.find(
                    (r) => refKey(r.ref) === refKey(release.ref),
                  );
                  const availability = releaseAvailability(
                    (full?.tracks || []).map((t) => t.ref!).filter(Boolean),
                    saved,
                  );
                  return (
                    <View key={refKey(release.ref)} style={{ width: 155, gap: 7 }}>
                      <Press
                        label={`Discover album ${release.title}`}
                        onPress={() => releaseRoute(release.ref!)}
                        style={{ gap: 10 }}
                      >
                        <Artwork
                          title={release.title}
                          uri={release.artworkUrl}
                          id={release.title}
                          size={155}
                          radius={14}
                        />
                        <Txt weight="bold" size={13} numberOfLines={1}>
                          {release.title}
                        </Txt>
                        <Txt muted size={12} numberOfLines={1}>
                          {release.artist}
                        </Txt>
                      </Press>
                      <LibraryBadge
                        count={availability.available}
                        total={full?.tracks.length || 0}
                        onPress={() => releaseRoute(release.ref!)}
                      />
                    </View>
                  );
                })}
              </ScrollView>
            </Section>
          )}
          {!!catalog.data?.tracks.length && (
            <Section title="Songs" eyebrow="FIND IT. KEEP IT.">
              {catalog.data.tracks.map((track) => (
                <ExternalSong
                  key={refKey(track.ref)}
                  track={track}
                  imported={saved.get(refKey(track.ref))}
                />
              ))}
            </Section>
          )}
          {catalog.data &&
            !catalog.data.artists.length &&
            !catalog.data.releases.length &&
            !catalog.data.tracks.length && (
              <Empty
                icon={Search}
                title="Nothing found this time"
                text="Try an artist, a song title, or a different spelling."
              />
            )}
        </>
      )}
    </Screen>
  );
}

export function ReleaseScreen() {
  const { provider, id } = useLocalSearchParams<{ provider: string; id: string }>(),
    api = useAPI(),
    player = usePlayer(),
    feedback = useFeedback(),
    cache = useQueryClient();
  const data = useQuery({
    queryKey: ['release', provider, id],
    queryFn: () => api.discovery.getRelease({ ref: { provider, id } }),
    staleTime: 120000,
  });
  const imported = useImported(data.data?.release?.tracks.map((t) => t.ref) || []),
    release = data.data?.release,
    saved = imported.data || new Map<string, string>();
  if (data.isPending)
    return (
      <Screen>
        <Loading rows={7} />
      </Screen>
    );
  if (data.error || !release)
    return (
      <Screen>
        <Failure
          error={data.error || new Error('This release is not available right now.')}
          retry={data.refetch}
        />
      </Screen>
    );
  const availability = releaseAvailability(
    release.tracks.map((t) => t.ref!).filter(Boolean),
    saved,
  );
  async function download() {
    await api.ingestion.createImport({
      requestId: Crypto.randomUUID(),
      releaseRef: { provider, id },
    });
    void cache.invalidateQueries({ queryKey: ['imports'] });
    feedback.notify('Album added to Downloads');
    router.push('/activity');
  }
  async function playSaved() {
    const ids = availability.ids.filter((id): id is string => !!id);
    const tracks = await Promise.all(
      ids.map((id) => api.library.getTrack({ id }).then((r) => r.track!)),
    );
    await player.play(tracks);
  }
  return (
    <Screen>
      <Header title="" back />
      <View style={{ alignItems: 'center', paddingBottom: 28 }}>
        <Artwork
          title={release.title}
          id={release.title}
          uri={release.artworkUrl}
          size={232}
          radius={16}
        />
      </View>
      <View style={{ paddingHorizontal: 24, gap: 10 }}>
        <Txt size={10} weight="bold" style={{ color: color.accent, letterSpacing: 2 }}>
          FROM THE CATALOG
        </Txt>
        <Txt size={32} weight="heavy" style={{ letterSpacing: -1.2, lineHeight: 40 }}>
          {release.title}
        </Txt>
        <Press
          label={`Discover ${release.artist}`}
          onPress={() => release.artistRef && artistRoute(release.artistRef)}
          style={{ paddingVertical: 5 }}
        >
          <Txt muted size={15}>
            {release.artist} {release.artistRef ? '→' : ''}
          </Txt>
        </Press>
        <Txt muted size={12}>
          {release.tracks.length} songs ·{' '}
          {duration(release.tracks.reduce((s, t) => s + t.durationSeconds, 0))}
        </Txt>
        {availability.available > 0 && (
          <Txt size={12} style={{ color: color.accent }}>
            {availability.complete
              ? 'Every song is already in your library.'
              : `${availability.available} of ${release.tracks.length} songs already saved`}
          </Txt>
        )}
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 10, marginVertical: 16 }}>
          {availability.complete ? (
            <Button title="Play album" icon={Play} onPress={playSaved} />
          ) : (
            <Button
              title={availability.available ? 'Download missing songs' : 'Download album'}
              icon={ArrowDownToLine}
              onPress={download}
              disabled={imported.isPending || !release.tracks.length}
            />
          )}
          {availability.available > 0 && !availability.complete && (
            <Button title="Play saved" secondary small onPress={playSaved} />
          )}
        </View>
      </View>
      {release.tracks.map((track) => (
        <ExternalSong
          key={refKey(track.ref)}
          track={track}
          imported={saved.get(refKey(track.ref))}
        />
      ))}
    </Screen>
  );
}

export function ExternalArtistScreen() {
  const { provider, id } = useLocalSearchParams<{ provider: string; id: string }>(),
    api = useAPI();
  const data = useQuery({
    queryKey: ['external-artist', provider, id],
    queryFn: () => api.discovery.getArtist({ ref: { provider, id } }),
    staleTime: 120000,
  });
  const artist = data.data?.artist;
  return (
    <Screen>
      {data.isPending ? (
        <Loading rows={7} />
      ) : data.error || !artist ? (
        <Failure
          error={data.error || new Error('This artist is unavailable.')}
          retry={data.refetch}
        />
      ) : (
        <>
          <Header
            back
            title={artist.name}
            eyebrow="DISCOVER ARTIST"
            subtitle={`${artist.releases.length} catalog releases`}
          />
          <View style={{ alignItems: 'center', marginVertical: 20 }}>
            <Artwork
              title={artist.name}
              id={artist.name}
              uri={artist.artworkUrl}
              size={200}
              radius={100}
              label={false}
            />
          </View>
          <Section title="Explore the discography">
            {artist.releases.map((release) => (
              <Press
                key={refKey(release.ref)}
                label={`Open ${release.title}`}
                onPress={() => releaseRoute(release.ref!)}
                style={{
                  paddingHorizontal: 24,
                  paddingVertical: 12,
                  gap: 15,
                  flexDirection: 'row',
                  alignItems: 'center',
                }}
              >
                <Artwork
                  title={release.title}
                  id={release.title}
                  uri={release.artworkUrl}
                  size={66}
                  radius={10}
                />
                <View style={{ flex: 1, gap: 5 }}>
                  <Txt numberOfLines={2} weight="medium">
                    {release.title}
                  </Txt>
                  <Txt muted size={12}>
                    {release.artist}
                  </Txt>
                </View>
                <ChevronRight size={19} color={color.faint} />
              </Press>
            ))}
          </Section>
        </>
      )}
    </Screen>
  );
}

export function SourcesScreen() {
  const params = useLocalSearchParams<{
      provider: string;
      id: string;
      title?: string;
      job?: string;
      index?: string;
      replace?: string;
    }>(),
    api = useAPI(),
    session = useSession(),
    feedback = useFeedback(),
    cache = useQueryClient();
  const [selected, setSelected] = useState<SourceCandidate | null>(null);
  const data = useQuery({
    queryKey: ['sources', params.provider, params.id],
    queryFn: ({ signal }) =>
      api.discovery.findSources(
        { metadataRef: { provider: params.provider, id: params.id } },
        { signal },
      ),
    staleTime: 300000,
    retry: false,
  });
  async function submit() {
    if (!selected?.ref) return;
    if (
      params.replace &&
      !(await feedback.confirm(
        'Replace this recording?',
        'The song stays in your playlists. Only its audio is replaced with the recording you selected.',
        'Replace audio',
      ))
    )
      return;
    if (params.job)
      await api.ingestion.retryImport({
        id: params.job,
        overrides: [{ index: Number(params.index || 0), sourceRef: selected.ref }],
      });
    else
      await api.ingestion.createImport({
        requestId: Crypto.randomUUID(),
        items: [
          {
            metadataRef: { provider: params.provider, id: params.id },
            sourceRef: selected.ref,
            replaceTrackId: params.replace || '',
          },
        ],
      });
    void cache.invalidateQueries({ queryKey: ['imports'] });
    feedback.notify(params.replace ? 'Replacement queued' : 'Recording queued for download');
    router.replace('/activity');
  }
  return (
    <Screen>
      <Header
        back
        title="The right recording."
        eyebrow="A LITTLE MORE CONTROL"
        subtitle={params.title || 'Choose the version that belongs in your collection.'}
      />
      <View
        style={{
          marginHorizontal: 24,
          padding: 17,
          borderRadius: 16,
          backgroundColor: color.surface,
          marginBottom: 20,
          gap: 6,
        }}
      >
        <Txt size={13} weight="medium">
          Preview first. Choose with confidence.
        </Txt>
        <Txt muted size={12} style={{ lineHeight: 20 }}>
          Scores rank metadata matches. They don’t verify the audio. Live versions, covers, and
          remixes can sound very different.
        </Txt>
      </View>
      {data.isPending ? (
        <Loading rows={5} />
      ) : data.error ? (
        <Failure error={data.error} retry={data.refetch} />
      ) : !data.data.candidates.length ? (
        <Empty title="No recordings found" text="Try another release, or come back in a moment." />
      ) : (
        data.data.candidates.map((candidate) => (
          <View
            key={refKey(candidate.ref)}
            style={{
              marginHorizontal: 24,
              borderRadius: 18,
              borderWidth: 1,
              borderColor: selected?.ref?.id === candidate.ref?.id ? color.accent : color.line,
              backgroundColor: selected?.ref?.id === candidate.ref?.id ? '#1B3024' : color.surface,
              marginBottom: 12,
              overflow: 'hidden',
            }}
          >
            <Press
              label={`Select ${candidate.title}`}
              selected={selected?.ref?.id === candidate.ref?.id}
              onPress={() => setSelected(candidate)}
              style={{ padding: 18, gap: 12 }}
            >
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
                <View style={{ flex: 1, gap: 6 }}>
                  <Txt weight="bold" size={14} style={{ lineHeight: 21 }}>
                    {candidate.title}
                  </Txt>
                  <Txt muted size={12}>
                    {candidate.uploader} · {clock(candidate.durationSeconds)}
                  </Txt>
                </View>
                <View
                  style={{
                    width: 22,
                    height: 22,
                    borderRadius: 11,
                    borderColor:
                      selected?.ref?.id === candidate.ref?.id ? color.accent : color.faint,
                    borderWidth: 1,
                    backgroundColor:
                      selected?.ref?.id === candidate.ref?.id ? color.accent : 'transparent',
                    alignItems: 'center',
                    justifyContent: 'center',
                  }}
                >
                  {selected?.ref?.id === candidate.ref?.id && (
                    <Check size={15} color={color.accentInk} />
                  )}
                </View>
              </View>
              <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center' }}>
                <View
                  style={{
                    paddingHorizontal: 8,
                    paddingVertical: 4,
                    backgroundColor: color.raised,
                    borderRadius: 6,
                  }}
                >
                  <Txt
                    size={10}
                    weight="bold"
                    style={{ color: candidate.suitable ? color.accent : color.gold }}
                  >
                    MATCH {Math.round(candidate.score * 100)}/100
                  </Txt>
                </View>
                <Txt size={10} muted>
                  {candidate.suitable ? 'Strong candidate' : 'Review carefully'}
                </Txt>
              </View>
              <Txt muted size={12} style={{ lineHeight: 19 }}>
                {candidate.reason}
              </Txt>
            </Press>
            <Press
              label={`Preview ${candidate.title}`}
              onPress={async () => {
                if (session.preview) {
                  feedback.notify('Recording previews open YouTube on a live connection.');
                  return;
                }
                await Linking.openURL(
                  'https://www.youtube.com/watch?v=' + encodeURIComponent(candidate.ref!.id),
                );
              }}
              style={{
                paddingHorizontal: 18,
                paddingVertical: 13,
                borderTopWidth: 1,
                borderTopColor: color.line,
                flexDirection: 'row',
                alignItems: 'center',
                gap: 7,
              }}
            >
              <ArrowUpRight size={15} color={color.accent} />
              <Txt size={12} style={{ color: color.accent }}>
                Preview recording
              </Txt>
            </Press>
          </View>
        ))
      )}
      <View style={{ padding: 24 }}>
        <Button
          title={params.replace ? 'Replace with selected recording' : 'Download selected recording'}
          icon={ArrowDownToLine}
          onPress={submit}
          disabled={!selected}
        />
      </View>
    </Screen>
  );
}
