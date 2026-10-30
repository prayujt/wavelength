import { create } from '@bufbuild/protobuf';
import { describe, expect, it, vi, afterEach } from 'vitest';
import type { FeedPage } from '../../src/core/play-feed';
import type { Track } from '../../src/gen/wavelength/v1/library_pb';
import { TrackSchema } from '../../src/gen/wavelength/v1/library_pb';
import {
  PlaybackController,
  type AudioPort,
  type PlaybackPort,
  type AudioSource,
} from '../../src/core/playback';
const song = (id: string) =>
  create(TrackSchema, { id, title: id, artist: 'Artist', durationSeconds: 200 });
function setup() {
  let position = 0;
  const sources: (AudioSource | null)[] = [],
    released: string[] = [],
    recorded: string[] = [];
  const audio: AudioPort = {
    pause: vi.fn(),
    replace: (source) => {
      sources.push(source);
      position = 0;
    },
    play: vi.fn(),
    async seek(value) {
      position = value;
    },
    position: () => position,
    duration: () => 200,
    volume: vi.fn(),
    metadata: vi.fn(),
    clearMetadata: vi.fn(),
  };
  const port: PlaybackPort = {
    async source(id) {
      return {
        source: { uri: 'https://music.example/temporary/' + id },
        release: async () => {
          released.push(id);
        },
      };
    },
    async track(id) {
      return id === 'missing' ? undefined : song(id);
    },
    async queue() {
      return undefined;
    },
    saveQueue: vi.fn(async () => {}),
    async record(id) {
      recorded.push(id);
    },
    listening: {
      startListeningSession: vi.fn(async () => {}),
      reportListeningProgress: vi.fn(async () => {}),
      finishListeningSession: vi.fn(async () => {}),
    },
  };
  const controller = new PlaybackController(
    audio,
    () => 'listening-test-id',
    () => 'Playback failed',
  );
  return {
    controller,
    port,
    audio,
    sources,
    released,
    recorded,
    position: (value: number) => {
      position = value;
    },
  };
}
const controllers: PlaybackController[] = [];
function connected() {
  const f = setup();
  f.controller.connect(f.port);
  controllers.push(f.controller);
  return f;
}
afterEach(() => {
  for (const c of controllers.splice(0)) c.disconnect();
});
describe('native playback orchestration', () => {
  it('starts a large playlist before remaining metadata loads without losing order or duplicates', async () => {
    const f = connected();
    await f.controller.play([
      song('a'),
      create(TrackSchema, { id: 'b' }),
      create(TrackSchema, { id: 'a' }),
    ]);
    expect(f.sources.at(-1)).toEqual({ uri: 'https://music.example/temporary/a' });
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['a', 'b', 'a']);
    f.controller.hydrate([song('a')]);
    expect(f.controller.getSnapshot().queue[2].title).toBe('a');
    await f.controller.next();
    expect(f.controller.getSnapshot().track?.title).toBe('b');
    expect(f.sources.at(-1)).toEqual({ uri: 'https://music.example/temporary/b' });
    expect(f.audio.metadata).toHaveBeenLastCalledWith(song('b'));
    f.controller.hydrate([song('b'), song('c')]);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['a', 'b', 'a']);
  });
  it('restores ordered saved IDs, skips deleted tracks, and never autoplays on launch', async () => {
    const f = setup();
    f.port.queue = async () => ({
      trackIds: ['a', 'missing', 'b', 'a'],
      currentTrackId: 'b',
      positionMs: 42000n,
    });
    f.controller.connect(f.port);
    controllers.push(f.controller);
    await vi.waitFor(() =>
      expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['a', 'b', 'a']),
    );
    expect(f.controller.getSnapshot().index).toBe(1);
    expect(f.audio.play).not.toHaveBeenCalled();
    await f.controller.toggle();
    f.controller.status({
      isLoaded: true,
      currentTime: 0,
      duration: 200,
      playing: true,
      isBuffering: false,
      didJustFinish: false,
    });
    expect(f.audio.position()).toBe(42);
  });
  it('ignores a delayed restore after the listener starts another album', async () => {
    const f = setup();
    let finish!: (value: Awaited<ReturnType<PlaybackPort['queue']>>) => void;
    f.port.queue = () =>
      new Promise((resolve) => {
        finish = resolve;
      });
    f.controller.connect(f.port);
    controllers.push(f.controller);
    await f.controller.play([song('new')]);
    finish({ trackIds: ['old'], currentTrackId: 'old', positionMs: 0n });
    await Promise.resolve();
    await Promise.resolve();
    expect(f.controller.getSnapshot().track?.id).toBe('new');
  });
  it('discards stale source requests and revokes their temporary grants', async () => {
    const f = connected();
    let resolve!: (value: Awaited<ReturnType<PlaybackPort['source']>>) => void;
    f.port.source = (id) =>
      id === 'slow'
        ? new Promise((r) => {
            resolve = r;
          })
        : Promise.resolve({
            source: { uri: 'https://music.example/fast' },
            release: async () => {},
          });
    const old = f.controller.play([song('slow')]);
    await f.controller.play([song('fast')]);
    resolve({
      source: { uri: 'https://music.example/slow' },
      release: async () => {
        f.released.push('slow');
      },
    });
    await old;
    expect(f.controller.getSnapshot().track?.id).toBe('fast');
    expect(f.sources.filter(Boolean)).toEqual([{ uri: 'https://music.example/fast' }]);
    expect(f.released).toContain('slow');
  });
  it('advances naturally and keeps the current song selected while reordering its queue', async () => {
    const f = connected();
    await f.controller.play([song('a'), song('b'), song('c')], 1);
    f.controller.move(1, 0);
    expect(f.controller.getSnapshot().track?.id).toBe('b');
    expect(f.controller.getSnapshot().index).toBe(0);
    f.controller.remove(0);
    expect(f.controller.getSnapshot().queue).toHaveLength(3);
    f.controller.status({
      isLoaded: true,
      currentTime: 200,
      duration: 200,
      playing: false,
      isBuffering: false,
      didJustFinish: true,
    });
    await vi.waitFor(() => expect(f.controller.getSnapshot().track?.id).toBe('a'));
    expect(f.released).toContain('b');
  });
  it('preserves duplicate entries and inserts play-next at the current position', async () => {
    const f = connected();
    await f.controller.play([song('a'), song('b'), song('a')], 1);
    f.controller.add([song('next')], true);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['a', 'b', 'next', 'a']);
    f.controller.remove(0);
    expect(f.controller.getSnapshot().track?.id).toBe('b');
    expect(f.controller.getSnapshot().index).toBe(0);
  });
  it('shuffle keeps the current track, and disabling it restores original order', async () => {
    const f = connected();
    await f.controller.play([song('a'), song('b'), song('c')], 1);
    f.controller.toggleShuffle();
    expect(f.controller.getSnapshot().track?.id).toBe('b');
    expect(f.controller.getSnapshot().queue[0].id).toBe('b');
    f.controller.toggleShuffle();
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['a', 'b', 'c']);
    expect(f.controller.getSnapshot().index).toBe(1);
  });
  it('restarts the song when previous is pressed after three seconds', async () => {
    const f = connected();
    await f.controller.play([song('a'), song('b')], 1);
    f.position(12);
    await f.controller.previous();
    expect(f.audio.position()).toBe(0);
    expect(f.controller.getSnapshot().track?.id).toBe('b');
    await f.controller.previous();
    expect(f.controller.getSnapshot().track?.id).toBe('a');
  });
  it('clear stops playback, revokes its grant, and saves an empty queue', async () => {
    const f = connected();
    await f.controller.play([song('a')]);
    await f.controller.clear();
    expect(f.controller.getSnapshot().queue).toHaveLength(0);
    expect(f.released).toContain('a');
    expect(f.port.saveQueue).toHaveBeenLastCalledWith([], '', 0, {
      recommendations: [],
      playFeedEnabled: false,
    });
  });
  it('does not resume a pending source after clear or sign-out', async () => {
    const f = connected();
    let resolve!: (value: Awaited<ReturnType<PlaybackPort['source']>>) => void;
    f.port.source = () =>
      new Promise((r) => {
        resolve = r;
      });
    const loading = f.controller.play([song('slow')]);
    await f.controller.clear();
    resolve({
      source: { uri: 'https://music.example/slow' },
      release: async () => {
        f.released.push('slow');
      },
    });
    await loading;
    expect(f.audio.play).not.toHaveBeenCalled();
    expect(f.controller.getSnapshot().queue).toHaveLength(0);
    expect(f.released).toContain('slow');
  });
  it('clamps seek/volume and exposes a retryable error instead of a secret media URL', async () => {
    const f = connected();
    f.port.source = async () => {
      throw new Error('https://private.example/secret');
    };
    await f.controller.play([song('a')]);
    expect(f.controller.getSnapshot().error).toBe('Playback failed');
    await f.controller.seek(-4);
    expect(f.audio.position()).toBe(0);
    f.controller.setVolume(5);
    expect(f.audio.volume).toHaveBeenCalledWith(1);
  });
});

