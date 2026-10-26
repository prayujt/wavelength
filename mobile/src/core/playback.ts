import type { Track } from '../gen/wavelength/v1/library_pb';
import { PlayFeedQueue, type FeedReference, type FeedRequest, type FeedPage } from './play-feed';
import { ListeningReporter, type ListeningPort } from './listening';
import { moveItem, nextIndex, shuffled, type RepeatMode } from './queue';

export type AudioSource = number | { uri: string };
export interface AudioPort {
  pause(): void;
  replace(source: AudioSource | null): void;
  play(): void;
  seek(seconds: number): Promise<void>;
  position(): number;
  duration(): number;
  volume(value: number): void;
  metadata(track: Track): void;
  clearMetadata(): void;
}
export type QueueFeedState = { recommendations: FeedReference[]; playFeedEnabled?: boolean };
export interface PlaybackPort {
  getPlayFeed?(input: FeedRequest, signal: AbortSignal): Promise<FeedPage<Track>>;
  source(id: string): Promise<{ source: AudioSource; release(): Promise<unknown> }>;
  track(id: string): Promise<Track | undefined>;
  queue(): Promise<
    | {
        trackIds: string[];
        currentTrackId: string;
        positionMs: bigint;
        recommendations?: FeedReference[];
        playFeedEnabled?: boolean;
      }
    | undefined
  >;
  saveQueue(
    ids: string[],
    current: string,
    position: number,
    feed?: QueueFeedState,
  ): Promise<unknown>;
  record(id: string): Promise<unknown>;
  listening: ListeningPort;
}
export type PlaybackSnapshot = {
  queue: Track[];
  index: number;
  track?: Track;
  playing: boolean;
  loading: boolean;
  error: string;
  repeat: RepeatMode;
  shuffle: boolean;
  playFeedEnabled: boolean;
  playFeedLoading: boolean;
  playFeedError: boolean;
  startingFeed: boolean;
};
export type NativeStatus = {
  isLoaded: boolean;
  currentTime: number;
  duration: number;
  playing: boolean;
  isBuffering: boolean;
  didJustFinish: boolean;
  error?: string | null;
};
const empty = (): PlaybackSnapshot => ({
  queue: [],
  index: 0,
  playing: false,
  loading: false,
  error: '',
  repeat: 'off',
  shuffle: false,
  playFeedEnabled: false,
  playFeedLoading: false,
  playFeedError: false,
  startingFeed: false,
});

