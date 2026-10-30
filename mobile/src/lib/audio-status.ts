import { useEffect, useState } from 'react';
import type { AudioMetadata } from 'expo-audio';
import type { AudioSource, NativeStatus } from '../core/playback';

export interface AppAudio {
  readonly currentTime: number;
  readonly duration: number;
  readonly currentStatus: NativeStatus;
  volume: number;
  play(): void;
  pause(): void;
  replace(source: AudioSource | null): void;
  seekTo(seconds: number): Promise<void>;
  setActiveForLockScreen(active: boolean, metadata?: AudioMetadata): void;
  updateLockScreenMetadata(metadata: AudioMetadata): void;
  clearLockScreenControls(): void;
  addListener(
    event: 'playbackStatusUpdate',
    listener: (status: NativeStatus) => void,
  ): { remove(): void };
}

export function useAudioPlayerStatus(audio: AppAudio) {
  const [status, setStatus] = useState(() => audio.currentStatus);
  useEffect(() => {
    const subscription = audio.addListener('playbackStatusUpdate', setStatus);
    return () => subscription.remove();
  }, [audio]);
  return status;
}
