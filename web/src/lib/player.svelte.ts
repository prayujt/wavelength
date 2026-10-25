import type { Track } from '../gen/wavelength/v1/library_pb';
import { api, session } from './session.svelte';
import { errorMessage } from './api';
import { ListeningReporter, type ListeningOrigin } from './listening';

export class PlayerState {
  tracks = $state<Track[]>([]);
  index = $state(-1);
  position = $state(0);
  length = $state(0);
  playing = $state(false);
  loading = $state(false);
  error = $state('');
  track = $derived(this.tracks[this.index]);
  audio?: HTMLAudioElement;
  private deletedIDs = new Set<string>();
  private generation = 0;
  private pendingPosition = 0;
  private recorded = false;
  private lastSaved = 0;
  private lastHeartbeat = 0;
  private saves = Promise.resolve();
  private listening = new ListeningReporter();
  private origin: ListeningOrigin = 'queue';

  async restore() {
    const generation = this.generation;
    try {
      const { queue } = await api.user.getQueue({});
      if (!queue?.trackIds.length) return;
      const tracks = await Promise.all(
        queue.trackIds.map(async (id) => {
          try {
            return (await api.library.getTrack({ id })).track;
          } catch {
            return undefined;
          }
        }),
      );
      if (generation !== this.generation || !this.audio) return;
      this.tracks = tracks.filter(
        (track): track is Track => !!track && !this.deletedIDs.has(track.id),
      );
      const index = this.tracks.findIndex(
        (track) => track.id === queue.currentTrackId,
      );
      if (this.tracks.length)
        await this.open(
          Math.max(0, index),
          false,
          Number(queue.positionMs) / 1000,
          'restored',
        );
    } catch (error) {
      this.error = errorMessage(error);
    }
  }

  async play(tracks: Track[], index = 0) {
    this.finishListen('skipped');
    this.tracks = tracks;
    await this.open(index, true, 0, 'direct');
  }

  async open(
    index: number,
    autoplay: boolean,
    position = 0,
    origin: ListeningOrigin = 'queue',
  ) {
    if (!this.audio || !this.tracks[index]) return;
    this.finishListen('skipped');
    this.origin = origin;
    const generation = ++this.generation;
    this.audio.pause();
    this.audio.removeAttribute('src');
    this.audio.load();
    this.index = index;
    this.position = position;
    this.pendingPosition = position;
    this.length = this.track.durationSeconds;
    this.recorded = false;
    this.loading = true;
    this.error = '';
    try {
      const info = await api.library.getPlaybackInfo({ id: this.track.id });
      if (generation !== this.generation || !this.audio) return;
      this.audio.src = info.httpPath;
      this.audio.load();
      this.save();
      if (autoplay) await this.audio.play();
    } catch (error) {
      if (generation === this.generation) this.error = errorMessage(error);
    } finally {
      if (generation === this.generation) this.loading = false;
    }
  }

  async toggle() {
    if (!this.audio || !this.track) return;
    try {
      if (this.audio.paused) await this.audio.play();
      else this.audio.pause();
    } catch {
      this.error = 'Playback could not start. Please try pressing play again.';
    }
  }

  async next(completed = false) {
    this.finishListen(completed ? 'completed' : 'skipped');
    if (this.index + 1 < this.tracks.length)
      await this.open(this.index + 1, true);
    else this.save();
  }

  async previous() {
    if (this.position > 3) this.seek(0);
    else if (this.index > 0) await this.open(this.index - 1, true);
  }

  enqueue(track: Track) {
    this.tracks = [...this.tracks, track];
    if (this.index < 0) void this.open(0, false);
    else this.save();
  }

  loaded() {
    if (!this.audio) return;
    this.length = Number.isFinite(this.audio.duration)
      ? this.audio.duration
      : this.track?.durationSeconds || 0;
    this.audio.currentTime = Math.min(this.pendingPosition, this.length);
    this.pendingPosition = 0;
  }

  started() {
    this.playing = true;
    if (this.track)
      this.listening.start(
        this.track.id,
        this.origin,
        this.audio?.currentTime || 0,
      );
    if (!this.recorded && this.track) {
      this.recorded = true;
      void api.user.recordPlay({ trackId: this.track.id }).catch((error) => {
        this.error = errorMessage(error);
      });
      this.lastHeartbeat = Date.now();
    }
  }

  update() {
    if (!this.audio || this.audio.readyState === 0) return;
    this.position = this.audio.currentTime;
    this.listening.sample(this.position, this.playing && !this.audio.seeking);
    if (Date.now() - this.lastSaved > 15000) this.save();
    if (this.playing && this.track && Date.now() - this.lastHeartbeat > 60000) {
      this.lastHeartbeat = Date.now();
      void api.user
        .recordPlay({ trackId: this.track.id, nowPlayingOnly: true })
        .catch((error) => {
          this.error = errorMessage(error);
        });
    }
  }

  seek(position: number) {
    if (!this.audio) return;
    this.listening.sample(this.audio.currentTime, this.playing);
    this.listening.reset(position);
    this.audio.currentTime = position;
    this.position = position;
    this.listening.report(position, this.playing);
    this.save();
  }

  paused() {
    if (this.audio)
      this.listening.sample(
        this.audio.currentTime,
        this.playing && !this.audio.seeking,
      );
    this.playing = false;
    if (!this.loading) {
      this.listening.report(this.audio?.currentTime || this.position, false);
      this.save();
    }
  }

  seeking() {
    if (this.audio) this.listening.reset(this.audio.currentTime);
  }

  private finishListen(outcome: 'completed' | 'skipped' | 'stopped') {
    this.listening.finish(
      this.audio?.currentTime || this.position,
      this.playing && !this.audio?.seeking,
      outcome,
    );
  }

  save() {
    if (!this.track || !this.audio || session.status !== 'signed_in') return;
    this.lastSaved = Date.now();
    const request = {
      trackIds: this.tracks.map((track) => track.id),
      currentTrackId: this.track.id,
      positionMs: BigInt(Math.round(this.position * 1000)),
    };
    // Preserve write order when track changes and progress updates happen together.
    this.saves = this.saves
      .then(async () => {
        if (
          this.audio &&
          session.status === 'signed_in' &&
          !request.trackIds.some((id) => this.deletedIDs.has(id))
        )
          await api.user.saveQueue(request);
      })
      .catch((error) => {
        this.error = errorMessage(error);
      });
  }

  discardTrack(id: string) {
    this.deletedIDs.add(id);
    const current = this.track?.id;
    this.tracks = this.tracks.filter((track) => track.id !== id);
    if (current === id) {
      ++this.generation;
      this.index = -1;
      this.loading = this.playing = false;
      this.position = this.length = 0;
      this.listening = new ListeningReporter();
      this.audio?.pause();
      this.audio?.removeAttribute('src');
      this.audio?.load();
    } else {
      this.index = this.tracks.findIndex((track) => track.id === current);
      this.save();
    }
  }
  dispose() {
    this.finishListen('stopped');
    ++this.generation;
    this.audio?.pause();
    this.audio?.removeAttribute('src');
    this.audio?.load();
    this.audio = undefined;
  }
}