/** Owns playback independently of screen lifetimes and React render timing. */
export class PlaybackController {
  private snapshot = empty();
  private subscribers = new Set<() => void>();
  private port: PlaybackPort | null = null;
  private reporter: ListeningReporter | null = null;
  private serial = 0;
  private navigation = 0;
  private waitingForFeed = 0;
  private startingFeedRequest?: Promise<void>;
  private feedStartToken?: object;
  private loaded: string | null = null;
  private ended = false;
  private pendingSeek: number | null = null;
  private restoredPosition = 0;
  private original: Track[] = [];
  private release: (() => Promise<unknown>) | null = null;
  private saveTimer: ReturnType<typeof setTimeout> | null = null;
  private checkpoints: ReturnType<typeof setInterval> | null = null;
  private writes: Promise<unknown> = Promise.resolve();
  private feed = new PlayFeedQueue<Track>({
    read: () => ({ tracks: this.snapshot.queue, index: this.snapshot.index }),
    apply: (queue, index) => this.commit(queue, index, false),
    active: () => !!this.port?.getPlayFeed && this.snapshot.repeat === 'off',
    fetch: (input, signal) => this.port!.getPlayFeed!(input, signal),
    flush: () =>
      this.reporter?.report(this.audio.position(), this.snapshot.playing) ?? Promise.resolve(),
    uuid: () => this.random(),
    changed: (status) =>
      this.patch({
        playFeedEnabled: status.enabled,
        playFeedLoading: status.loading,
        playFeedError: status.error,
      }),
  });
  recommendation = (track: Track) => this.feed.recommendation(track);
  refreshRecommendations = () => this.feed.schedule();
  startFeed = (fresh = false): Promise<void> => {
    if (this.startingFeedRequest) return this.startingFeedRequest;
    const { track, playing, loading, playFeedEnabled } = this.snapshot;
    if (!fresh && playFeedEnabled && track)
      return playing || loading ? Promise.resolve() : this.toggle();
    const port = this.port;
    if (!port) return Promise.resolve();
    if (!fresh && track) {
      const recommendation = this.feed.recommendation(track);
      this.feed.reset();
      this.patch({ repeat: 'off', shuffle: false, error: '' });
      this.commit([track], 0, false);
      this.feed.restore(recommendation ? [{ index: 0, playFeedItemId: recommendation }] : [], true);
      this.original = [track];
      this.scheduleSave();
      if (loading) return Promise.resolve();
      this.feed.schedule();
      return playing ? Promise.resolve() : this.toggle();
    }
    ++this.serial;
    this.patch({ repeat: 'off', shuffle: false, startingFeed: true, error: '' });
    const token = {};
    this.feedStartToken = token;
    const request: Promise<void> = (async () => {
      try {
        if (await this.feed.start()) {
          this.original = [...this.snapshot.queue];
          await this.load(this.snapshot.queue[0], 0, 'queue');
        }
      } catch (error) {
        if (this.port === port && this.feedStartToken === token)
          this.patch({ error: this.message(error) });
      } finally {
        if (this.feedStartToken === token) {
          if (this.port === port) this.patch({ startingFeed: false });
          this.startingFeedRequest = undefined;
          this.feedStartToken = undefined;
        }
      }
    })();
    this.startingFeedRequest = request;
    return request;
  };
  stopFeed = () => {
    this.startingFeedRequest = undefined;
    this.feedStartToken = undefined;
    this.feed.setEnabled(false);
    this.patch({ startingFeed: false });
    this.scheduleSave();
  };
  togglePlayFeed = () => (this.snapshot.playFeedEnabled ? this.stopFeed() : this.startFeed());
  jump = async (index: number) => {
    if (index < 0 || index >= this.snapshot.queue.length) return;
    if (this.snapshot.playFeedEnabled) return this.play([this.snapshot.queue[index]]);
    this.commit(this.snapshot.queue, index);
    await this.load(this.snapshot.queue[index], 0, 'direct', 'skipped', 'selection');
  };

