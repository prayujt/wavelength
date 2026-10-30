import { setAudioModeAsync, useAudioPlayer } from 'expo-audio';
import type { AppAudio } from './audio-status';
export type { AppAudio } from './audio-status';
export { useAudioPlayerStatus } from './audio-status';

export function useAppAudio(): AppAudio {
  return useAudioPlayer(null, { updateInterval: 500, keepAudioSessionActive: true });
}
export async function configureAudio() {
  await setAudioModeAsync({
    playsInSilentMode: true,
    shouldPlayInBackground: true,
    interruptionMode: 'doNotMix',
  });
}
