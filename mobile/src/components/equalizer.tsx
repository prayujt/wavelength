import { useEffect, useState } from 'react';
import { Animated, Easing, View } from 'react-native';
import { color } from '../lib/theme';
import { useReducedMotion } from './ui';

export function Equalizer() {
  const reduced = useReducedMotion();
  const [bars] = useState(() => [
    new Animated.Value(0.45),
    new Animated.Value(0.85),
    new Animated.Value(0.6),
  ]);
  useEffect(() => {
    if (reduced) return;
    const loops = bars.map((bar, index) =>
      Animated.loop(
        Animated.sequence([
          Animated.timing(bar, {
            toValue: 0.25,
            duration: 310 + index * 90,
            easing: Easing.inOut(Easing.sin),
            useNativeDriver: true,
          }),
          Animated.timing(bar, {
            toValue: 1,
            duration: 380 + index * 70,
            easing: Easing.inOut(Easing.sin),
            useNativeDriver: true,
          }),
        ]),
      ),
    );
    for (const loop of loops) loop.start();
    return () => {
      for (const loop of loops) loop.stop();
    };
  }, [bars, reduced]);
  return (
    <View
      accessibilityLabel="Playing"
      style={{ height: 18, flexDirection: 'row', gap: 3, alignItems: 'center' }}
    >
      {bars.map((bar, index) => (
        <Animated.View
          key={index}
          style={{
            height: 16,
            width: 2.5,
            borderRadius: 2,
            backgroundColor: color.accent,
            transform: [{ scaleY: bar }],
          }}
        />
      ))}
    </View>
  );
}