  constructor(
    private audio: AudioPort,
    private random: () => string,
    private message: (error: unknown) => string,
  ) {}
  getSnapshot = () => this.snapshot;
  hydrate = (tracks: Track[]) => {
    const known = new Map(tracks.map((track) => [track.id, track]));
    let changed = false;
    const queue = this.snapshot.queue.map((track) => {
      const detail = !track.title && known.get(track.id);
      if (detail) {
        changed = true;
        const next = { ...detail };
        this.feed.transfer(track, next);
        return next;
      }
      return track;
    });
    if (changed) this.patch({ queue, track: queue[this.snapshot.index] });
  };
  subscribe = (listener: () => void) => {
    this.subscribers.add(listener);
    return () => {
      this.subscribers.delete(listener);
    };
  };
  private patch(change: Partial<PlaybackSnapshot>) {
    this.snapshot = { ...this.snapshot, ...change };
    for (const listener of this.subscribers) listener();
  }
  private commit(queue: Track[], index: number, refresh = true) {
    const at = Math.max(0, Math.min(index, queue.length - 1));
    this.patch({ queue, index: at, track: queue[at] });
    this.scheduleSave();
    if (refresh) this.feed.schedule();
  }
  private async revoke() {
    const release = this.release;
    this.release = null;
    try {
      await release?.();
    } catch {
      /* The scoped grant also expires. */
    }
  }
  private scheduleSave() {
    if (this.saveTimer) clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => this.save(), 700);
  }
  save = () => {
    const port = this.port;
    if (!port) return this.writes;
    const { queue, track } = this.snapshot,
      position = this.audio.position() || this.restoredPosition,
      feed = {
        recommendations: this.feed.references(),
        playFeedEnabled: this.snapshot.playFeedEnabled,
      };
    this.writes = this.writes
      .then(() =>
        port.saveQueue(
          queue.map((t) => t.id),
          track?.id || '',
          position,
          feed,
        ),
      )
      .catch(() => {});
    return this.writes;
  };
  checkpoint = () => {
    void this.reporter?.report(this.audio.position(), this.snapshot.playing);
    void this.save();
    this.feed.schedule();
  };
  connect(port: PlaybackPort) {
    this.disconnect();
    this.port = port;
    this.reporter = new ListeningReporter(port.listening, this.random);
    const serial = this.serial;
    void (async () => {
      try {
        const queue = await port.queue();
        if (!queue?.trackIds.length) return;
        const tracks: Track[] = [];
        const references: FeedReference[] = [];
        for (let i = 0; i < queue.trackIds.length; i += 8) {
          if (serial !== this.serial || this.port !== port) return;
          const batch = await Promise.all(
            queue.trackIds.slice(i, i + 8).map((id) => port.track(id)),
          );
          if (serial !== this.serial || this.port !== port) return;
          batch.forEach((track, at) => {
            if (!track) return;
            const ref = queue.recommendations?.find((item) => item.index === i + at);
            if (ref) references.push({ index: tracks.length, playFeedItemId: ref.playFeedItemId });
            tracks.push(track);
          });
        }
        if (serial !== this.serial || this.snapshot.queue.length) return;
        const restoredIndex = tracks.findIndex((t) => t.id === queue.currentTrackId);
        const index = Math.max(0, restoredIndex);
        this.restoredPosition = restoredIndex >= 0 ? Number(queue.positionMs) / 1000 : 0;
        this.patch({ queue: tracks, index, track: tracks[index] });
        this.feed.restore(references, queue.playFeedEnabled ?? false);
        this.original = [...this.snapshot.queue];
        this.feed.schedule();
      } catch {
        /* A failed restore never overwrites a newer local queue. */
      }
    })();
    this.checkpoints = setInterval(() => {
      if (this.loaded) void this.save();
    }, 15000);
  }
  disconnect() {
    this.feedStartToken = undefined;
    this.startingFeedRequest = undefined;
    ++this.navigation;
    this.waitingForFeed = 0;
    this.feed.reset();
    this.serial++;
    const position = this.audio.position();
    this.audio.pause();
    this.audio.replace(null);
    this.audio.clearMetadata();
    if (this.saveTimer) clearTimeout(this.saveTimer);
    if (this.checkpoints) clearInterval(this.checkpoints);
    void this.reporter?.finish(position, 'stopped');
    void this.revoke();
    this.port = null;
    this.reporter = null;
    this.loaded = null;
    this.pendingSeek = null;
    this.restoredPosition = 0;
    this.original = [];
    this.snapshot = empty();
    this.feed.restore([], false);
    for (const listener of this.subscribers) listener();
  }
  private async load(
    track: Track,
    position = 0,
    origin = 'direct',
    outcome = 'skipped',
    reason = 'selection',
  ) {
    ++this.navigation;
    this.waitingForFeed = 0;
    const port = this.port;
    if (!port) return;
    const serial = ++this.serial,
      previous = this.audio.position();
    this.feed.cancel();
    this.restoredPosition = position;
    this.audio.pause();
    this.audio.replace(null);
    this.loaded = null;
    this.pendingSeek = null;
    void this.reporter?.finish(previous, outcome, reason);
    this.ended = false;
    this.patch({ loading: true, error: '', playing: false });
    void this.revoke();
    try {
      // A large playlist can enqueue its full ordered ID list immediately.
      // Resolve metadata only when a not-yet-hydrated song actually plays.
      if (!track.title) {
        const detail = await port.track(track.id);
        if (serial !== this.serial) return;
        if (!detail) throw new Error('This song is no longer available.');
        const queue = this.snapshot.queue.map((item) => {
          if (item.id !== detail.id) return item;
          const next = { ...detail };
          this.feed.transfer(item, next);
          return next;
        });
        track = queue[this.snapshot.index] || detail;
        this.patch({ queue, track });
      }
      const recording = await port.source(track.id);
      if (serial !== this.serial) {
        await recording.release().catch(() => {});
        return;
      }
      this.release = recording.release;
      this.loaded = track.id;
      this.pendingSeek = position > 0 ? position : null;
      this.reporter?.start(track.id, origin, this.feed.recommendation(track));
      this.audio.replace(recording.source);
      this.audio.metadata(track);
      this.audio.play();
      if (position === 0) this.restoredPosition = 0;
      void port.record(track.id).catch(() => {});
      this.scheduleSave();
      this.feed.schedule();
    } catch (error) {
      if (serial === this.serial) {
        this.loaded = null;
        this.patch({ error: this.message(error) });
      }
    } finally {
      if (serial === this.serial) this.patch({ loading: false });
    }
  }
  status = (status: NativeStatus) => {
    if (this.snapshot.playing !== status.playing) {
      this.patch({ playing: status.playing });
      if (this.loaded && !this.snapshot.loading && !this.waitingForFeed) this.feed.schedule();
    }
    if (status.isLoaded && this.pendingSeek !== null) {
      const seek = this.pendingSeek,
        serial = this.serial;
      this.pendingSeek = null;
      this.reporter?.seek(seek);
      void this.audio
        .seek(seek)
        .then(() => {
          if (serial !== this.serial) return;
          this.restoredPosition = 0;
          void this.save();
        })
        .catch(() => this.patch({ error: 'Could not restore your playback position.' }));
    }
    if (this.loaded && !this.ended && status.isLoaded)
      this.reporter?.sample(status.currentTime, status.playing && !status.isBuffering);
    if (status.didJustFinish && this.loaded && !this.ended) {
      this.ended = true;
      void this.advance(true);
    }
    if (this.loaded && status.isLoaded) this.feed.maintain();
    if (status.error && this.loaded) {
      this.loaded = null;
      this.patch({
        loading: false,
        error: 'This song could not be played. Check your connection and tap play to retry.',
      });
    }
  };
  play = async (tracks: Track[], index = 0, position = 0) => {
    if (!tracks.length || index < 0 || index >= tracks.length) return;
    this.feedStartToken = undefined;
    this.startingFeedRequest = undefined;
    this.patch({ startingFeed: false });
    const enabled = this.snapshot.playFeedEnabled;
    const recommendation = enabled ? this.feed.recommendation(tracks[index]) : '';
    this.feed.reset();
    const selected = enabled ? [tracks[index]] : tracks;
    const manual = selected.map((track) => ({ ...track }));
    const at = enabled ? 0 : index;
    this.original = manual;
    this.patch({ shuffle: false });
    this.commit(manual, at, false);
    if (recommendation) this.feed.restore([{ index: 0, playFeedItemId: recommendation }], true);
    await this.load(manual[at], position);
  };
  toggle = async () => {
    ++this.navigation;
    const { track, playing } = this.snapshot;
    if (!track) return;
    if (this.loaded !== track.id || this.ended) {
      await this.load(track, this.ended ? 0 : this.restoredPosition, 'restored');
      return;
    }
    if (playing) {
      this.audio.pause();
      this.patch({ playing: false });
      void this.reporter?.report(this.audio.position(), false);
      void this.save();
    } else {
      this.reporter?.seek(this.audio.position());
      this.audio.play();
    }
    this.feed.schedule();
  };
  private async advance(ended: boolean) {
    const before = this.snapshot,
      serial = this.serial,
      intent = ++this.navigation;
    if (!before.track) return;
    this.ended = true;
    void this.reporter?.finish(
      this.audio.position(),
      ended ? 'completed' : 'skipped',
      ended ? 'ended' : 'next',
    );
    if (!ended) this.feed.avoid(before.track.id);
    if (
      before.index + 1 >= before.queue.length &&
      before.repeat === 'off' &&
      before.playFeedEnabled &&
      this.port?.getPlayFeed
    ) {
      this.waitingForFeed = intent;
      this.audio.pause();
      try {
        await this.feed.refresh(true);
      } finally {
        if (this.waitingForFeed === intent) this.waitingForFeed = 0;
      }
      if (
        serial !== this.serial ||
        this.snapshot.track !== before.track ||
        intent !== this.navigation
      )
        return;
    }
    const state = this.snapshot,
      next = nextIndex(state.queue.length, state.index, state.repeat, ended);
    if (next === null) {
      this.audio.pause();
      this.ended = true;
      this.patch({ playing: false });
      void this.save();
      return;
    }
    this.commit(state.queue, next);
    await this.load(
      state.queue[next],
      0,
      'queue',
      ended ? 'completed' : 'skipped',
      ended ? 'ended' : 'next',
    );
  }
  next = () => this.advance(false);
  previous = async () => {
    if (!this.snapshot.track) return;
    if (this.audio.position() > 3) {
      await this.seek(0);
      return;
    }
    const index = Math.max(0, this.snapshot.index - 1);
    this.commit(this.snapshot.queue, index);
    await this.load(this.snapshot.queue[index], 0, 'queue', 'skipped', 'previous');
  };
  seek = async (seconds: number) => {
    ++this.navigation;
    const position = Math.max(0, Math.min(seconds, this.audio.duration() || seconds));
    this.reporter?.seek(position);
    this.restoredPosition = 0;
    await this.audio.seek(position);
    void this.reporter?.report(position, this.snapshot.playing);
    void this.save();
    this.feed.schedule();
  };
  add = (tracks: Track[], next = false) => {
    const state = this.snapshot,
      queue = state.queue.filter(
        (item, index) =>
          index <= state.index ||
          !tracks.some((track) => track.id === item.id) ||
          !this.feed.recommendation(item),
      );
    queue.splice(
      next ? state.index + 1 : queue.length,
      0,
      ...tracks.map((track) => ({ ...track })),
    );
    this.original = queue;
    this.commit(queue, state.index);
  };
  remove = (index: number) => {
    const state = this.snapshot;
    if (index === state.index || index < 0 || index >= state.queue.length) return;
    this.feed.dismiss(state.queue[index]);
    const queue = state.queue.filter((_, i) => i !== index);
    this.original = queue;
    this.commit(queue, index < state.index ? state.index - 1 : state.index);
  };
  move = (from: number, to: number) => {
    const state = this.snapshot;
    if (from < 0 || to < 0 || from >= state.queue.length || to >= state.queue.length) return;
    this.feed.promote(state.queue[from]);
    const queue = moveItem(state.queue, from, to);
    let index = state.index;
    if (index === from) index = to;
    else if (from < index && to >= index) index--;
    else if (from > index && to <= index) index++;
    this.original = queue;
    this.commit(queue, index);
  };
  toggleShuffle = () => {
    const state = this.snapshot;
    if (!state.track) return;
    for (const track of state.queue) this.feed.promote(track);
    if (state.shuffle)
      this.commit(
        this.original,
        Math.max(
          0,
          this.original.findIndex((t) => t.id === state.track!.id),
        ),
      );
    else {
      this.original = [...state.queue];
      this.commit([state.track, ...shuffled(state.queue.filter((_, i) => i !== state.index))], 0);
    }
    this.patch({ shuffle: !state.shuffle });
  };
  toggleRepeat = () => {
    const repeat =
      this.snapshot.repeat === 'off' ? 'all' : this.snapshot.repeat === 'all' ? 'one' : 'off';
    this.patch({ repeat });
    this.feed.schedule();
  };
  restoreSettings = (settings: { repeat?: RepeatMode; shuffle?: boolean }) =>
    this.patch({ repeat: settings.repeat || 'off', shuffle: settings.shuffle || false });
  setVolume = (value: number) => {
    this.audio.volume(Math.max(0, Math.min(1, value)));
    this.feed.schedule();
  };
  clear = async () => {
    this.feedStartToken = undefined;
    this.startingFeedRequest = undefined;
    ++this.navigation;
    this.waitingForFeed = 0;
    this.feed.reset();
    this.feed.setEnabled(false);
    this.serial++;
    const position = this.audio.position();
    this.audio.pause();
    this.audio.replace(null);
    this.audio.clearMetadata();
    void this.reporter?.finish(position, 'stopped');
    this.loaded = null;
    this.original = [];
    this.restoredPosition = 0;
    this.commit([], 0);
    this.patch({ error: '', playing: false, startingFeed: false });
    await this.revoke();
    await this.save();
  };
}
