import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  PlayFeedQueue as NativeFeed,
  type FeedPage,
  type FeedStatus,
} from '../../src/core/play-feed';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
type Track = { id: string; title?: string };
const song = (id: string): Track => ({ id, title: id });
const page = (...ids: string[]): FeedPage<Track> => ({
  items: ids.map((id) => ({ id: 'item-' + id, track: song(id) })),
  nextCursor: 'next-page',
  exhausted: false,
});
const settle = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve();
};
describe('mobile play-feed queue', () => {
  afterEach(() => vi.useRealTimers());
  function setup(tracks = [song('current')]) {
    let state = { tracks, index: 0 },
      active = true;
    let status: FeedStatus = { enabled: true, loading: false, error: false };
    const fetch = vi.fn(async (_input: unknown, _signal: AbortSignal) =>
      page('new-1', 'new-2', 'new-3'),
    );
    const flush = vi.fn(async () => {});
    const controller = new NativeFeed<Track>({
      read: () => state,
      apply: (tracks, index) => {
        state = { tracks, index };
      },
      fetch,
      flush,
      active: () => active,
      changed: (value) => {
        status = value;
      },
      uuid: () => 'request-' + Math.random(),
    });
    return {
      controller,
      fetch,
      flush,
      state: () => state,
      status: () => status,
      set: (next: typeof state) => {
        state = next;
      },
      stop: () => {
        active = false;
        controller.cancel();
      },
    };
  }
  it('replaces generated slots and preserves current/manual choices', async () => {
    const current = song('current'),
      pinned = song('manual');
    const f = setup([current, song('old-1'), pinned, song('old-2')]);
    f.controller.restore([
      { index: 1, playFeedItemId: 'old-item-1' },
      { index: 3, playFeedItemId: 'old-item-2' },
    ]);
    await f.controller.refresh();
    expect(f.state().tracks.map((t) => t.id)).toEqual(['current', 'new-1', 'manual', 'new-2']);
    expect(f.state().tracks[0]).toBe(current);
    expect(f.state().tracks[2]).toBe(pinned);
    expect(f.controller.references()).toEqual([
      { index: 1, playFeedItemId: 'item-new-1' },
      { index: 3, playFeedItemId: 'item-new-2' },
    ]);
    expect(f.fetch.mock.calls[0]?.[0]).toMatchObject({
      seedTrackId: 'current',
      queuedTrackIds: ['current', 'manual'],
      pageSize: 2,
      cursor: '',
    });
    f.stop();
  });
  it('debounces operations and flushes listening feedback before requesting', async () => {
    vi.useFakeTimers();
    const f = setup();
    let complete!: () => void;
    f.flush.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          complete = resolve;
        }),
    );
    f.controller.schedule();
    f.controller.schedule();
    f.controller.schedule();
    await vi.advanceTimersByTimeAsync(700);
    expect(f.flush).toHaveBeenCalledTimes(1);
    expect(f.fetch).not.toHaveBeenCalled();
    complete();
    await settle();
    expect(f.fetch).toHaveBeenCalledTimes(1);
    expect(f.state().tracks).toHaveLength(4);
    f.stop();
  });
  it('discards stale replies after an edit or disconnect', async () => {
    vi.useFakeTimers();
    const f = setup();
    let respond!: (p: FeedPage<Track>) => void;
    f.fetch.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          respond = resolve;
        }),
    );
    const pending = f.controller.refresh();
    await settle();
    const manual = song('play-next');
    f.set({ tracks: [song('current'), manual], index: 0 });
    f.controller.schedule();
    respond(page('stale'));
    await pending;
    expect(f.state().tracks.map((t) => t.id)).toEqual(['current', 'play-next']);
    await vi.advanceTimersByTimeAsync(700);
    expect(f.state().tracks[1]).toBe(manual);
    let late!: (p: FeedPage<Track>) => void;
    f.fetch.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          late = resolve;
        }),
    );
    const stopped = f.controller.refresh();
    await settle();
    f.stop();
    late(page('other-account'));
    await stopped;
    expect(f.state().tracks.some((t) => t.id === 'other-account')).toBe(false);
  });
  it('paginates and suppresses duplicates and dismissed entries', async () => {
    const f = setup();
    await f.controller.refresh();
    const removed = f.state().tracks[1];
    f.controller.dismiss(removed);
    f.set({ tracks: f.state().tracks.filter((t) => t !== removed), index: 1 });
    f.fetch.mockResolvedValueOnce(page('current', 'new-1', 'new-2', 'later'));
    await f.controller.refresh(true);
    expect(f.fetch.mock.calls.at(-1)?.[0]).toMatchObject({
      cursor: 'next-page',
      excludedTrackIds: ['new-1', 'current'],
    });
    expect(f.state().tracks.map((t) => t.id)).toEqual(['current', 'new-2', 'new-3', 'later']);
    f.stop();
  });
  it('waits until one upcoming track remains, then fetches only two fresh choices', async () => {
    vi.useFakeTimers();
    const f = setup([song('current'), song('manual-1'), song('manual-2')]);
    f.controller.schedule();
    f.controller.maintain();
    await vi.advanceTimersByTimeAsync(1000);
    expect(f.fetch).not.toHaveBeenCalled();
    f.set({ tracks: f.state().tracks, index: 1 });
    f.controller.maintain();
    await vi.advanceTimersByTimeAsync(700);
    expect(f.fetch).toHaveBeenCalledTimes(1);
    expect(f.fetch.mock.calls[0]?.[0]).toMatchObject({ pageSize: 2, seedTrackId: 'manual-1' });
    expect(f.state().tracks.map((t) => t.id)).toEqual([
      'current',
      'manual-1',
      'manual-2',
      'new-1',
      'new-2',
    ]);
    f.controller.maintain();
    await vi.advanceTimersByTimeAsync(1000);
    expect(f.fetch).toHaveBeenCalledTimes(1);
    f.set({ tracks: f.state().tracks, index: 3 });
    f.fetch.mockResolvedValueOnce(page('fresh-1', 'fresh-2'));
    f.controller.maintain();
    await vi.advanceTimersByTimeAsync(700);
    expect(f.fetch).toHaveBeenCalledTimes(2);
    expect(f.fetch.mock.calls[1]?.[0]).toMatchObject({
      pageSize: 2,
      cursor: 'next-page',
      seedTrackId: 'new-1',
    });
    expect(
      f
        .state()
        .tracks.slice(4)
        .map((t) => t.id),
    ).toEqual(['new-2', 'fresh-1', 'fresh-2']);
    f.stop();
  });
  it('backs off failed background refills instead of requesting on every audio tick', async () => {
    vi.useFakeTimers();
    const f = setup();
    f.fetch.mockRejectedValue(new Error('offline'));
    await f.controller.refresh(true);
    for (let i = 0; i < 10; i++) f.controller.maintain();
    await vi.advanceTimersByTimeAsync(1000);
    expect(f.fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(15000);
    f.controller.maintain();
    await vi.advanceTimersByTimeAsync(700);
    expect(f.fetch).toHaveBeenCalledTimes(2);
    f.stop();
  });
  it('disables generated future entries without losing current/manual tracks', async () => {
    const f = setup([song('current'), song('automatic'), song('manual')]);
    f.controller.restore([
      { index: 0, playFeedItemId: 'current-item' },
      { index: 1, playFeedItemId: 'auto-item' },
    ]);
    f.controller.setEnabled(false);
    expect(f.state().tracks.map((t) => t.id)).toEqual(['current', 'manual']);
    expect(f.controller.references()).toEqual([{ index: 0, playFeedItemId: 'current-item' }]);
    await f.controller.refresh();
    expect(f.fetch).not.toHaveBeenCalled();
    f.stop();
  });
  it('retains the playable queue when offline', async () => {
    const f = setup([song('current'), song('automatic'), song('manual')]);
    f.controller.restore([{ index: 1, playFeedItemId: 'old-auto' }]);
    const before = f.state().tracks;
    f.fetch.mockRejectedValueOnce(new Error('offline'));
    await f.controller.refresh();
    expect(f.state().tracks).toBe(before);
    expect(f.status().error).toBe(true);
    f.stop();
  });
  it('transfers attribution through metadata hydration and promotes manual edits', () => {
    const partial = { id: 'partial' },
      f = setup([song('current'), partial]);
    f.controller.restore([{ index: 1, playFeedItemId: 'original-item' }]);
    const detail = song('partial');
    f.controller.transfer(partial, detail);
    f.set({ tracks: [song('current'), detail], index: 0 });
    expect(f.controller.recommendation(detail)).toBe('original-item');
    f.controller.promote(detail);
    expect(f.controller.references()).toEqual([]);
    f.stop();
  });

  it('shrinks legacy generated buffers while preserving the current song and manual choices', () => {
    const f = setup([
      song('current'),
      song('manual'),
      ...Array.from({ length: 10 }, (_, i) => song('old-' + i)),
    ]);
    f.controller.restore(
      Array.from({ length: 10 }, (_, i) => ({ index: i + 2, playFeedItemId: 'old-item-' + i })),
    );
    expect(f.state().tracks.map((t) => t.id)).toEqual(['current', 'manual', 'old-0', 'old-1']);
    expect(f.controller.references()).toHaveLength(2);
    f.stop();
  });
});

it('keeps independently built web and mobile queue policies in sync', () => {
  const shape = (node: ts.Node): unknown => {
    const children: unknown[] = [];
    node.forEachChild((child) => {
      children.push(shape(child));
    });
    return {
      kind: node.kind,
      text: ts.isIdentifier(node) || ts.isLiteralExpression(node) ? node.text : undefined,
      children,
    };
  };
  const parse = (path: string) =>
    shape(
      ts.createSourceFile(
        'play-feed.ts',
        readFileSync(new URL(path, import.meta.url), 'utf8'),
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TS,
      ),
    );
  expect(parse('../../../web/src/lib/play-feed.ts')).toEqual(parse('../../src/core/play-feed.ts'));
});
