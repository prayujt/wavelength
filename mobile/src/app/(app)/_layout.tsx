import { Stack } from 'expo-router';
import { View } from 'react-native';
import { MediaMenuProvider } from '../../components/media';
import { PlayerDock } from '../../components/player-dock';
import { useReducedMotion } from '../../components/ui';
import { usePlayer } from '../../lib/player';
import { useLibraryPulse } from '../../lib/queries';
import { color } from '../../lib/theme';
export default function AppLayout() {
  const player = usePlayer();
  const reducedMotion = useReducedMotion();
  useLibraryPulse(player.refreshRecommendations);
  return (
    <MediaMenuProvider>
      <View style={{ flex: 1, backgroundColor: color.bg }}>
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: color.bg },
            animation: reducedMotion ? 'none' : 'fade',
          }}
        >
          <Stack.Screen name="(tabs)" />
          <Stack.Screen
            name="player"
            options={{
              presentation: 'fullScreenModal',
              animation: reducedMotion ? 'none' : 'slide_from_bottom',
            }}
          />
          <Stack.Screen
            name="queue"
            options={{
              presentation: 'modal',
              animation: reducedMotion ? 'none' : 'slide_from_bottom',
            }}
          />
          <Stack.Screen
            name="lyrics"
            options={{
              presentation: 'modal',
              animation: reducedMotion ? 'none' : 'slide_from_bottom',
            }}
          />
          <Stack.Screen
            name="playlist-edit"
            options={{
              presentation: 'modal',
              animation: reducedMotion ? 'none' : 'slide_from_bottom',
            }}
          />
        </Stack>
        <PlayerDock />
      </View>
    </MediaMenuProvider>
  );
}
