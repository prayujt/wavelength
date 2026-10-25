import type { Track } from '../gen/wavelength/v1/library_pb';
import { api, session } from './session.svelte';
import { errorMessage } from './api';

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
  private generation = 0;
  private pendingPosition = 0;
  private recorded = false;
  private lastSaved = 0;
  private lastHeartbeat = 0;
  private saves = Promise.resolve();

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
      this.tracks = tracks.filter((track): track is Track => !!track);
      const index = this.tracks.findIndex(
        (track) => track.id === queue.currentTrackId,
      );
      if (this.tracks.length)
        await this.open(
          Math.max(0, index),
          false,
          Number(queue.positionMs) / 1000,
        );
    } catch (error) {
      this.error = errorMessage(error);
    }
  }

  async play(tracks: Track[], index = 0) {
    this.tracks = tracks;
    await this.open(index, true);
  }

  async open(index: number, autoplay: boolean, position = 0) {
    if (!this.audio || !this.tracks[index]) return;
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

  async next() {
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
    this.audio.currentTime = position;
    this.position = position;
    this.save();
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
        if (this.audio && session.status === 'signed_in')
          await api.user.saveQueue(request);
      })
      .catch((error) => {
        this.error = errorMessage(error);
      });
  }

  dispose() {
    ++this.generation;
    this.audio?.pause();
    this.audio?.removeAttribute('src');
    this.audio?.load();
    this.audio = undefined;
  }
}
