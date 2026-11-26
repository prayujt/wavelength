import { useEffect, useMemo, useState } from 'react';
import { Animated, Easing, PanResponder, Platform, type ViewStyle } from 'react-native';
import { router } from 'expo-router';
import { useReducedMotion } from '../components/ui';

const web = Platform.OS === 'web';
export const playerSwipeArea = web ? ({ touchAction: 'none' } as ViewStyle) : undefined;

function dismiss() {
  if (router.canGoBack()) router.back();
  else router.replace('/home');
}

export function usePlayerTransition(height: number) {
  const reduced = useReducedMotion();
  const [closing, setClosing] = useState(false);
  const [progress] = useState(
    () =>
      new Animated.Value(
        web &&
          typeof window !== 'undefined' &&
          !window.matchMedia('(prefers-reduced-motion: reduce)').matches
          ? 1
          : 0,
      ),
  );
  useEffect(() => {
    if (!web) return;
    const entrance = Animated.timing(progress, {
      toValue: 0,
      duration: reduced ? 0 : 300,
      easing: Easing.out(Easing.cubic),
      useNativeDriver: false,
      isInteraction: false,
    });
    entrance.start();
    return () => progress.stopAnimation();
  }, [progress, reduced]);

  return useMemo(() => {
    function close() {
      if (closing) return;
      if (!web || reduced) return dismiss();
      setClosing(true);
      Animated.timing(progress, {
        toValue: 1,
        duration: 230,
        easing: Easing.in(Easing.quad),
        useNativeDriver: false,
        isInteraction: false,
      }).start(({ finished }) => {
        setClosing(false);
        if (finished) dismiss();
      });
    }

    function settle() {
      Animated.timing(progress, {
        toValue: 0,
        duration: 200,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: false,
        isInteraction: false,
      }).start();
    }

    const swipe = web
      ? PanResponder.create({
          onMoveShouldSetPanResponderCapture: (_, { dx, dy, numberActiveTouches }) =>
            !closing && numberActiveTouches === 1 && dy > 8 && dy > Math.abs(dx),
          onPanResponderMove: (_, { dy }) => {
            if (!reduced) progress.setValue(Math.max(0, dy / height));
          },
          onPanResponderRelease: (_, { dy, vy }) => {
            if (dy > Math.min(height * 0.2, 110) || (dy > 34 && vy > 0.55)) close();
            else settle();
          },
          onPanResponderTerminate: settle,
        }).panHandlers
      : undefined;

    return {
      close,
      swipe,
      translateY: web
        ? progress.interpolate({
            inputRange: [0, 1],
            outputRange: [0, height],
            extrapolate: 'clamp',
          })
        : 0,
    };
  }, [closing, height, progress, reduced]);
}
