import type { Track } from '../gen/wavelength/v1/library_pb';
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
export interface PlaybackPort {
  source(id: string): Promise<{ source: AudioSource; release(): Promise<unknown> }>;
  track(id: string): Promise<Track | undefined>;
  queue(): Promise<{ trackIds: string[]; currentTrackId: string; positionMs: bigint } | undefined>;
  saveQueue(ids: string[], current: string, position: number): Promise<unknown>;
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
});

/** Owns playback independently of screen lifetimes and React render timing. */
export class PlaybackController {
  private snapshot = empty();
  private subscribers = new Set<() => void>();
  private port: PlaybackPort | null = null;
  private reporter: ListeningReporter | null = null;
  private serial = 0;
  private loaded: string | null = null;
  private ended = false;
  private pendingSeek: number | null = null;
  private restoredPosition = 0;
  private original: Track[] = [];
  private release: (() => Promise<unknown>) | null = null;
  private saveTimer: ReturnType<typeof setTimeout> | null = null;
  private checkpoints: ReturnType<typeof setInterval> | null = null;
  private writes: Promise<unknown> = Promise.resolve();
  constructor(
    private audio: AudioPort,
    private random: () => string,
    private message: (error: unknown) => string,
  ) {}
  getSnapshot = () => this.snapshot;
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
  private commit(queue: Track[], index: number) {
    const at = Math.max(0, Math.min(index, queue.length - 1));
    this.patch({ queue, index: at, track: queue[at] });
    this.scheduleSave();
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
      position = this.audio.position() || this.restoredPosition;
    this.writes = this.writes
      .then(() =>
        port.saveQueue(
          queue.map((t) => t.id),
          track?.id || '',
          position,
        ),
      )
      .catch(() => {});
    return this.writes;
  };
  checkpoint = () => {
    void this.reporter?.report(this.audio.position(), this.snapshot.playing);
    void this.save();
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
        for (let i = 0; i < queue.trackIds.length; i += 8) {
          if (serial !== this.serial || this.port !== port) return;
          const batch = await Promise.all(
            queue.trackIds.slice(i, i + 8).map((id) => port.track(id)),
          );
          if (serial !== this.serial || this.port !== port) return;
          tracks.push(...batch.filter((t): t is Track => !!t));
        }
        if (serial !== this.serial || this.snapshot.queue.length) return;
        const index = Math.max(
          0,
          tracks.findIndex((t) => t.id === queue.currentTrackId),
        );
        this.original = tracks;
        this.restoredPosition = Number(queue.positionMs) / 1000;
        this.patch({ queue: tracks, index, track: tracks[index] });
      } catch {
        /* A failed restore never overwrites a newer local queue. */
      }
    })();
    this.checkpoints = setInterval(() => {
      if (this.loaded) void this.save();
    }, 15000);
  }
  disconnect() {
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
    for (const listener of this.subscribers) listener();
  }
  private async load(track: Track, position = 0, origin = 'direct', outcome = 'skipped') {
    const port = this.port;
    if (!port) return;
    const serial = ++this.serial,
      previous = this.audio.position();
    this.restoredPosition = position;
    this.audio.pause();
    this.audio.replace(null);
    this.loaded = null;
    this.pendingSeek = null;
    void this.reporter?.finish(previous, outcome);
    this.ended = false;
    this.patch({ loading: true, error: '', playing: false });
    void this.revoke();
    try {
      const recording = await port.source(track.id);
      if (serial !== this.serial) {
        await recording.release().catch(() => {});
        return;
      }
      this.release = recording.release;
      this.loaded = track.id;
      this.pendingSeek = position > 0 ? position : null;
      this.reporter?.start(track.id, origin);
      this.audio.replace(recording.source);
      this.audio.metadata(track);
      this.audio.play();
      if (position === 0) this.restoredPosition = 0;
      void port.record(track.id).catch(() => {});
      this.scheduleSave();
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
    if (this.snapshot.playing !== status.playing) this.patch({ playing: status.playing });
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
    this.original = [...tracks];
    this.patch({ shuffle: false });
    this.commit([...tracks], index);
    await this.load(tracks[index], position);
  };
  toggle = async () => {
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
  };
  private async advance(ended: boolean) {
    const state = this.snapshot,
      next = nextIndex(state.queue.length, state.index, state.repeat, ended);
    if (next === null) {
      this.audio.pause();
      this.ended = true;
      this.patch({ playing: false });
      void this.reporter?.finish(this.audio.position(), ended ? 'completed' : 'skipped');
      void this.save();
      return;
    }
    this.commit(state.queue, next);
    await this.load(state.queue[next], 0, 'queue', ended ? 'completed' : 'skipped');
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
    await this.load(this.snapshot.queue[index], 0, 'queue');
  };
  seek = async (seconds: number) => {
    const position = Math.max(0, Math.min(seconds, this.audio.duration() || seconds));
    this.reporter?.seek(position);
    this.restoredPosition = 0;
    await this.audio.seek(position);
    void this.save();
  };
  add = (tracks: Track[], next = false) => {
    const state = this.snapshot,
      queue = [...state.queue];
    queue.splice(next ? state.index + 1 : queue.length, 0, ...tracks);
    this.original = queue;
    this.commit(queue, state.index);
  };
  remove = (index: number) => {
    const state = this.snapshot;
    if (index === state.index || index < 0 || index >= state.queue.length) return;
    const queue = state.queue.filter((_, i) => i !== index);
    this.original = queue;
    this.commit(queue, index < state.index ? state.index - 1 : state.index);
  };
  move = (from: number, to: number) => {
    const state = this.snapshot;
    if (from < 0 || to < 0 || from >= state.queue.length || to >= state.queue.length) return;
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
  toggleRepeat = () =>
    this.patch({
      repeat:
        this.snapshot.repeat === 'off' ? 'all' : this.snapshot.repeat === 'all' ? 'one' : 'off',
    });
  setVolume = (value: number) => this.audio.volume(Math.max(0, Math.min(1, value)));
  clear = async () => {
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
    this.patch({ error: '', playing: false });
    await this.revoke();
    await this.save();
  };
}
