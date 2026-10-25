import React, { useState } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, View } from 'react-native';
import { Redirect } from 'expo-router';
import { LinearGradient } from 'expo-linear-gradient';
import { ArrowRight, Check, ShieldCheck } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { DEFAULT_SERVER, useSession } from '../lib/session';
import { color } from '../lib/theme';
import { Artwork } from '../components/artwork';
import { Button, FadeIn, Field, Press, Txt, WaveMark } from '../components/ui';

export default function Welcome() {
  const session = useSession(),
    insets = useSafeAreaInsets();
  const [server, setServer] = useState(DEFAULT_SERVER),
    [advanced, setAdvanced] = useState(false),
    [pending, setPending] = useState(false),
    [callback, setCallback] = useState('');
  if (session.grant || session.preview) return <Redirect href="/home" />;
  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      style={{ flex: 1, backgroundColor: color.bg }}
    >
      <ScrollView
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{
          paddingTop: insets.top + 22,
          paddingBottom: insets.bottom + 26,
          paddingHorizontal: 28,
          maxWidth: 560,
          width: '100%',
          alignSelf: 'center',
        }}
      >
        <FadeIn>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
            <WaveMark size={28} />
            <Txt weight="display" size={21} style={{ letterSpacing: -0.8 }}>
              wavelength
            </Txt>
          </View>
        </FadeIn>
        <FadeIn
          delay={80}
          style={{ height: 300, alignItems: 'center', justifyContent: 'center', marginTop: 24 }}
        >
          <View
            style={{
              position: 'absolute',
              transform: [{ rotate: '-16deg' }, { translateX: -68 }, { translateY: 10 }],
              opacity: 0.65,
            }}
          >
            <Artwork title="Blue hour" id="album-1" size={186} radius={18} />
          </View>
          <View
            style={{
              position: 'absolute',
              transform: [{ rotate: '14deg' }, { translateX: 71 }, { translateY: 3 }],
              opacity: 0.7,
            }}
          >
            <Artwork title="Soft focus" id="album-2" size={186} radius={18} />
          </View>
          <View style={{ transform: [{ rotate: '-3deg' }], boxShadow: '0px 18px 40px #00000088' }}>
            <Artwork title="The quiet between" id="album-0" size={210} radius={18} />
          </View>
          <LinearGradient
            colors={['#0A111100', color.bg]}
            style={{ position: 'absolute', left: -28, right: -28, bottom: -2, height: 60 }}
          />
        </FadeIn>
        <FadeIn delay={140}>
          <Txt
            size={11}
            weight="bold"
            style={{ color: color.accent, letterSpacing: 2, marginBottom: 14 }}
          >
            YOURS. EVERY NOTE.
          </Txt>
          <Txt size={43} weight="heavy" style={{ lineHeight: 49, letterSpacing: -2 }}>
            Less noise.{`\n`}More music.
          </Txt>
          <Txt muted size={15} style={{ lineHeight: 24, marginTop: 17, marginBottom: 26 }}>
            Your collection, beautifully connected.{`\n`}Find something you love. Stay a little
            longer.
          </Txt>
        </FadeIn>
        <FadeIn delay={210} style={{ gap: 14 }}>
          <Button
            title={pending ? 'Open sign-in again' : 'Find your wavelength'}
            icon={ArrowRight}
            onPress={async () => {
              setPending(true);
              await session.login(server);
            }}
          />
          <Press
            label="Explore the preview"
            onPress={session.enterPreview}
            style={{ minHeight: 44, justifyContent: 'center', alignItems: 'center' }}
          >
            <Txt muted size={13}>
              Take a look around first
            </Txt>
          </Press>
          <View
            style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 7 }}
          >
            <ShieldCheck size={14} color={color.faint} />
            <Txt size={11} style={{ color: color.faint }}>
              Secure sign-in with your Wavelength account
            </Txt>
          </View>
          {session.error ? (
            <Txt
              accessibilityRole="alert"
              size={13}
              style={{ color: color.danger, lineHeight: 21 }}
            >
              {session.error}
            </Txt>
          ) : null}
          <Press
            label="Connection options"
            onPress={() => setAdvanced(!advanced)}
            style={{ minHeight: 44, alignItems: 'center', justifyContent: 'center' }}
          >
            <Txt size={11} muted>
              {advanced
                ? 'Hide connection options'
                : pending
                  ? 'Trouble getting back to the app?'
                  : 'Using another server?'}
            </Txt>
          </Press>
          {advanced && (
            <View style={{ gap: 18 }}>
              <Field
                label="Wavelength server"
                value={server}
                onChange={setServer}
                placeholder="https://wavelength.prayujt.com"
              />
              <Txt muted size={12} style={{ lineHeight: 20 }}>
                After approving sign-in, return from the browser. If Expo Go stays in the browser,
                tap “Copy return link instead” on the sign-in page, then paste it here.
              </Txt>
              <Field
                label="Browser return link"
                value={callback}
                onChange={setCallback}
                placeholder="Paste the complete return link"
                secure
              />
              <Button
                title="Finish connecting"
                secondary
                icon={Check}
                disabled={!callback}
                onPress={() => session.complete(callback)}
              />
            </View>
          )}
        </FadeIn>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
