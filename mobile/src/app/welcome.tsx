import { useState } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, StyleSheet, View } from 'react-native';
import { Redirect } from 'expo-router';
import { ArrowRight, Check } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { DEFAULT_SERVER, useSession } from '../lib/session';
import { color } from '../lib/theme';
import { Button, Field, Press, Txt, WaveMark } from '../components/ui';

export default function Welcome() {
  const session = useSession();
  const insets = useSafeAreaInsets();
  const [server, setServer] = useState(DEFAULT_SERVER);
  const [advanced, setAdvanced] = useState(false);
  const [pending, setPending] = useState(false);
  const [callback, setCallback] = useState('');
  if (session.grant || session.preview) return <Redirect href="/home" />;

  return (
    <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined} style={s.screen}>
      <ScrollView
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={[
          s.content,
          { paddingTop: insets.top + 28, paddingBottom: insets.bottom + 28 },
        ]}
      >
        <View style={s.mark}>
          <WaveMark size={31} />
        </View>
        <Txt accessibilityRole="header" weight="heavy" size={40} style={s.title}>
          Wavelength
        </Txt>
        <Txt muted size={16} style={s.subtitle}>
          Sign in to listen to your music library.
        </Txt>
        <View style={s.actions}>
          <Button
            title={pending ? 'Open sign-in again' : 'Sign in'}
            icon={ArrowRight}
            onPress={async () => {
              setPending(true);
              await session.login(server);
            }}
          />
          <Press label="Explore the preview" onPress={session.enterPreview} style={s.link}>
            <Txt muted size={14}>
              Open preview
            </Txt>
          </Press>
          {session.error ? (
            <Txt accessibilityRole="alert" size={13} style={s.error}>
              {session.error}
            </Txt>
          ) : null}
          <Press label="Connection options" onPress={() => setAdvanced(!advanced)} style={s.link}>
            <Txt muted size={13}>
              {advanced ? 'Hide connection options' : 'Connection options'}
            </Txt>
          </Press>
          {advanced && (
            <View style={s.options}>
              <Field
                label="Wavelength server"
                value={server}
                onChange={setServer}
                placeholder="https://wavelength.prayujt.com"
              />
              <Txt muted size={13} style={{ lineHeight: 20 }}>
                If the browser stays open after sign-in, choose “Copy return link instead” there,
                then paste the link below.
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
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.bg },
  content: {
    alignSelf: 'center',
    justifyContent: 'center',
    flexGrow: 1,
    width: '100%',
    maxWidth: 416,
    paddingHorizontal: 26,
  },
  mark: {
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: color.raised,
    width: 58,
    height: 58,
    borderRadius: 14,
    marginBottom: 22,
  },
  title: { letterSpacing: -1.6, lineHeight: 48 },
  subtitle: { marginTop: 10, lineHeight: 24 },
  actions: { marginTop: 30, gap: 4 },
  link: { minHeight: 44, alignItems: 'center', justifyContent: 'center' },
  error: { color: color.danger, lineHeight: 20, paddingVertical: 6 },
  options: { borderTopColor: color.line, borderTopWidth: 1, paddingTop: 16, gap: 14 },
});
