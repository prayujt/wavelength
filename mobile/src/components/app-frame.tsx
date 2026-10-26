import React, { useMemo } from 'react';
import { Platform, View } from 'react-native';
import { SafeAreaInsetsContext, useSafeAreaInsets } from 'react-native-safe-area-context';
import { usePlayer } from '../lib/player';

export function AppFrame({ children }: { children: React.ReactNode }) {
  const insets = useSafeAreaInsets();
  const player = usePlayer();
  const headerVisible = player.playFeedEnabled || (player.playFeedError && !!player.error);
  const handlesTop = Platform.OS === 'web' || headerVisible;
  const contentInsets = useMemo(
    () => ({ ...insets, top: handlesTop ? 0 : insets.top }),
    [insets, handlesTop],
  );
  return (
    <View style={{ flex: 1, minHeight: 0, paddingTop: handlesTop ? insets.top : 0 }}>
      <SafeAreaInsetsContext.Provider value={contentInsets}>
        {children}
      </SafeAreaInsetsContext.Provider>
    </View>
  );
}
