import React, { useState } from 'react';
import { FlatList, ScrollView, View } from 'react-native';
import { router } from 'expo-router';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import {
  ArrowDownToLine,
  Check,
  ChevronDown,
  ChevronUp,
  Clock3,
  Headphones,
  RefreshCw,
  X,
} from 'lucide-react-native';
import type { ImportJob } from '../gen/wavelength/v1/ingestion_pb';
import { useAPI, useSession } from '../lib/session';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { duration } from '../core/queue';
import { Artwork } from '../components/artwork';
import { TrackRow } from '../components/media';
import {
  Button,
  Empty,
  Failure,
  Header,
  Loading,
  Pill,
  Press,
  Screen,
  Txt,
} from '../components/ui';
import { useFeedback } from '../components/feedback';

const stateLabel: Record<string, string> = {
  queued: 'Queued',
  running: 'In progress',
  completed: 'In your library',
  failed: 'Needs a retry',
  partial: 'Partly complete',
  needs_selection: 'Choose a recording',
  cancelled: 'Cancelled',
};
function JobCard({ job }: { job: ImportJob }) {
  const [expanded, setExpanded] = useState(
      job.state === 'needs_selection' || job.state === 'failed',
    ),
    api = useAPI(),
    cache = useQueryClient(),
    feedback = useFeedback();
  const first = job.items[0]?.metadata,
    done = job.items.filter((item) => item.state === 'completed').length,
    active = ['queued', 'running'].includes(job.state);
  const tint =
    job.state === 'failed'
      ? color.danger
      : job.state === 'needs_selection' || job.state === 'partial'
        ? color.gold
        : job.state === 'completed'
          ? color.accent
          : color.muted;
  async function cancel() {
    await api.ingestion.cancelImport({ id: job.id });
    void cache.invalidateQueries({ queryKey: ['imports'] });
    feedback.notify('Download cancelled');
  }
  async function retry() {
    await api.ingestion.retryImport({ id: job.id });
    void cache.invalidateQueries({ queryKey: ['imports'] });
    feedback.notify('Download queued again');
  }
  return (
    <View
      style={{
        marginHorizontal: 24,
        marginBottom: 14,
        padding: 17,
        borderRadius: 20,
        backgroundColor: color.surface,
        borderWidth: 1,
        borderColor: color.line,
      }}
    >
      <Press
        label={`${expanded ? 'Collapse' : 'Expand'} ${first?.title || 'download'}`}
        onPress={() => setExpanded(!expanded)}
        style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}
      >
        <Artwork
          title={first?.album || 'Download'}
          id={first?.album || job.id}
          uri={first?.artworkUrl}
          size={56}
          radius={10}
        />
        <View style={{ flex: 1, gap: 6 }}>
          <Txt weight="bold" size={14} numberOfLines={2}>
            {job.items.length > 1
              ? first?.album || 'Album download'
              : first?.title || 'Preparing your music'}
          </Txt>
          <Txt muted size={12} numberOfLines={1}>
            {first?.artist || 'Wavelength'}
          </Txt>
        </View>
        {expanded ? (
          <ChevronUp size={18} color={color.muted} />
        ) : (
          <ChevronDown size={18} color={color.muted} />
        )}
      </Press>
      <View
        style={{
          flexDirection: 'row',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginTop: 16,
        }}
      >
        <View style={{ flexDirection: 'row', gap: 6, alignItems: 'center' }}>
          {job.state === 'completed' && <Check size={13} color={tint} />}
          <Txt size={11} weight="medium" style={{ color: tint }}>
            {stateLabel[job.state] || job.state}
          </Txt>
        </View>
        <Txt size={10} muted>
          {done} of {job.items.length} ready
        </Txt>
      </View>
      <View
        style={{
          height: 3,
          borderRadius: 2,
          backgroundColor: color.line,
          marginTop: 11,
          overflow: 'hidden',
        }}
      >
        <View
          style={{
            height: 3,
            width: `${job.items.length ? (done / job.items.length) * 100 : 0}%`,
            backgroundColor: tint,
          }}
        />
      </View>
      {expanded && (
        <View style={{ marginTop: 14 }}>
          {job.error && (
            <Txt size={12} style={{ color: color.danger, lineHeight: 19, marginBottom: 10 }}>
              {job.error}
            </Txt>
          )}
          {job.items.map((item, index) => (
            <View
              key={`${item.metadataRef?.id}:${index}`}
              style={{
                paddingVertical: 11,
                borderTopWidth: index ? 1 : 0,
                borderTopColor: color.line,
                gap: 8,
              }}
            >
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
                <Txt muted size={11}>
                  {String(index + 1).padStart(2, '0')}
                </Txt>
                <View style={{ flex: 1, gap: 5 }}>
                  <Txt size={12} numberOfLines={1}>
                    {item.metadata?.title || 'Resolving metadata'}
                  </Txt>
                  <Txt muted size={10}>
                    {item.state === 'completed'
                      ? item.reused
                        ? 'Already in your library'
                        : 'Ready to play'
                      : item.state.replaceAll('_', ' ')}
                  </Txt>
                </View>
                {item.state === 'completed' && <Check size={15} color={color.accent} />}
              </View>
              {item.error && (
                <Txt size={11} style={{ color: color.danger, lineHeight: 18 }}>
                  {item.error}
                </Txt>
              )}
              {item.warnings.map((warning, i) => (
                <Txt key={i} muted size={11} style={{ lineHeight: 18 }}>
                  {warning}
                </Txt>
              ))}
              {item.state === 'needs_selection' && (
                <View style={{ alignItems: 'flex-start', marginLeft: 22 }}>
                  <Button
                    title="Choose a recording"
                    small
                    secondary
                    onPress={() =>
                      router.push({
                        pathname: '/sources',
                        params: {
                          provider: item.metadataRef!.provider,
                          id: item.metadataRef!.id,
                          title: item.metadata?.title,
                          job: job.id,
                          index: String(index),
                        },
                      })
                    }
                  />
                </View>
              )}
            </View>
          ))}
        </View>
      )}
      {active && (
        <View style={{ alignItems: 'flex-start', marginTop: 10 }}>
          <Button title="Cancel download" icon={X} small secondary onPress={cancel} />
        </View>
      )}
      {['failed', 'partial', 'cancelled'].includes(job.state) && (
        <View style={{ alignItems: 'flex-start', marginTop: 12 }}>
          <Button title="Retry unfinished" icon={RefreshCw} small secondary onPress={retry} />
        </View>
      )}
    </View>
  );
}

