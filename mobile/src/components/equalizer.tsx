import { useEffect, useMemo, useState } from 'react';
import { Animated, Easing, View } from 'react-native';
import { usePlayer } from '../lib/player';
import { color as theme } from '../lib/theme';
import { useReducedMotion } from './ui';

const levels = [
  [0.28, 0.58, 0.38, 0.69, 0.28],
  [0.62, 0.32, 0.87, 0.44, 0.62],
  [0.88, 0.69, 0.48, 1, 0.88],
  [0.47, 0.93, 0.68, 0.3, 0.47],
  [0.7, 0.42, 0.98, 0.56, 0.7],
  [0.32, 0.64, 0.28, 0.51, 0.32],
];

export function Equalizer({ size = 18, color = theme.accent }: { size?: number; color?: string }) {
  const { audioActive, loading, playing } = usePlayer();
  const reducedMotion = useReducedMotion();
  const running = playing && audioActive && !loading && !reducedMotion;
  const [phase] = useState(() => new Animated.Value(0));
  const bars = useMemo(
    () =>
      levels.map((outputRange) =>
        phase.interpolate({ inputRange: [0, 0.25, 0.5, 0.75, 1], outputRange }),
      ),
    [phase],
  );
  useEffect(() => {
    if (!running) return;
    const loop = Animated.loop(
      Animated.timing(phase, {
        toValue: 1,
        duration: 1060,
        easing: Easing.linear,
        isInteraction: false,
        useNativeDriver: true,
      }),
    );
    loop.start();
    return () => {
      loop.stop();
      phase.setValue(0);
    };
  }, [phase, running]);
  return (
    <View
      testID="playback-wave"
      aria-hidden
      style={{
        width: size,
        height: size,
        flexDirection: 'row',
        alignItems: 'center',
        justifyContent: 'space-between',
      }}
    >
      {bars.map((bar, index) => (
        <Animated.View
          key={index}
          style={{
            height: size * 0.82,
            width: size * 0.09,
            borderRadius: size * 0.05,
            backgroundColor: color,
            transform: [{ scaleY: running ? bar : levels[index][0] }],
          }}
        />
      ))}
    </View>
  );
}
