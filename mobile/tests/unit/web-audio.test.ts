import { afterEach, describe, expect, it, vi } from 'vitest';
import { WebAudio } from '../../src/core/web-audio';

class Media extends EventTarget {
  src = '';
  currentTime = 0;
  duration = 120;
  paused = true;
  ended = false;
  readyState = 4;
  volume = 1;
  playbackRate = 1;
  error = null;
  preload = '';
  play = vi.fn(async () => {
    this.paused = false;
    this.dispatchEvent(new Event('play'));
  });
  pause() {
    this.paused = true;
    this.dispatchEvent(new Event('pause'));
  }
  load() {
    this.currentTime = 0;
    this.ended = false;
  }
  setAttribute() {}
  removeAttribute() {
    this.src = '';
  }
}
class Session extends EventTarget {
  type = 'auto';
  state = 'active';
  change(state: string) {
    this.state = state;
    this.dispatchEvent(new Event('statechange'));
  }
}
afterEach(() => vi.unstubAllGlobals());
function setup() {
  const session = new Session();
  const actions = new Map<string, (() => void) | null>();
  vi.stubGlobal('navigator', {
    audioSession: session,
    mediaSession: {
      setActionHandler: (name: string, action: (() => void) | null) => actions.set(name, action),
      setPositionState() {},
      playbackState: 'none',
      metadata: null,
    },
  });
  const media = new Media();
  const player = new WebAudio(media as unknown as HTMLAudioElement, (source) =>
    typeof source === 'string' ? source : typeof source === 'number' ? 'preview' : source.uri,
  );
  return { media, player, session, actions };
}
describe('persistent PWA audio', () => {
  it('notifies ended immediately without waiting for timeupdate and reuses the same media element', () => {
    const { player, media } = setup();
    const statuses: boolean[] = [];
    player.addListener('playbackStatusUpdate', (status) => statuses.push(status.didJustFinish));
    player.replace({ uri: 'first' });
    player.play();
    media.currentTime = 120;
    media.ended = true;
    media.paused = true;
    media.dispatchEvent(new Event('ended'));
    expect(statuses.at(-1)).toBe(true);
    player.replace({ uri: 'second' });
    player.play();
    expect(media.src).toBe('second');
    expect(media.play).toHaveBeenCalledTimes(2);
    expect(player.currentStatus.didJustFinish).toBe(false);
    player.release();
  });
  it('resumes a temporary system interruption only when focus returns and the user still wants playback', () => {
    const { player, media, session, actions } = setup();
    player.setActiveForLockScreen(true);
    player.replace({ uri: 'music' });
    player.play();
    expect(session.type).toBe('playback');
    session.change('interrupted');
    media.pause();
    expect(media.play).toHaveBeenCalledTimes(1);
    session.change('active');
    expect(media.play).toHaveBeenCalledTimes(2);
    session.change('interrupted');
    media.pause();
    actions.get('pause')!();
    session.change('active');
    expect(media.play).toHaveBeenCalledTimes(2);
    player.release();
    expect(session.type).toBe('auto');
  });
  it('handles rejected play promises and ignores errors from a superseded source', async () => {
    const { player, media } = setup();
    let reject!: (error: Error) => void;
    media.play.mockImplementationOnce(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        }),
    );
    player.replace({ uri: 'old' });
    player.play();
    player.replace({ uri: 'new' });
    player.play();
    reject(new Error('old failure'));
    await Promise.resolve();
    expect(player.currentStatus.error).toBeNull();
    media.play.mockRejectedValueOnce(new Error('Not allowed'));
    player.play();
    await Promise.resolve();
    expect(player.currentStatus.error).toContain('Tap play');
    player.release();
  });
});
