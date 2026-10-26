import type { Track } from '../gen/wavelength/v1/library_pb';
import { api, session } from './session.svelte';
import { errorMessage } from './api';
import { PlayFeedQueue } from './play-feed';
import {
  ListeningReporter,
  type ListeningOrigin,
  type ChangeReason,
} from './listening';

export class PlayerState {
  tracks = $state.raw<Track[]>([]);
  index = $state(-1);
  position = $state(0);
  length = $state(0);
  playing = $state(false);
  loading = $state(false);
  error = $state('');
  track = $derived(this.tracks[this.index]);
  audio?: HTMLAudioElement;
  playFeedEnabled = $state(true);
  playFeedLoading = $state(false);
  playFeedError = $state(false);
  startingFeed = $state(false);
  private startingFeedRequest?: Promise<void>;
  private feed = new PlayFeedQueue<Track>({
    read: () => ({ tracks: this.tracks, index: this.index }),
    apply: (tracks, index) => {
      this.tracks = tracks;
      this.index = index;
      this.save();
    },
    fetch: (input, signal) =>
      api.playfeed.getPlayFeed(input, { signal, timeoutMs: 12000 }),
    flush: () => {
      this.listening.report(
        this.audio?.currentTime ?? this.position,
        this.playing,
      );
      return this.listening.flush();
    },
    active: () => session.status === 'signed_in' && !!this.audio,
    changed: (status) => {
      this.playFeedEnabled = status.enabled;
      this.playFeedLoading = status.loading;
      this.playFeedError = status.error;
    },
    uuid: () => crypto.randomUUID(),
  });
  recommendation(track: Track) {
    return this.feed.recommendation(track);
  }
  refreshRecommendations() {
    this.feed.schedule();
  }
  startFeed(fresh = false): Promise<void> {
    if (this.startingFeedRequest) return this.startingFeedRequest;
    if (
      !fresh &&
      this.track &&
      this.tracks.every((track) => this.feed.recommendation(track))
    )
      return this.playing ? Promise.resolve() : this.toggle();
    ++this.generation;
    this.startingFeed = true;
    this.error = '';
    const request = (async () => {
      try {
        // The feed route can mount before the persistent audio element.
        if (!this.audio)
          await new Promise<void>((resolve) =>
            requestAnimationFrame(() => resolve()),
          );
        if (await this.feed.start()) await this.open(0, true, 0, 'queue');
      } catch (error) {
        this.error = errorMessage(error);
      } finally {
        this.startingFeed = false;
        this.startingFeedRequest = undefined;
      }
    })();
    this.startingFeedRequest = request;
    return request;
  }
  togglePlayFeed() {
    this.feed.setEnabled(!this.playFeedEnabled);
    this.save();
  }
  remove(index: number) {
    if (index <= this.index || index >= this.tracks.length) return;
    this.feed.dismiss(this.tracks[index]);
    this.tracks = this.tracks.filter((_, at) => at !== index);
    this.save();
    this.feed.schedule();
  }
  private deletedIDs = new Set<string>();
  private generation = 0;
  private navigation = 0;
  private waitingForFeed = 0;
  private pendingPosition = 0;
  private recorded = false;
  private lastSaved = 0;
  private lastHeartbeat = 0;
  private saves = Promise.resolve();
  private listening = new ListeningReporter();
  private origin: ListeningOrigin = 'queue';

  async restore() {
    if (this.startingFeed) return;
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
      if (generation !== this.generation || !this.audio || this.startingFeed)
        return;
      this.tracks = tracks.filter(
        (track): track is Track => !!track && !this.deletedIDs.has(track.id),
      );
      const recommendations = queue.recommendations.flatMap((item) => {
        const target = tracks[item.index];
        const index = target ? this.tracks.indexOf(target) : -1;
        return index >= 0
          ? [{ index, playFeedItemId: item.playFeedItemId }]
          : [];
      });
      const index = this.tracks.findIndex(
        (track) => track.id === queue.currentTrackId,
      );
      this.index = Math.max(0, index);
      this.feed.restore(recommendations, queue.playFeedEnabled ?? true);
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
    this.finishListen('skipped', 'selection');
    this.feed.reset();
    this.tracks = tracks.map((track) => ({ ...track }));
    await this.open(index, true, 0, 'direct');
  }

