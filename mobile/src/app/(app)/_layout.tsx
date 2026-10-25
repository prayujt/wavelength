import { Stack } from 'expo-router';
import { View } from 'react-native';
import { MediaMenuProvider } from '../../components/media';
import { PlayerDock } from '../../components/player-dock';
import { useLibraryPulse } from '../../lib/queries';
import { color } from '../../lib/theme';
export default function AppLayout() {
  useLibraryPulse();
  return (
    <MediaMenuProvider>
      <View style={{ flex: 1, backgroundColor: color.bg }}>
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: color.bg },
            animation: 'slide_from_right',
          }}
        >
          <Stack.Screen name="(tabs)" />
          <Stack.Screen
            name="player"
            options={{ presentation: 'fullScreenModal', animation: 'slide_from_bottom' }}
          />
          <Stack.Screen
            name="queue"
            options={{ presentation: 'modal', animation: 'slide_from_bottom' }}
          />
          <Stack.Screen
            name="lyrics"
            options={{ presentation: 'modal', animation: 'slide_from_bottom' }}
          />
          <Stack.Screen
            name="playlist-edit"
            options={{ presentation: 'modal', animation: 'slide_from_bottom' }}
          />
        </Stack>
        <PlayerDock />
      </View>
    </MediaMenuProvider>
  );
}