describe('play feed and playback integration', () => {
  afterEach(() => vi.useRealTimers());
  it('starts playback from an empty queue without a manually selected song', async () => {
    const f = connected();
    f.controller.restoreSettings({ repeat: 'all', shuffle: true });
    f.port.getPlayFeed = vi.fn(async () => ({
      items: ['first', 'second', 'third'].map((id) => ({ id: 'item-' + id, track: song(id) })),
      nextCursor: 'continue',
      exhausted: false,
    }));
    await f.controller.startFeed();
    expect(f.controller.getSnapshot().track?.id).toBe('first');
    expect(f.controller.getSnapshot().queue).toHaveLength(3);
    expect(f.controller.getSnapshot().startingFeed).toBe(false);
    expect(f.controller.getSnapshot().repeat).toBe('off');
    expect(f.controller.getSnapshot().shuffle).toBe(false);
    expect(f.audio.play).toHaveBeenCalledTimes(1);
    expect(f.controller.recommendation(f.controller.getSnapshot().track!)).toBe('item-first');
    expect(f.port.getPlayFeed).toHaveBeenCalledWith(
      expect.objectContaining({ seedTrackId: '', queuedTrackIds: [], pageSize: 3 }),
      expect.any(AbortSignal),
    );
  });
  it('keeps a mode active across direct selections, clears the queue, and flushes feedback before reseeding', async () => {
    vi.useFakeTimers();
    const f = connected();
    const events: string[] = [];
    f.port.listening.finishListeningSession = vi.fn(async (input) => {
      events.push('finish:' + input.changeReason);
    });
    f.port.getPlayFeed = vi.fn(async (input) => {
      events.push('feed:' + input.seedTrackId);
      const id = input.seedTrackId === 'current' ? 'old-auto' : 'new-auto';
      return {
        items: [{ id: 'item-' + id, track: song(id) }],
        nextCursor: 'cursor',
        exhausted: true,
      };
    });
    await f.controller.play([song('current'), song('old-manual')]);
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(false);
    f.position(42);
    f.controller.status({
      isLoaded: true,
      currentTime: 42,
      duration: 200,
      playing: true,
      isBuffering: false,
      didJustFinish: false,
    });
    const source = f.sources.at(-1);
    await f.controller.startFeed();
    expect(f.sources.at(-1)).toBe(source);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['current']);
    await vi.advanceTimersByTimeAsync(800);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['current', 'old-auto']);
    await f.controller.play([song('unselected'), song('chosen'), song('also-unselected')], 1);
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(true);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['chosen']);
    await vi.advanceTimersByTimeAsync(800);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['chosen', 'new-auto']);
    expect(f.port.getPlayFeed).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: '', seedTrackId: 'chosen', queuedTrackIds: ['chosen'] }),
      expect.any(AbortSignal),
    );
    expect(events.indexOf('finish:selection')).toBeLessThan(events.lastIndexOf('feed:chosen'));
    const selectedSource = f.sources.at(-1);
    f.controller.stopFeed();
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(false);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['chosen']);
    expect(f.sources.at(-1)).toBe(selectedSource);
    await f.controller.save();
    expect(f.port.saveQueue).toHaveBeenLastCalledWith(
      ['chosen'],
      'chosen',
      expect.anything(),
      expect.objectContaining({ playFeedEnabled: false }),
    );
  });
  it('turning the mode off discards an in-flight startup without playing a late reply', async () => {
    const f = connected();
    let reply!: (page: FeedPage<Track>) => void;
    f.port.getPlayFeed = vi.fn(
      () =>
        new Promise<FeedPage<Track>>((resolve) => {
          reply = resolve;
        }),
    );
    const pending = f.controller.startFeed();
    await vi.waitFor(() => expect(f.port.getPlayFeed).toHaveBeenCalledTimes(1));
    f.controller.stopFeed();
    reply({ items: [{ id: 'late-item', track: song('late') }], nextCursor: '', exhausted: true });
    await pending;
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(false);
    expect(f.controller.getSnapshot().queue).toEqual([]);
    expect(f.audio.play).not.toHaveBeenCalled();
    expect(f.controller.getSnapshot().startingFeed).toBe(false);
  });
  it('choosing a song during startup keeps mode on and discards the late first batch', async () => {
    const f = connected();
    let reply!: (page: FeedPage<Track>) => void;
    f.port.getPlayFeed = vi.fn(
      () =>
        new Promise<FeedPage<Track>>((resolve) => {
          reply = resolve;
        }),
    );
    const pending = f.controller.startFeed();
    await vi.waitFor(() => expect(f.port.getPlayFeed).toHaveBeenCalledTimes(1));
    await f.controller.play([song('unselected'), song('chosen')], 1);
    expect(f.controller.getSnapshot().startingFeed).toBe(false);
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(true);
    reply({ items: [{ id: 'late-item', track: song('late') }], nextCursor: '', exhausted: true });
    await pending;
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['chosen']);
    expect(f.audio.play).toHaveBeenCalledTimes(1);
  });
  it('rejects stale last-song refills after a new selection and clears its waiting state', async () => {
    vi.useFakeTimers();
    const f = connected();
    let reply!: (value: FeedPage<Track>) => void;
    f.port.getPlayFeed = vi.fn(
      () =>
        new Promise<FeedPage<Track>>((resolve) => {
          reply = resolve;
        }),
    );
    await f.controller.play([song('current')]);
    await f.controller.startFeed();
    const next = f.controller.next();
    await vi.advanceTimersByTimeAsync(1);
    await f.controller.play([song('different')]);
    reply({ items: [{ id: 'late-item', track: song('late') }], nextCursor: '', exhausted: true });
    await next;
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['different']);
    f.port.getPlayFeed = vi.fn(async () => ({
      items: [{ id: 'new-item', track: song('new') }],
      nextCursor: '',
      exhausted: true,
    }));
    f.controller.status({
      isLoaded: true,
      currentTime: 1,
      duration: 200,
      playing: true,
      isBuffering: false,
      didJustFinish: false,
    });
    await vi.advanceTimersByTimeAsync(800);
    expect(f.controller.getSnapshot().queue.map((t) => t.id)).toEqual(['different', 'new']);
  });
  it('restores recommendation indices after deleted entries and preserves an explicit opt-out', async () => {
    const f = setup();
    f.port.queue = async () => ({
      trackIds: ['missing', 'current', 'auto'],
      currentTrackId: 'current',
      positionMs: 0n,
      recommendations: [{ index: 2, playFeedItemId: 'restore-item' }],
      playFeedEnabled: false,
    });
    f.port.getPlayFeed = vi.fn(async () => ({ items: [], nextCursor: '', exhausted: true }));
    f.controller.connect(f.port);
    controllers.push(f.controller);
    await vi.waitFor(() => expect(f.controller.getSnapshot().queue).toHaveLength(2));
    expect(f.controller.recommendation(f.controller.getSnapshot().queue[1])).toBe('restore-item');
    expect(f.controller.getSnapshot().playFeedEnabled).toBe(false);
    expect(f.port.getPlayFeed).not.toHaveBeenCalled();
  });
  it('does not resurrect a trimmed legacy feed when turning restored shuffle off', async () => {
    const f = setup();
    f.port.queue = async () => ({
      trackIds: ['current', ...Array.from({ length: 10 }, (_, i) => 'old-' + i)],
      currentTrackId: 'current',
      positionMs: 0n,
      recommendations: Array.from({ length: 10 }, (_, i) => ({
        index: i + 1,
        playFeedItemId: 'old-item-' + i,
      })),
      playFeedEnabled: true,
    });
    f.controller.connect(f.port);
    controllers.push(f.controller);
    await vi.waitFor(() => expect(f.controller.getSnapshot().queue).toHaveLength(4));
    f.controller.restoreSettings({ shuffle: true });
    f.controller.toggleShuffle();
    expect(f.controller.getSnapshot().queue).toHaveLength(4);
    expect(f.controller.getSnapshot().track?.id).toBe('current');
  });
});