  async open(
    index: number,
    autoplay: boolean,
    position = 0,
    origin: ListeningOrigin = 'queue',
    reason: ChangeReason = 'selection',
  ) {
    if (!this.audio || !this.tracks[index]) return;
    ++this.navigation;
    this.waitingForFeed = 0;
    this.finishListen('skipped', reason);
    this.feed.cancel();
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
      this.feed.schedule();
    } catch (error) {
      if (generation === this.generation) this.error = errorMessage(error);
    } finally {
      if (generation === this.generation) this.loading = false;
    }
  }

  async toggle() {
    ++this.navigation;
    this.feed.schedule();
    if (!this.audio || !this.track) return;
    try {
      if (this.audio.paused) await this.audio.play();
      else this.audio.pause();
    } catch {
      this.error = 'Playback could not start. Please try pressing play again.';
    }
  }

  async next(completed = false) {
    const intent = ++this.navigation;
    this.waitingForFeed = 0;
    this.finishListen(
      completed ? 'completed' : 'skipped',
      completed ? 'ended' : 'next',
    );
    if (!completed && this.track) this.feed.avoid(this.track.id);
    const generation = this.generation;
    if (this.index + 1 >= this.tracks.length && this.playFeedEnabled) {
      this.waitingForFeed = intent;
      this.audio?.pause();
      try {
        await this.feed.refresh(true);
      } finally {
        if (this.waitingForFeed === intent) this.waitingForFeed = 0;
      }
      if (generation !== this.generation || intent !== this.navigation) return;
    }
    if (this.index + 1 < this.tracks.length)
      await this.open(
        this.index + 1,
        true,
        0,
        'queue',
        completed ? 'ended' : 'next',
      );
    else {
      this.audio?.pause();
      this.playing = false;
      this.save();
    }
  }

  async previous() {
    ++this.navigation;
    if (this.position > 3) this.seek(0);
    else if (this.index > 0)
      await this.open(this.index - 1, true, 0, 'queue', 'previous');
  }

  enqueue(track: Track) {
    this.tracks = this.tracks.filter(
      (item, index) =>
        index <= this.index ||
        item.id !== track.id ||
        !this.feed.recommendation(item),
    );
    this.tracks = [...this.tracks, { ...track }];
    this.feed.schedule();
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
        this.feed.recommendation(this.track),
      );
    if (!this.recorded && this.track) {
      this.recorded = true;
      void api.user.recordPlay({ trackId: this.track.id }).catch((error) => {
        this.error = errorMessage(error);
      });
      this.lastHeartbeat = Date.now();
    }
    this.feed.schedule();
  }

  update() {
    if (!this.audio || this.audio.readyState === 0) return;
    this.position = this.audio.currentTime;
    this.feed.maintain();
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
    ++this.navigation;
    if (!this.audio) return;
    this.listening.sample(this.audio.currentTime, this.playing);
    this.listening.reset(position);
    this.audio.currentTime = position;
    this.position = position;
    this.listening.report(position, this.playing);
    this.save();
    this.feed.schedule();
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
      if (!this.waitingForFeed) this.feed.schedule();
    }
  }

  seeking() {
    if (this.audio) this.listening.reset(this.audio.currentTime);
  }

  private finishListen(
    outcome: 'completed' | 'skipped' | 'stopped',
    reason?: ChangeReason,
  ) {
    this.listening.finish(
      this.audio?.currentTime || this.position,
      this.playing && !this.audio?.seeking,
      outcome,
      reason,
    );
  }

  save() {
    if (!this.track || !this.audio || session.status !== 'signed_in') return;
    this.lastSaved = Date.now();
    const request = {
      trackIds: this.tracks.map((track) => track.id),
      currentTrackId: this.track.id,
      positionMs: BigInt(Math.round(this.position * 1000)),
      recommendations: this.feed.references(),
      playFeedEnabled: this.playFeedEnabled,
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
    this.feed.avoid(id);
    this.feed.cancel();
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
      this.feed.schedule();
    }
  }
  dispose() {
    this.feed.cancel();
    this.finishListen('stopped');
    ++this.generation;
    this.audio?.pause();
    this.audio?.removeAttribute('src');
    this.audio?.load();
    this.audio = undefined;
  }
}
