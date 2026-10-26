// Portable queue policy; kept local to each independently buildable UI.
export type FeedReference = { index: number; playFeedItemId: string };
export type FeedRequest = {
  requestId: string;
  cursor?: string;
  pageSize: number;
  seedTrackId: string;
  queuedTrackIds: string[];
  excludedTrackIds: string[];
};
export type FeedPage<T> = {
  items: { id: string; track?: T }[];
  nextCursor: string;
  exhausted: boolean;
};
export type FeedStatus = { enabled: boolean; loading: boolean; error: boolean };
export type FeedDependencies<T> = {
  read(): { tracks: T[]; index: number };
  apply(tracks: T[], index: number): void;
  fetch(input: FeedRequest, signal: AbortSignal): Promise<FeedPage<T>>;
  flush(): Promise<unknown>;
  active(): boolean;
  changed(status: FeedStatus): void;
  uuid(): string;
};

/** Owns only unplayed generated entries. Manual entries retain their queue slots. */
export class PlayFeedQueue<T extends { id: string }> {
  private generated = new Map<T, string>();
  private excluded = new Set<string>();
  private revision = 0;
  private timer?: ReturnType<typeof setTimeout>;
  private request?: AbortController;
  private cursor = '';
  private exhausted = false;
  private enabled = false;
  private loading = false;
  private error = false;
  private retryAt = 0;
  constructor(private deps: FeedDependencies<T>) {}
  private status() {
    this.deps.changed({
      enabled: this.enabled,
      loading: this.loading,
      error: this.error,
    });
  }
  recommendation(track: T) {
    return this.generated.get(track) || '';
  }
  references(): FeedReference[] {
    return this.deps.read().tracks.flatMap((track, index) => {
      const id = this.generated.get(track);
      return id ? [{ index, playFeedItemId: id }] : [];
    });
  }
  restore(items: FeedReference[], enabled = true) {
    this.cancel();
    this.generated.clear();
    const { tracks } = this.deps.read();
    for (const item of items)
      if (tracks[item.index] && item.playFeedItemId)
        this.generated.set(tracks[item.index], item.playFeedItemId);
    this.enabled = enabled;
    this.limitUpcoming();
    this.status();
  }
  transfer(previous: T, next: T) {
    const id = this.generated.get(previous);
    if (id) {
      this.generated.delete(previous);
      this.generated.set(next, id);
    }
  }
  promote(track: T) {
    this.generated.delete(track);
  }
  dismiss(track: T) {
    this.generated.delete(track);
    this.avoid(track.id);
  }
  avoid(id: string) {
    this.excluded.add(id);
    if (this.excluded.size > 200)
      this.excluded.delete(this.excluded.values().next().value!);
  }
  cancel() {
    ++this.revision;
    clearTimeout(this.timer);
    this.timer = undefined;
    this.request?.abort();
    this.request = undefined;
    this.loading = false;
    this.status();
  }
  reset() {
    this.cancel();
    this.generated.clear();
    this.excluded.clear();
    this.cursor = '';
    this.exhausted = false;
    this.error = false;
    this.retryAt = 0;
    this.status();
  }
  setEnabled(enabled: boolean) {
    this.enabled = enabled;
    this.cancel();
    if (!enabled) {
      this.error = false;
      const { tracks, index } = this.deps.read();
      const kept = tracks.filter(
        (track, at) => at <= index || !this.generated.has(track),
      );
      const present = new Set(kept);
      for (const track of this.generated.keys())
        if (!present.has(track)) this.generated.delete(track);
      if (kept.length !== tracks.length) this.deps.apply(kept, index);
    } else this.schedule();
    this.status();
  }
  async start() {
    if (!this.deps.active()) return false;
    const previous = {
      generated: this.generated,
      excluded: this.excluded,
      cursor: this.cursor,
      exhausted: this.exhausted,
      enabled: this.enabled,
    };
    this.generated = new Map(this.generated);
    this.excluded = new Set(this.excluded);
    this.reset();
    this.enabled = true;
    const ticket = this.revision;
    const controller = new AbortController();
    this.request = controller;
    this.loading = true;
    this.status();
    try {
      await this.deps.flush();
      if (ticket !== this.revision || !this.deps.active()) return false;
      const page = await this.deps.fetch(
        {
          requestId: this.deps.uuid(),
          cursor: '',
          pageSize: 3,
          seedTrackId: '',
          queuedTrackIds: [],
          excludedTrackIds: [],
        },
        controller.signal,
      );
      if (ticket !== this.revision || !this.deps.active()) return false;
      const tracks: T[] = [];
      const seen = new Set<string>();
      for (const item of page.items) {
        if (!item.track || !item.id || seen.has(item.track.id)) continue;
        const track = { ...item.track };
        tracks.push(track);
        seen.add(track.id);
        this.generated.set(track, item.id);
        if (tracks.length === 3) break;
      }
      if (!tracks.length)
        throw new Error('No songs are available for your play feed yet.');
      this.cursor = page.nextCursor;
      this.exhausted = page.exhausted;
      this.deps.apply(tracks, 0);
      return true;
    } catch (error) {
      if (ticket !== this.revision || controller.signal.aborted) return false;
      this.generated = previous.generated;
      this.excluded = previous.excluded;
      this.cursor = previous.cursor;
      this.exhausted = previous.exhausted;
      this.enabled = previous.enabled;
      this.error = true;
      throw error;
    } finally {
      if (ticket === this.revision) {
        this.loading = false;
        this.request = undefined;
        this.status();
      }
    }
  }
  private limitUpcoming() {
    const { tracks, index } = this.deps.read();
    let slots = Math.max(
      0,
      3 -
        tracks.slice(index + 1).filter((track) => !this.generated.has(track))
          .length,
    );
    const kept = tracks.filter(
      (track, at) => at <= index || !this.generated.has(track) || slots-- > 0,
    );
    if (kept.length !== tracks.length) {
      const present = new Set(kept);
      for (const track of this.generated.keys())
        if (!present.has(track)) this.generated.delete(track);
      this.deps.apply(kept, index);
    }
  }
  schedule() {
    this.cancel();
    this.limitUpcoming();
    this.exhausted = false;
    const { tracks, index } = this.deps.read();
    if (
      !this.enabled ||
      !this.deps.active() ||
      !tracks[index] ||
      tracks.length - index - 1 > 1
    )
      return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.refresh(true);
    }, 650);
  }
  maintain() {
    if (
      this.timer ||
      this.loading ||
      this.exhausted ||
      !this.enabled ||
      !this.deps.active() ||
      Date.now() < this.retryAt
    )
      return;
    const { tracks, index } = this.deps.read();
    if (!tracks[index] || tracks.length - index - 1 > 1) return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.refresh(true);
    }, 650);
  }
  async refresh(append = false) {
    this.cancel();
    if (!this.enabled || !this.deps.active()) return;
    const ticket = this.revision;
    const controller = new AbortController();
    this.request = controller;
    this.loading = true;
    this.error = false;
    this.retryAt = 0;
    this.status();
    try {
      // Finish/progress writes must be visible before reading recommendations.
      await this.deps.flush();
      if (ticket !== this.revision || !this.deps.active()) return;
      const { tracks, index } = this.deps.read();
      const current = tracks[index];
      if (!current) return;
      const upcoming = tracks
        .slice(index)
        .filter(
          (track, at) => at === 0 || append || !this.generated.has(track),
        );
      // Keep at most three upcoming choices. Refill only when one is left,
      // preserving the next song rather than swapping it under the listener.
      const ahead = upcoming.length - 1;
      const pageSize = 3 - ahead;
      if (pageSize < 2) return;
      const page = await this.deps.fetch(
        {
          requestId: this.deps.uuid(),
          cursor: append ? this.cursor : '',
          pageSize,
          seedTrackId: current.id,
          queuedTrackIds: upcoming.map((track) => track.id).slice(0, 1000),
          excludedTrackIds: [
            ...new Set([
              ...this.excluded,
              ...tracks.slice(0, index).map((track) => track.id),
            ]),
          ].slice(-1000),
        },
        controller.signal,
      );
      if (ticket !== this.revision || !this.deps.active()) return;
      const blocked = new Set([
        ...upcoming.map((track) => track.id),
        ...tracks.slice(0, index).map((track) => track.id),
        ...this.excluded,
      ]);
      const incoming: T[] = [];
      const ids = new Map<T, string>();
      for (const item of page.items) {
        if (incoming.length >= pageSize) break;
        if (!item.track || !item.id || blocked.has(item.track.id)) continue;
        const track = { ...item.track };
        blocked.add(track.id);
        incoming.push(track);
        ids.set(track, item.id);
      }
      const next = tracks.slice(0, index + 1);
      const rest = [...incoming];
      for (const track of tracks.slice(index + 1)) {
        if (append || !this.generated.has(track)) next.push(track);
        else {
          this.generated.delete(track);
          const replacement = rest.shift();
          if (replacement) next.push(replacement);
        }
      }
      next.push(...rest);
      for (const [track, id] of ids) this.generated.set(track, id);
      const present = new Set(next);
      for (const track of this.generated.keys())
        if (!present.has(track)) this.generated.delete(track);
      this.retryAt = 0;
      this.cursor = page.nextCursor;
      this.exhausted = page.exhausted;
      this.deps.apply(next, index);
    } catch {
      // Offline/provider failures retain the playable queue and manual choices.
      if (ticket === this.revision && !controller.signal.aborted) {
        this.error = true;
        this.retryAt = Date.now() + 15000;
      }
    } finally {
      if (ticket === this.revision) {
        this.loading = false;
        this.request = undefined;
        this.status();
      }
    }
  }
}
