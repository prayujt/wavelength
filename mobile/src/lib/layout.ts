import { useSegments } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { usePlayer } from './player';
import { PLAYER_DOCK_HEIGHT, PLAYER_DOCK_GAP } from './theme';

export const hidesPlayerDock = (segments: readonly string[]) =>
  segments.some((segment) =>
    ['player', 'queue', 'feed', 'lyrics', 'playlist-edit'].includes(segment),
  );

// Tabs already reserve their own bar and home-indicator space. Only leave
// extra scrolling room when the floating player actually covers this page.
export function usePageBottomInset() {
  const segments = useSegments(),
    insets = useSafeAreaInsets(),
    player = usePlayer();
  const tabs = segments.includes('(tabs)');
  const dock = !!player.track && !hidesPlayerDock(segments);
  return (
    16 +
    (tabs ? 0 : insets.bottom) +
    (dock ? PLAYER_DOCK_HEIGHT + (tabs ? PLAYER_DOCK_GAP : 12) : 0)
  );
}