it('prepares the next source and starts it synchronously from ended without a network wait', async () => {
  const f = connected();
  const source = vi.fn(f.port.source);
  f.port.source = source;
  await f.controller.play([song('a'), song('b'), song('c')]);
  await vi.waitFor(() => expect(source).toHaveBeenCalledWith('b'));
  await Promise.resolve();
  f.position(200);
  f.controller.status({
    isLoaded: true,
    currentTime: 200,
    duration: 200,
    playing: false,
    isBuffering: false,
    didJustFinish: true,
  });
  // No await: a hidden document must request play before its ended task returns.
  expect(f.sources.at(-1)).toEqual({ uri: 'https://music.example/temporary/b' });
  expect(f.audio.play).toHaveBeenCalledTimes(2);
  expect(source.mock.calls.filter(([id]) => id === 'b')).toHaveLength(1);
  expect(f.controller.getSnapshot().track?.id).toBe('b');
});

it('releases prepared grants after queue edits, clear, and late replies after sign-out', async () => {
  const f = connected();
  await f.controller.play([song('a'), song('b'), song('c')]);
  await Promise.resolve();
  f.controller.remove(1);
  await vi.waitFor(() => expect(f.released).toContain('b'));
  await f.controller.clear();
  await vi.waitFor(() => expect(f.released).toContain('c'));
  expect(f.released.filter((id) => id === 'b')).toHaveLength(1);
  let reply!: (recording: Awaited<ReturnType<PlaybackPort['source']>>) => void;
  const original = f.port.source;
  f.port.source = (id) =>
    id === 'late'
      ? new Promise((resolve) => {
          reply = resolve;
        })
      : original(id);
  await f.controller.play([song('a'), song('late')]);
  f.controller.disconnect();
  const release = vi.fn(async () => {});
  reply({ source: { uri: 'late' }, release });
  await vi.waitFor(() => expect(release).toHaveBeenCalledTimes(1));
  expect(f.sources.at(-1)).toBeNull();
});
