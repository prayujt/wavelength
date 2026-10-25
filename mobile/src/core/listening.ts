type EventTime = { seconds: bigint; nanos: number };
const eventTime = (now: number): EventTime => ({
  seconds: BigInt(Math.floor(now / 1000)),
  nanos: (now % 1000) * 1000000,
});
export type Progress = {
  listenedMs: bigint;
  positionMs: bigint;
  playing: boolean;
  occurredAt?: EventTime;
};
export interface ListeningPort {
  startListeningSession(input: {
    trackId: string;
    origin: string;
    requestId: string;
    occurredAt?: EventTime;
  }): Promise<unknown>;
  reportListeningProgress(input: Progress & { sessionId: string }): Promise<unknown>;
  finishListeningSession(
    input: Omit<Progress, 'playing'> & { sessionId: string; outcome: string },
  ): Promise<unknown>;
}

/** Count advancing audio, never elapsed wall time during pauses, buffering or seeks. */
export class ListeningReporter {
  private current?: {
    id: string;
    trackId: string;
    origin: string;
    listened: number;
    started: boolean;
    at: EventTime;
  };
  private baseline?: { position: number; time: number };
  private writes: Promise<unknown> = Promise.resolve();
  private lastReport = 0;
  constructor(
    private api: ListeningPort,
    private random: () => string,
    private now = Date.now,
  ) {}

  start(trackId: string, origin: string) {
    this.current = {
      id: this.random(),
      trackId,
      origin,
      listened: 0,
      started: false,
      at: eventTime(this.now()),
    };
    this.baseline = undefined;
    this.lastReport = 0;
  }
  seek(position: number) {
    this.baseline = { position, time: this.now() };
  }
  sample(position: number, playing: boolean) {
    const now = this.now();
    if (this.current && this.baseline && playing) {
      const delta = position - this.baseline.position;
      const elapsed = Math.max(0, (now - this.baseline.time) / 1000);
      if (delta > 0 && delta <= elapsed + 0.25)
        this.current.listened += Math.min(delta, elapsed) * 1000;
    }
    this.baseline = { position, time: now };
    if (this.current && now - this.lastReport >= 5000) this.report(position, playing);
  }
  report(position: number, playing: boolean, outcome?: string) {
    const session = this.current;
    if (!session) return this.writes;
    this.lastReport = this.now();
    const progress = {
      sessionId: session.id,
      listenedMs: BigInt(Math.round(session.listened)),
      positionMs: BigInt(Math.round(Math.max(0, position) * 1000)),
      occurredAt: eventTime(this.now()),
    };
    this.writes = this.writes
      .then(async () => {
        if (!session.started) {
          await this.api.startListeningSession({
            trackId: session.trackId,
            origin: session.origin,
            requestId: session.id,
            occurredAt: session.at,
          });
          session.started = true;
        }
        if (outcome) await this.api.finishListeningSession({ ...progress, outcome });
        else await this.api.reportListeningProgress({ ...progress, playing });
      })
      .catch(() => {
        /* The next cumulative report retries a failed start. */
      });
    return this.writes;
  }
  finish(position: number, outcome: string) {
    const write = this.report(position, false, outcome);
    this.current = undefined;
    this.baseline = undefined;
    return write;
  }
}
