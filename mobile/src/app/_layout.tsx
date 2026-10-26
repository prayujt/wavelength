import React, { useEffect, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { Stack, type ErrorBoundaryProps } from 'expo-router';
import { useFonts } from 'expo-font';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { DMSans_400Regular } from '@expo-google-fonts/dm-sans/400Regular';
import { DMSans_500Medium } from '@expo-google-fonts/dm-sans/500Medium';
import { DMSans_700Bold } from '@expo-google-fonts/dm-sans/700Bold';
import { Manrope_700Bold } from '@expo-google-fonts/manrope/700Bold';
import { Manrope_800ExtraBold } from '@expo-google-fonts/manrope/800ExtraBold';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { SessionProvider, useSession } from '../lib/session';
import { PlayerProvider } from '../lib/player';
import { FeedbackProvider } from '../components/feedback';
import { Button, MotionProvider, Txt, WaveMark } from '../components/ui';
import { color } from '../lib/theme';
import { DeviceProvider, DeviceStatus } from '../components/device';
import { AppFrame } from '../components/app-frame';
import { useAppViewport } from '../lib/viewport';

void SplashScreen.preventAutoHideAsync().catch(() => {});
export const unstable_settings = { anchor: 'index' };

function Navigation() {
  const session = useSession();
  if (!session.ready)
    return (
      <View
        style={{
          flex: 1,
          backgroundColor: color.bg,
          alignItems: 'center',
          justifyContent: 'center',
          gap: 24,
        }}
      >
        <WaveMark size={48} />
        <ActivityIndicator color={color.accent} />
      </View>
    );
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        contentStyle: { backgroundColor: color.bg },
        animation: 'slide_from_right',
      }}
    >
      <Stack.Screen name="index" />
      <Stack.Screen name="welcome" />
      <Stack.Screen name="oauth/callback" />
      <Stack.Protected guard={!!session.grant || !!session.account || session.preview}>
        <Stack.Screen name="(app)" />
      </Stack.Protected>
    </Stack>
  );
}
export default function RootLayout() {
  useAppViewport();
  const [noticeHeight, setNoticeHeight] = useState(0);
  const [loaded, error] = useFonts({
    DMSans_400Regular,
    DMSans_500Medium,
    DMSans_700Bold,
    Manrope_700Bold,
    Manrope_800ExtraBold,
  });
  useEffect(() => {
    if (loaded || error) void SplashScreen.hideAsync();
  }, [loaded, error]);
  if (!loaded && !error) return null;
  return (
    <SafeAreaProvider>
      <MotionProvider>
        <FeedbackProvider noticeHeight={noticeHeight}>
          <SessionProvider>
            <PlayerProvider>
              <DeviceProvider>
                <StatusBar style="light" />
                <AppFrame>
                  <View onLayout={(event) => setNoticeHeight(event.nativeEvent.layout.height)}>
                    <DeviceStatus />
                  </View>
                  <Navigation />
                </AppFrame>
              </DeviceProvider>
            </PlayerProvider>
          </SessionProvider>
        </FeedbackProvider>
      </MotionProvider>
    </SafeAreaProvider>
  );
}
export function ErrorBoundary({ retry }: ErrorBoundaryProps) {
  return (
    <View
      style={{
        flex: 1,
        backgroundColor: color.bg,
        alignItems: 'center',
        justifyContent: 'center',
        padding: 30,
        gap: 20,
      }}
    >
      <WaveMark size={48} />
      <Txt size={26} weight="display">
        Let’s find our rhythm again.
      </Txt>
      <Txt muted style={{ textAlign: 'center', lineHeight: 24 }}>
        Wavelength couldn’t open this screen. Your collection is safe.
      </Txt>
      <Button title="Try again" onPress={retry} />
    </View>
  );
}
