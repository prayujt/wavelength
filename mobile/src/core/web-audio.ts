import type { AudioMetadata } from 'expo-audio';
import type { AppAudio } from '../lib/audio-status';
import type { AudioSource, NativeStatus } from './playback';

type BrowserAudioSession = EventTarget & {
  type: string;
  state: 'inactive' | 'active' | 'interrupted';
};

/** One user-activated media element for the lifetime of the PWA player. */
export class WebAudio implements AppAudio {
  private listeners = new Set<(status: NativeStatus) => void>();
  private version = 0;
  private wanted = false;
  private interrupted = false;
  private error = '';
  private lockScreen = false;
  private previousSessionType?: string;
  private session = (
    globalThis.navigator as (Navigator & { audioSession?: BrowserAudioSession }) | undefined
  )?.audioSession;
  private mediaSession = globalThis.navigator?.mediaSession;
  constructor(
    private media: HTMLAudioElement,
    private uri: (source: AudioSource) => string,
  ) {
    media.preload = 'auto';
    media.setAttribute('playsinline', '');
    for (const event of [
      'play',
      'pause',
      'playing',
      'waiting',
      'loadedmetadata',
      'loadeddata',
      'durationchange',
      'seeked',
      'timeupdate',
      'error',
      'ended',
    ])
      media.addEventListener(event, this.changed);
    this.session?.addEventListener('statechange', this.sessionChanged);
  }
  get currentTime() {
    return this.media.currentTime;
  }
  get duration() {
    return Number.isFinite(this.media.duration) ? this.media.duration : 0;
  }
  get volume() {
    return this.media.volume;
  }
  set volume(value: number) {
    this.media.volume = value;
  }
  get currentStatus(): NativeStatus {
    return {
      isLoaded: this.media.readyState >= 1,
      currentTime: this.currentTime,
      duration: this.duration,
      playing: !this.media.paused && !this.media.ended,
      isBuffering: !this.media.paused && this.media.readyState < 3,
      didJustFinish: this.media.ended,
      error: this.error || (this.media.error ? 'Audio playback failed.' : null),
    };
  }
  addListener(_event: 'playbackStatusUpdate', listener: (status: NativeStatus) => void) {
    this.listeners.add(listener);
    return {
      remove: () => {
        this.listeners.delete(listener);
      },
    };
  }
  private changed = () => {
    const status = this.currentStatus;
    this.updateSession(status);
    // In particular, ended is synchronous and never depends on a throttled
    // timeupdate, React effect, interval, or a foreground visibility event.
    for (const listener of [...this.listeners]) listener(status);
  };
  play() {
    this.wanted = true;
    if (this.session) {
      try {
        this.previousSessionType ??= this.session.type;
        this.session.type = 'playback';
      } catch {
        /* Browsers without a writable audio session still play media. */
      }
    }
    const version = this.version;
    this.error = '';
    void this.media.play().catch((error: unknown) => {
      // A newer selection or an explicit pause legitimately aborts play().
      if (
        version !== this.version ||
        !this.wanted ||
        (error instanceof Error && error.name === 'AbortError')
      )
        return;
      this.error = 'Playback was interrupted. Tap play to resume.';
      this.changed();
    });
  }
  pause() {
    this.wanted = false;
    this.media.pause();
  }
  replace(source: AudioSource | null) {
    ++this.version;
    this.wanted = false;
    this.error = '';
    this.media.pause();
    if (source) this.media.src = this.uri(source);
    else this.media.removeAttribute('src');
    this.media.load();
  }
  async seekTo(seconds: number) {
    this.media.currentTime = Math.max(0, Math.min(seconds, this.duration || seconds));
  }
  private sessionChanged = () => {
    if (this.session?.state === 'interrupted') this.interrupted = this.wanted;
    else if (this.session?.state === 'active') {
      const resume = this.interrupted && this.wanted;
      this.interrupted = false;
      if (resume && this.media.paused && !this.media.ended) this.play();
    }
  };
  setActiveForLockScreen(active: boolean, metadata?: AudioMetadata) {
    this.lockScreen = active;
    if (!active) return this.clearLockScreenControls();
    if (metadata) this.updateLockScreenMetadata(metadata);
    this.action('play', () => this.play());
    this.action('pause', () => this.pause());
    this.action('seekto', (details) => {
      if (details.seekTime !== undefined) void this.seekTo(details.seekTime);
    });
    this.action('seekbackward', (details) => {
      void this.seekTo(this.currentTime - (details.seekOffset ?? 10));
    });
    this.action('seekforward', (details) => {
      void this.seekTo(this.currentTime + (details.seekOffset ?? 10));
    });
    this.updateSession(this.currentStatus);
  }
  updateLockScreenMetadata(metadata: AudioMetadata) {
    if (this.lockScreen && this.mediaSession && typeof MediaMetadata !== 'undefined') {
      this.mediaSession.metadata = new MediaMetadata({
        title: metadata.title,
        artist: metadata.artist,
        album: metadata.albumTitle,
        artwork: metadata.artworkUrl ? [{ src: metadata.artworkUrl }] : [],
      });
    }
  }
  private action(action: MediaSessionAction, handler: MediaSessionActionHandler | null) {
    try {
      this.mediaSession?.setActionHandler(action, handler);
    } catch {
      /* Optional media action. */
    }
  }
  private updateSession(status: NativeStatus) {
    if (!this.lockScreen || !this.mediaSession) return;
    this.mediaSession.playbackState = status.playing ? 'playing' : 'paused';
    if (this.duration > 0) {
      try {
        this.mediaSession.setPositionState({
          duration: this.duration,
          position: Math.min(this.duration, Math.max(0, this.currentTime)),
          playbackRate: this.media.playbackRate || 1,
        });
      } catch {
        /* Optional position API. */
      }
    }
  }
  clearLockScreenControls() {
    this.lockScreen = false;
    if (!this.mediaSession) return;
    this.mediaSession.metadata = null;
    this.mediaSession.playbackState = 'none';
    for (const action of [
      'play',
      'pause',
      'seekto',
      'seekbackward',
      'seekforward',
      'nexttrack',
      'previoustrack',
    ] as const)
      this.action(action, null);
    try {
      this.mediaSession.setPositionState();
    } catch {
      /* Optional position API. */
    }
  }
  release() {
    this.pause();
    this.replace(null);
    this.clearLockScreenControls();
    for (const event of [
      'play',
      'pause',
      'playing',
      'waiting',
      'loadedmetadata',
      'loadeddata',
      'durationchange',
      'seeked',
      'timeupdate',
      'error',
      'ended',
    ])
      this.media.removeEventListener(event, this.changed);
    this.session?.removeEventListener('statechange', this.sessionChanged);
    if (
      this.session &&
      this.previousSessionType !== undefined &&
      this.session.type === 'playback'
    ) {
      try {
        this.session.type = this.previousSessionType;
      } catch {
        /* Optional session API. */
      }
    }
    this.listeners.clear();
  }
}
