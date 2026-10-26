import { api, session } from './session.svelte';

export type ListeningOrigin = 'direct' | 'queue' | 'restored';
export type ChangeReason = 'next' | 'previous' | 'selection' | 'ended' | 'stop';
type Outcome = 'completed' | 'skipped' | 'stopped';
type Listen = {
  id: string;
  owner: string;
  trackId: string;
  origin: ListeningOrigin;
  playFeedItemId: string;
  started: boolean;
  listened: number;
};

// Audio time counts only while it advances normally. A seek resets the baseline.
export class ListeningReporter {
  private current?: Listen;
  private baseline?: { position: number; time: number };
  private writes = Promise.resolve();
  private lastReport = 0;

  start(
    trackId: string,
    origin: ListeningOrigin,
    position: number,
    playFeedItemId = '',
  ) {
    if (session.status !== 'signed_in' || !session.user) return;
    if (!this.current) {
      this.current = {
        id: crypto.randomUUID(),
        owner: session.user.id,
        trackId,
        origin,
        playFeedItemId,
        started: false,
        listened: 0,
      };
    }
    this.report(position, true);
    this.reset(position);
  }

  reset(position: number) {
    this.baseline = { position, time: performance.now() };
  }

  sample(position: number, playing: boolean) {
    const now = performance.now();
    if (this.current && this.baseline && playing) {
      const delta = position - this.baseline.position;
      const elapsed = (now - this.baseline.time) / 1000;
      if (delta > 0 && delta <= elapsed + 0.25)
        this.current.listened += Math.min(delta, elapsed) * 1000;
    }
    this.baseline = { position, time: now };
    if (this.current && now - this.lastReport >= 15000)
      this.report(position, playing);
  }

  report(
    position: number,
    playing: boolean,
    outcome?: Outcome,
    changeReason?: ChangeReason,
  ) {
    const listen = this.current;
    if (!listen) return;
    this.lastReport = performance.now();
    const progress = {
      sessionId: listen.id,
      listenedMs: BigInt(Math.round(listen.listened)),
      positionMs: BigInt(Math.max(0, Math.round(position * 1000))),
    };
    // Capture each event before enqueueing it, preserving order across track changes.
    this.writes = this.writes
      .then(async () => {
        if (session.status !== 'signed_in' || session.user?.id !== listen.owner)
          return;
        if (!listen.started) {
          await api.listening.startListeningSession({
            trackId: listen.trackId,
            origin: listen.origin,
            requestId: listen.id,
            playFeedItemId: listen.playFeedItemId,
          });
          listen.started = true;
        }
        if (outcome)
          await api.listening.finishListeningSession({
            ...progress,
            outcome,
            changeReason,
          });
        else
          await api.listening.reportListeningProgress({ ...progress, playing });
      })
      .catch(() => {
        // The next report retries the start if needed and carries cumulative time.
      });
  }

  flush() {
    return this.writes;
  }

  finish(
    position: number,
    playing: boolean,
    outcome: Outcome,
    changeReason: ChangeReason = outcome === 'completed'
      ? 'ended'
      : outcome === 'stopped'
        ? 'stop'
        : 'selection',
  ) {
    this.sample(position, playing);
    this.report(position, false, outcome, changeReason);
    this.current = undefined;
    this.baseline = undefined;
    return this.writes;
  }
}
