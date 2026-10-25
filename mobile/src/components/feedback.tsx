import React, { createContext, useContext, useEffect, useRef, useState } from 'react';
import { Modal, Pressable, StyleSheet, Text, View } from 'react-native';
import { Check, X } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { color, font } from '../lib/theme';

type Feedback = {
  notify(message: string, tone?: 'success' | 'error'): void;
  confirm(title: string, message: string, action?: string): Promise<boolean>;
};
const Context = createContext<Feedback>({
  notify() {},
  async confirm() {
    return false;
  },
});
export function FeedbackProvider({ children }: { children: React.ReactNode }) {
  const insets = useSafeAreaInsets();
  const [toast, setToast] = useState<{ message: string; tone: 'success' | 'error' } | null>(null);
  const [confirmation, setConfirmation] = useState<{
    title: string;
    message: string;
    action: string;
    resolve(value: boolean): void;
  } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  function finish(value: boolean) {
    confirmation?.resolve(value);
    setConfirmation(null);
  }
  return (
    <Context.Provider
      value={{
        notify(message, tone = 'success') {
          if (timer.current) clearTimeout(timer.current);
          setToast({ message, tone });
          timer.current = setTimeout(() => setToast(null), 4500);
        },
        confirm(title, message, action = 'Delete') {
          return new Promise((resolve) => setConfirmation({ title, message, action, resolve }));
        },
      }}
    >
      {children}
      {toast && (
        <View pointerEvents="box-none" style={[s.toastLayer, { top: insets.top + 14 }]}>
          <Pressable
            accessibilityRole="alert"
            accessibilityLiveRegion="polite"
            onPress={() => setToast(null)}
            style={[s.toast, toast.tone === 'error' && { backgroundColor: '#482E2A' }]}
          >
            {toast.tone === 'error' ? (
              <X color={color.danger} size={18} />
            ) : (
              <Check color={color.accent} size={18} />
            )}
            <Text style={s.toastText}>{toast.message}</Text>
          </Pressable>
        </View>
      )}
      <Modal
        transparent
        animationType="fade"
        visible={!!confirmation}
        onRequestClose={() => finish(false)}
      >
        <View style={s.modal}>
          <Pressable
            accessibilityLabel="Cancel"
            onPress={() => finish(false)}
            style={StyleSheet.absoluteFill}
          />
          <View accessibilityViewIsModal style={s.dialog}>
            <Text style={s.title}>{confirmation?.title}</Text>
            <Text style={s.message}>{confirmation?.message}</Text>
            <View style={s.actions}>
              <Pressable accessibilityRole="button" onPress={() => finish(false)} style={s.button}>
                <Text style={s.label}>Keep it</Text>
              </Pressable>
              <Pressable
                accessibilityRole="button"
                onPress={() => finish(true)}
                style={[s.button, { backgroundColor: color.danger }]}
              >
                <Text style={[s.label, { color: color.bg }]}>{confirmation?.action}</Text>
              </Pressable>
            </View>
          </View>
        </View>
      </Modal>
    </Context.Provider>
  );
}
export const useFeedback = () => useContext(Context);
const s = StyleSheet.create({
  toastLayer: { position: 'absolute', left: 16, right: 16, alignItems: 'center', zIndex: 100 },
  toast: {
    backgroundColor: '#243C2E',
    borderRadius: 18,
    padding: 16,
    flexDirection: 'row',
    gap: 10,
    alignItems: 'center',
    maxWidth: 500,
    boxShadow: '0px 8px 30px #00000044',
  },
  toastText: {
    color: color.text,
    fontFamily: font.medium,
    fontSize: 14,
    flexShrink: 1,
    lineHeight: 20,
  },
  modal: {
    flex: 1,
    backgroundColor: color.overlay,
    justifyContent: 'center',
    alignItems: 'center',
    padding: 24,
  },
  dialog: {
    backgroundColor: color.raised,
    borderRadius: 26,
    width: '100%',
    maxWidth: 400,
    padding: 24,
    gap: 14,
  },
  title: { color: color.text, fontFamily: font.display, fontSize: 24, letterSpacing: -0.5 },
  message: { color: color.muted, fontFamily: font.regular, fontSize: 15, lineHeight: 24 },
  actions: { flexDirection: 'row', gap: 10, marginTop: 10 },
  button: {
    flex: 1,
    minHeight: 48,
    borderRadius: 24,
    backgroundColor: color.surface,
    alignItems: 'center',
    justifyContent: 'center',
  },
  label: { color: color.text, fontFamily: font.bold, fontSize: 14 },
});
