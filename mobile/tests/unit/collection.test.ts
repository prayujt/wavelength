import { describe, expect, it } from 'vitest';
import { clock, moveItem, nextIndex, releaseAvailability, shuffled } from '../../src/core/queue';
import { parseLyrics } from '../../src/core/lyrics';
import { ListeningReporter } from '../../src/core/listening';
describe('collection identity and queue order', () => {
  it('requires every exact reference before marking an album as saved', () => {
    const refs = [
        { provider: 'itunes', id: 'one' },
        { provider: 'itunes', id: 'two' },
      ],
      saved = new Map([['itunes:one', 'local-one']]);
    expect(releaseAvailability(refs, saved)).toMatchObject({ available: 1, complete: false });
    saved.set('itunes:two', 'local-two');
    expect(releaseAvailability(refs, saved).complete).toBe(true);
    expect(releaseAvailability([], saved).complete).toBe(false);
  });
  it('reorders duplicates by position without deleting another occurrence', () => {
    expect(moveItem(['a', 'b', 'a'], 2, 0)).toEqual(['a', 'a', 'b']);
    expect(moveItem(['a', 'b'], 0, -1)).toEqual(['a', 'b']);
  });
  it('shuffles a copy, preserving the complete multiset', () => {
    const input = ['a', 'a', 'b', 'c'];
    expect(shuffled(input, () => 0.2).sort()).toEqual([...input].sort());
    expect(input).toEqual(['a', 'a', 'b', 'c']);
  });
  it('distinguishes manual next from repeat-one at the natural end', () => {
    expect(nextIndex(3, 1, 'one', true)).toBe(1);
    expect(nextIndex(3, 1, 'one')).toBe(2);
    expect(nextIndex(3, 2, 'off', true)).toBeNull();
    expect(nextIndex(3, 2, 'all', true)).toBe(0);
  });
  it('formats finite, missing and negative times safely', () => {
    expect(clock(185.6)).toBe('3:05');
    expect(clock(NaN)).toBe('0:00');
    expect(clock(-3)).toBe('0:00');
  });
});
describe('saved lyrics', () => {
  it('handles fractional timestamps, repeated timestamps and metadata', () => {
    expect(parseLyrics('[ar:Artist]\n[00:02.5][00:04.250]Hello\n[00:01.003]First')).toEqual([
      { text: 'First', time: 1.003 },
      { text: 'Hello', time: 2.5 },
      { text: 'Hello', time: 4.25 },
    ]);
  });
  it('retains untimed lyrics as text', () =>
    expect(parseLyrics('A verse\n\nAnother line')).toEqual([
      { text: 'A verse' },
      { text: 'Another line' },
    ]));
});
describe('actual listening time', () => {
  it('excludes pauses, buffering and seek jumps, serializing cumulative reports', async () => {
    const events: { kind: string; listened?: bigint }[] = [];
    let now = 100000;
    const report = new ListeningReporter(
      {
        async startListeningSession() {
          events.push({ kind: 'start' });
        },
        async reportListeningProgress(p) {
          events.push({ kind: 'progress', listened: p.listenedMs });
        },
        async finishListeningSession(p) {
          events.push({ kind: 'finish', listened: p.listenedMs });
        },
      },
      () => 'test-session-123',
      () => now,
    );
    report.start('track', 'direct');
    report.sample(0, true);
    now += 1000;
    report.sample(1, true);
    now += 10000;
    report.sample(1, false);
    report.seek(90);
    now += 1000;
    report.sample(91, true);
    now += 1000;
    report.sample(160, true);
    await report.finish(160, 'stopped');
    expect(events[0].kind).toBe('start');
    expect(events.at(-1)).toEqual({ kind: 'finish', listened: 2000n });
  });
});