export function ActivityScreen() {
  const { online } = useSession();
  const [tab, setTab] = useState<'Downloads' | 'Listening'>('Downloads'),
    [outcome, setOutcome] = useState(''),
    api = useAPI(),
    player = usePlayer();
  const downloads = useInfiniteQuery({
    queryKey: ['imports', 'activity'],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => api.ingestion.listImports({ limit: 50, offset: pageParam }),
    getNextPageParam: (last, pages) => {
      const offset = pages.reduce((n, p) => n + p.imports.length, 0);
      return offset < last.total && last.imports.length ? offset : undefined;
    },
    enabled: online,
    refetchInterval: (query) =>
      !online
        ? false
        : query.state.data?.pages.some((p) =>
              p.imports.some((j) => ['queued', 'running'].includes(j.state)),
            )
          ? 3000
          : 15000,
  });
  const listening = useInfiniteQuery({
    queryKey: ['listening', outcome],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      api.listening.listListeningSessions({ limit: 40, offset: pageParam, outcome }),
    getNextPageParam: (last, pages) => {
      const offset = pages.reduce((n, p) => n + p.sessions.length, 0);
      return offset < last.total && last.sessions.length ? offset : undefined;
    },
    enabled: tab === 'Listening',
    refetchInterval: online ? 15000 : false,
  });
  const jobs = downloads.data?.pages.flatMap((p) => p.imports) || [],
    sessions = listening.data?.pages.flatMap((p) => p.sessions) || [];
  const active = jobs.filter((j) => ['queued', 'running'].includes(j.state)).length;
  const top = (
    <View>
      <Header
        title="The music keeps moving."
        eyebrow="YOUR ACTIVITY"
        subtitle="Coming in. Played back. All right here."
      />
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ paddingHorizontal: 24, gap: 8, marginBottom: 22 }}
      >
        <Pill
          title={active ? `Downloads · ${active}` : 'Downloads'}
          active={tab === 'Downloads'}
          icon={ArrowDownToLine}
          onPress={() => setTab('Downloads')}
        />
        <Pill
          title="Listening"
          active={tab === 'Listening'}
          icon={Headphones}
          onPress={() => setTab('Listening')}
        />
      </ScrollView>
      {tab === 'Downloads' ? (
        <View style={{ paddingHorizontal: 24, marginBottom: 20, gap: 6 }}>
          <Txt size={21} weight="display">
            {active ? 'Making room for more.' : 'Your latest additions'}
          </Txt>
          <Txt muted size={12} style={{ lineHeight: 20 }}>
            {active
              ? `${active} ${active === 1 ? 'download is' : 'downloads are'} running. Your library updates automatically.`
              : 'Downloads go straight into your shared collection.'}
          </Txt>
        </View>
      ) : (
        <>
          <View style={{ paddingHorizontal: 24, marginBottom: 18 }}>
            <Txt size={22} weight="display">
              A little time, well spent.
            </Txt>
            <Txt muted size={12} style={{ marginTop: 7 }}>
              Your listening history, with every session in order.
            </Txt>
          </View>
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ paddingHorizontal: 24, gap: 8, marginBottom: 16 }}
          >
            {[
              ['All sessions', ''],
              ['Completed', 'completed'],
              ['Skipped', 'skipped'],
              ['Stopped', 'stopped'],
            ].map(([title, value]) => (
              <Pill
                title={title}
                key={value}
                active={outcome === value}
                onPress={() => setOutcome(value)}
              />
            ))}
          </ScrollView>
        </>
      )}
    </View>
  );
  return (
    <Screen scroll={false}>
      {tab === 'Downloads' ? (
        <FlatList
          data={jobs}
          keyExtractor={(j) => j.id}
          ListHeaderComponent={top}
          renderItem={({ item }) => <JobCard job={item} />}
          contentContainerStyle={{ paddingBottom: 170 }}
          showsVerticalScrollIndicator={false}
          ListEmptyComponent={
            downloads.isPending ? (
              <Loading />
            ) : downloads.error ? (
              <Failure error={downloads.error} retry={downloads.refetch} />
            ) : (
              <Empty
                icon={ArrowDownToLine}
                title="Something new sounds good."
                text="Songs and albums you download will appear here. Find a favorite in Discover."
                action="Explore the catalog"
                onPress={() => router.push('/search')}
              />
            )
          }
          onEndReached={() => {
            if (downloads.hasNextPage && !downloads.isFetchingNextPage)
              void downloads.fetchNextPage();
          }}
          onEndReachedThreshold={0.4}
          refreshing={downloads.isRefetching && !downloads.isFetchingNextPage}
          onRefresh={() => {
            void downloads.refetch();
          }}
        />
      ) : (
        <FlatList
          data={sessions}
          keyExtractor={(s) => s.id}
          ListHeaderComponent={top}
          contentContainerStyle={{ paddingBottom: 170 }}
          renderItem={({ item, index }) => {
            const date = item.startedAt
              ? new Date(Number(item.startedAt.seconds) * 1000).toLocaleDateString(undefined, {
                  month: 'long',
                  day: 'numeric',
                })
              : '';
            const previous = sessions[index - 1]?.startedAt,
              previousDate = previous
                ? new Date(Number(previous.seconds) * 1000).toLocaleDateString(undefined, {
                    month: 'long',
                    day: 'numeric',
                  })
                : '';
            return (
              <View>
                {date !== previousDate && (
                  <Txt
                    muted
                    size={10}
                    weight="bold"
                    style={{
                      marginHorizontal: 24,
                      marginTop: 24,
                      marginBottom: 12,
                      letterSpacing: 1,
                    }}
                  >
                    {date.toUpperCase()}
                  </Txt>
                )}
                {item.track && (
                  <TrackRow
                    track={item.track}
                    subtitle={`${item.track.artist} · ${item.outcome || 'In progress'} · ${duration(Number(item.listenedMs) / 1000)} listened`}
                    onPlay={async () => {
                      const result = await api.library.getTrack({ id: item.track!.id });
                      if (result.track) await player.play([result.track]);
                    }}
                  />
                )}
              </View>
            );
          }}
          ListEmptyComponent={
            listening.isPending ? (
              <Loading />
            ) : listening.error ? (
              <Failure error={listening.error} retry={listening.refetch} />
            ) : (
              <Empty
                icon={Clock3}
                title="Your next song starts the story."
                text="Listening sessions appear here after you press play. Pauses and buffering don’t count as listening time."
              />
            )
          }
          onEndReached={() => {
            if (listening.hasNextPage && !listening.isFetchingNextPage)
              void listening.fetchNextPage();
          }}
          refreshing={listening.isRefetching && !listening.isFetchingNextPage}
          onRefresh={() => {
            void listening.refetch();
          }}
        />
      )}
    </Screen>
  );
}
