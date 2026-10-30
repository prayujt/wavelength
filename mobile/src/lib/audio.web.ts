import { useEffect, useState } from 'react';
import { Asset } from 'expo-asset';
import { WebAudio } from '../core/web-audio';
import type { AppAudio } from './audio-status';
export type { AppAudio } from './audio-status';
export { useAudioPlayerStatus } from './audio-status';

export function useAppAudio(): AppAudio {
  const [audio] = useState(
    () =>
      new WebAudio(new Audio(), (source) =>
        typeof source === 'string'
          ? source
          : typeof source === 'number'
            ? Asset.fromModule(source).uri
            : source.uri,
      ),
  );
  useEffect(() => () => audio.release(), [audio]);
  return audio;
}
export async function configureAudio() {
  // WebAudio claims a playback session only when music starts.
}
