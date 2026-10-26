import React, { useMemo } from 'react';
import { Platform, View } from 'react-native';
import { SafeAreaInsetsContext, useSafeAreaInsets } from 'react-native-safe-area-context';

export function AppFrame({ children }: { children: React.ReactNode }) {
  const insets = useSafeAreaInsets();
  const contentInsets = useMemo(() => ({ ...insets, top: 0 }), [insets]);
  if (Platform.OS !== 'web') return <View style={{ flex: 1 }}>{children}</View>;
  return (
    <View style={{ flex: 1, minHeight: 0, paddingTop: insets.top }}>
      <SafeAreaInsetsContext.Provider value={contentInsets}>
        {children}
      </SafeAreaInsetsContext.Provider>
    </View>
  );
}
