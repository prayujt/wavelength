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
  private enabled = true;
  private loading = false;
  private error = false;
  constructor(private deps: FeedDependencies<T>) {}
  private status() {
    this.deps.changed({ enabled: this.enabled, loading: this.loading, error: this.error });
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
    if (this.excluded.size > 200) this.excluded.delete(this.excluded.values().next().value!);
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
    this.status();
  }
  setEnabled(enabled: boolean) {
    this.enabled = enabled;
    this.cancel();
    if (!enabled) {
      const { tracks, index } = this.deps.read();
      const kept = tracks.filter((track, at) => at <= index || !this.generated.has(track));
      const present = new Set(kept);
      for (const track of this.generated.keys())
        if (!present.has(track)) this.generated.delete(track);
      if (kept.length !== tracks.length) this.deps.apply(kept, index);
    } else this.schedule();
    this.status();
  }
  schedule() {
    this.cancel();
    this.exhausted = false;
    if (!this.enabled || !this.deps.active() || !this.deps.read().tracks.length) return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.refresh();
    }, 650);
  }
  maintain() {
    if (this.timer || this.loading || this.exhausted || !this.enabled) return;
    const { tracks, index } = this.deps.read();
    if (tracks.slice(index + 1).filter((track) => this.generated.has(track)).length >= 3) return;
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
        .filter((track, at) => at === 0 || append || !this.generated.has(track));
      const page = await this.deps.fetch(
        {
          requestId: this.deps.uuid(),
          cursor: append ? this.cursor : '',
          pageSize: 10,
          seedTrackId: current.id,
          queuedTrackIds: upcoming.map((track) => track.id).slice(0, 1000),
          excludedTrackIds: [...this.excluded],
        },
        controller.signal,
      );
      if (ticket !== this.revision || !this.deps.active()) return;
      const blocked = new Set([...upcoming.map((track) => track.id), ...this.excluded]);
      const incoming: T[] = [];
      const ids = new Map<T, string>();
      for (const item of page.items) {
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
      this.cursor = page.nextCursor;
      this.exhausted = page.exhausted;
      this.deps.apply(next, index);
    } catch {
      // Offline/provider failures retain the playable queue and manual choices.
      if (ticket === this.revision && !controller.signal.aborted) this.error = true;
    } finally {
      if (ticket === this.revision) {
        this.loading = false;
        this.request = undefined;
        this.status();
      }
    }
  }
}
