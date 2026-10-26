import React, { createContext, useContext, useEffect, useState } from 'react';
import {
  AccessibilityInfo,
  ActivityIndicator,
  Animated,
  Easing,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  type StyleProp,
  type TextProps,
  type TextStyle,
  type ViewStyle,
} from 'react-native';
import { ArrowLeft, ArrowRight, Search, X, type LucideIcon } from 'lucide-react-native';
import { router } from 'expo-router';
import * as Haptics from 'expo-haptics';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import Svg, { Path } from 'react-native-svg';
import { color, font } from '../lib/theme';
import { errorMessage } from '../lib/api';
import { useFeedback } from './feedback';

export function Txt({
  style,
  muted,
  size = 15,
  weight = 'regular',
  ...props
}: TextProps & { muted?: boolean; size?: number; weight?: keyof typeof font }) {
  return (
    <Text
      {...props}
      style={[
        { fontFamily: font[weight], color: muted ? color.muted : color.text, fontSize: size },
        style,
      ]}
    />
  );
}
export function WaveMark({ size = 30, tint = color.accent }: { size?: number; tint?: string }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 40 40" accessibilityLabel="Wavelength">
      <Path
        d="M5 16v8m6-15v22m6-27v32m6-24v16m6-21v26m6-18v10"
        stroke={tint}
        strokeWidth={3}
        strokeLinecap="round"
      />
    </Svg>
  );
}
const MotionContext = createContext(false);
export function MotionProvider({ children }: { children: React.ReactNode }) {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    void AccessibilityInfo.isReduceMotionEnabled().then(setReduced);
    const sub = AccessibilityInfo.addEventListener('reduceMotionChanged', setReduced);
    return () => sub.remove();
  }, []);
  return <MotionContext.Provider value={reduced}>{children}</MotionContext.Provider>;
}
export function useReducedMotion() {
  return useContext(MotionContext);
}
export function FadeIn({
  children,
  delay = 0,
  style,
}: {
  children: React.ReactNode;
  delay?: number;
  style?: StyleProp<ViewStyle>;
}) {
  const reduced = useReducedMotion(),
    progress = useState(() => new Animated.Value(0))[0];
  useEffect(() => {
    const animation = Animated.timing(progress, {
      toValue: 1,
      duration: reduced ? 0 : 220,
      delay: reduced ? 0 : Math.min(delay, 80),
      easing: Easing.out(Easing.cubic),
      useNativeDriver: true,
    });
    animation.start();
    return () => animation.stop();
  }, [delay, progress, reduced]);
  return (
    <Animated.View
      style={[
        style,
        {
          opacity: progress,
          transform: [
            {
              translateY: progress.interpolate({
                inputRange: [0, 1],
                outputRange: [reduced ? 0 : 8, 0],
              }),
            },
          ],
        },
      ]}
    >
      {children}
    </Animated.View>
  );
}
type PressProps = {
  children: React.ReactNode;
  onPress?: () => unknown | Promise<unknown>;
  label?: string;
  style?: StyleProp<ViewStyle>;
  disabled?: boolean;
  testID?: string;
  selected?: boolean;
};
export function Press({ children, onPress, label, style, disabled, testID, selected }: PressProps) {
  const scale = useState(() => new Animated.Value(1))[0],
    reduced = useReducedMotion(),
    feedback = useFeedback();
  const [busy, setBusy] = useState(false);
  function animate(toValue: number) {
    if (!reduced)
      Animated.spring(scale, {
        toValue,
        damping: 20,
        stiffness: 350,
        mass: 0.45,
        useNativeDriver: true,
      }).start();
  }
  const layout = StyleSheet.flatten(style);
  return (
    <Animated.View
      style={{
        flex: layout?.flex,
        flexGrow: layout?.flexGrow,
        flexShrink: layout?.flexShrink,
        alignSelf: layout?.alignSelf,
        transform: [{ scale }],
        opacity: disabled || busy ? 0.4 : 1,
      }}
    >
      <Pressable
        testID={testID}
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityState={{ disabled: !!disabled || busy, selected }}
        disabled={disabled || busy}
        onPressIn={() => animate(0.97)}
        onPressOut={() => animate(1)}
        style={style}
        onPress={async (event) => {
          event.stopPropagation();
          if (!onPress) return;
          if (Platform.OS !== 'web') void Haptics.selectionAsync().catch(() => {});
          setBusy(true);
          try {
            await onPress();
          } catch (e) {
            feedback.notify(errorMessage(e), 'error');
          } finally {
            setBusy(false);
          }
        }}
      >
        {children}
      </Pressable>
    </Animated.View>
  );
}
export function Button({
  title,
  onPress,
  icon: Icon,
  secondary,
  small,
  disabled,
  loading,
  style,
}: {
  title: string;
  onPress?: () => unknown | Promise<unknown>;
  icon?: LucideIcon;
  secondary?: boolean;
  small?: boolean;
  disabled?: boolean;
  loading?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const [busy, setBusy] = useState(false);
  return (
    <Press
      label={title}
      onPress={async () => {
        setBusy(true);
        try {
          await onPress?.();
        } finally {
          setBusy(false);
        }
      }}
      disabled={disabled || loading || busy}
      style={[
        s.button,
        secondary && s.secondary,
        small && { minHeight: 42, paddingHorizontal: 18 },
        style,
      ]}
    >
      {loading || busy ? (
        <ActivityIndicator size="small" color={secondary ? color.text : color.accentInk} />
      ) : Icon ? (
        <Icon
          size={small ? 17 : 20}
          color={secondary ? color.text : color.accentInk}
          strokeWidth={2}
        />
      ) : null}
      <Txt
        weight="bold"
        size={small ? 13 : 15}
        style={{ color: secondary ? color.text : color.accentInk }}
      >
        {title}
      </Txt>
    </Press>
  );
}
export function IconButton({
  icon: Icon,
  label,
  onPress,
  active,
  size = 22,
  style,
  disabled,
  tint,
}: {
  icon: LucideIcon;
  label: string;
  onPress?: () => unknown | Promise<unknown>;
  active?: boolean;
  size?: number;
  style?: StyleProp<ViewStyle>;
  disabled?: boolean;
  tint?: string;
}) {
  return (
    <Press
      onPress={onPress}
      label={label}
      disabled={disabled}
      style={[s.iconButton, active && { backgroundColor: color.raised }, style]}
    >
      <Icon size={size} strokeWidth={1.8} color={tint || (active ? color.accent : color.text)} />
    </Press>
  );
}
export function Pill({
  title,
  active,
  onPress,
  icon: Icon,
}: {
  title: string;
  active?: boolean;
  onPress: () => unknown;
  icon?: LucideIcon;
}) {
  return (
    <Press
      label={title}
      selected={active}
      onPress={onPress}
      style={[s.pill, active && { backgroundColor: color.accent, borderColor: color.accent }]}
    >
      {Icon && <Icon size={14} color={active ? color.accentInk : color.muted} />}
      <Txt size={13} weight="medium" style={{ color: active ? color.accentInk : color.muted }}>
        {title}
      </Txt>
    </Press>
  );
}
export function Screen({
  children,
  scroll = true,
  style,
}: {
  children: React.ReactNode;
  scroll?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const inset = useSafeAreaInsets();
  return (
    <View style={[s.screen, { paddingTop: inset.top }, style]}>
      {scroll ? (
        <ScrollView
          showsVerticalScrollIndicator={false}
          contentContainerStyle={{ paddingBottom: 160 }}
          keyboardShouldPersistTaps="handled"
        >
          {children}
        </ScrollView>
      ) : (
        children
      )}
    </View>
  );
}
export function Header({
  title,
  subtitle,
  back,
  right,
  eyebrow,
}: {
  title: string;
  subtitle?: string;
  back?: boolean;
  right?: React.ReactNode;
  eyebrow?: string;
}) {
  return (
    <View style={s.header}>
      {back && (
        <View style={{ marginLeft: -10, marginBottom: 14 }}>
          <IconButton
            icon={ArrowLeft}
            label="Go back"
            onPress={() => (router.canGoBack() ? router.back() : router.replace('/'))}
          />
        </View>
      )}
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
        }}
      >
        <View style={{ flex: 1 }}>
          {eyebrow && (
            <Txt size={10} weight="bold" style={s.eyebrow}>
              {eyebrow}
            </Txt>
          )}
          <Txt
            accessibilityRole="header"
            weight="heavy"
            size={32}
            style={{ letterSpacing: -1.2, lineHeight: 40 }}
          >
            {title}
          </Txt>
          {subtitle && (
            <Txt muted size={14} style={{ marginTop: 5, lineHeight: 21 }}>
              {subtitle}
            </Txt>
          )}
        </View>
        {right}
      </View>
    </View>
  );
}
export function Section({
  title,
  eyebrow,
  action,
  onPress,
  children,
}: {
  title: string;
  eyebrow?: string;
  action?: string;
  onPress?: () => unknown;
  children: React.ReactNode;
}) {
  return (
    <View style={{ marginTop: 32 }}>
      <View style={s.sectionHeader}>
        <View>
          {eyebrow && (
            <Txt weight="bold" size={10} style={s.eyebrow}>
              {eyebrow}
            </Txt>
          )}
          <Txt
            accessibilityRole="header"
            size={22}
            weight="display"
            style={{ letterSpacing: -0.7 }}
          >
            {title}
          </Txt>
        </View>
        {action && (
          <Press
            label={action}
            onPress={onPress}
            style={{ minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: 4 }}
          >
            <Txt muted size={12} weight="medium">
              {action}
            </Txt>
            <ArrowRight size={15} color={color.muted} />
          </Press>
        )}
      </View>
      {children}
    </View>
  );
}
export function SearchField({
  value,
  onChange,
  placeholder = 'Artists, songs, albums…',
  autoFocus,
}: {
  value: string;
  onChange(value: string): void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <View style={[s.search, focused && { borderColor: color.accent }]}>
      <Search size={20} color={color.muted} />
      <TextInput
        accessibilityLabel={placeholder}
        placeholder={placeholder}
        placeholderTextColor={color.faint}
        value={value}
        onChangeText={onChange}
        autoFocus={autoFocus}
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
        underlineColorAndroid="transparent"
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        style={s.input}
      />
      {value.length > 0 && (
        <Press label="Clear search" onPress={() => onChange('')} style={{ padding: 5 }}>
          <X size={18} color={color.muted} />
        </Press>
      )}
    </View>
  );
}
export function Field({
  label,
  value,
  onChange,
  placeholder,
  multiline,
  secure,
}: {
  label: string;
  value: string;
  onChange(value: string): void;
  placeholder?: string;
  multiline?: boolean;
  secure?: boolean;
}) {
  return (
    <View style={{ gap: 9 }}>
      <Txt size={12} weight="medium" muted>
        {label}
      </Txt>
      <TextInput
        accessibilityLabel={label}
        placeholder={placeholder}
        placeholderTextColor={color.faint}
        value={value}
        onChangeText={onChange}
        autoCapitalize="none"
        autoCorrect={false}
        multiline={multiline}
        secureTextEntry={secure}
        style={[s.field, multiline && { minHeight: 100, textAlignVertical: 'top' }]}
      />
    </View>
  );
}
export function Empty({
  title,
  text,
  icon: Icon,
  action,
  onPress,
}: {
  title: string;
  text: string;
  icon?: LucideIcon;
  action?: string;
  onPress?: () => unknown;
}) {
  return (
    <FadeIn style={s.empty}>
      <View style={s.emptyIcon}>
        {Icon ? <Icon size={30} color={color.accent} strokeWidth={1.3} /> : <WaveMark size={35} />}
      </View>
      <Txt size={21} weight="display" style={{ textAlign: 'center' }}>
        {title}
      </Txt>
      <Txt muted size={14} style={{ textAlign: 'center', lineHeight: 22, maxWidth: 290 }}>
        {text}
      </Txt>
      {action && (
        <View style={{ marginTop: 8 }}>
          <Button title={action} onPress={onPress} secondary small />
        </View>
      )}
    </FadeIn>
  );
}
export function Loading({ rows = 4 }: { rows?: number }) {
  return (
    <View accessibilityLabel="Loading collection" style={{ padding: 24, gap: 18 }}>
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ flexDirection: 'row', gap: 14 }}>
          <View style={s.skeletonArt} />
          <View style={{ flex: 1, justifyContent: 'center', gap: 10 }}>
            <View style={[s.skeletonLine, { width: '70%' }]} />
            <View style={[s.skeletonLine, { width: '40%', opacity: 0.5 }]} />
          </View>
        </View>
      ))}
    </View>
  );
}
export function Failure({ error, retry }: { error: unknown; retry: () => unknown }) {
  return (
    <Empty
      title="A little out of tune"
      text={errorMessage(error)}
      action="Try again"
      onPress={retry}
    />
  );
}
export function useDebounced<T>(value: T, delay = 400) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
export const textStyles = {
  eyebrow: {
    fontFamily: font.bold,
    fontSize: 10,
    color: color.accent,
    letterSpacing: 2,
  } satisfies TextStyle,
};
const s = StyleSheet.create({
  screen: { backgroundColor: color.bg, flex: 1 },
  header: { paddingHorizontal: 24, paddingTop: 16, paddingBottom: 16 },
  button: {
    minHeight: 52,
    borderRadius: 28,
    backgroundColor: color.accent,
    paddingHorizontal: 24,
    flexDirection: 'row',
    gap: 9,
    justifyContent: 'center',
    alignItems: 'center',
  },
  secondary: { backgroundColor: color.raised, borderColor: color.line, borderWidth: 1 },
  iconButton: {
    width: 44,
    height: 44,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 22,
  },
  pill: {
    minHeight: 38,
    borderRadius: 22,
    borderWidth: 1,
    borderColor: color.line,
    paddingHorizontal: 17,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
  },
  sectionHeader: {
    paddingHorizontal: 24,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 17,
    gap: 10,
  },
  eyebrow: { color: color.accent, letterSpacing: 1.8, marginBottom: 8 },
  search: {
    marginHorizontal: 24,
    minHeight: 52,
    paddingHorizontal: 16,
    borderWidth: 1,
    borderColor: color.line,
    borderRadius: 16,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    backgroundColor: color.surface,
  },
  input: {
    color: color.text,
    flex: 1,
    minHeight: 50,
    fontFamily: font.regular,
    fontSize: 16,
    outlineWidth: 0,
    outlineStyle: 'solid',
  },
  field: {
    borderRadius: 15,
    borderWidth: 1,
    borderColor: color.line,
    backgroundColor: color.surface,
    minHeight: 54,
    padding: 16,
    color: color.text,
    fontFamily: font.regular,
    fontSize: 16,
    outlineWidth: 0,
    outlineStyle: 'solid',
  },
  empty: {
    alignItems: 'center',
    justifyContent: 'center',
    padding: 30,
    paddingVertical: 54,
    gap: 12,
  },
  emptyIcon: {
    width: 72,
    height: 72,
    borderRadius: 25,
    backgroundColor: color.surface,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 8,
  },
  skeletonArt: { width: 55, height: 55, borderRadius: 12, backgroundColor: color.surface },
  skeletonLine: { height: 10, borderRadius: 5, backgroundColor: color.raised },
});
