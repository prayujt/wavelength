import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Animated, Easing, Platform, type ScrollView, type ViewStyle } from 'react-native';
import { router } from 'expo-router';
import { useReducedMotion } from '../components/ui';

const web = Platform.OS === 'web';
export const playerScrollStyle = web ? ({ overscrollBehaviorY: 'none' } as ViewStyle) : undefined;

function dismiss() {
  if (router.canGoBack()) router.back();
  else router.replace('/home');
}

export function usePlayerTransition(height: number, hasTrack: boolean) {
  const reduced = useReducedMotion();
  const [closing, setClosing] = useState(false);
  const scrollRef = useRef<ScrollView>(null);
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

  const close = useCallback(() => {
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
  }, [closing, progress, reduced]);

  useEffect(() => {
    if (!web || !hasTrack || closing) return;
    const scroll = scrollRef.current?.getScrollableNode();
    if (!(scroll instanceof HTMLElement)) return;
    let start: Touch | undefined;
    let startedAt = 0;
    let dragging = false;

    function settle() {
      Animated.timing(progress, {
        toValue: 0,
        duration: 200,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: false,
        isInteraction: false,
      }).start();
    }

    function cancel() {
      if (dragging) settle();
      start = undefined;
      dragging = false;
    }
    function touchStart(event: TouchEvent) {
      cancel();
      if (event.touches.length !== 1 || scroll.scrollTop > 1) return;
      const target = event.target;
      if (target instanceof Element && target.closest('input, [role="slider"]')) return;
      start = event.touches[0];
      startedAt = event.timeStamp;
    }
    function touchMove(event: TouchEvent) {
      if (!start) return;
      const touch = event.touches[0];
      if (event.touches.length !== 1 || !event.cancelable) return cancel();
      const x = Math.abs(touch.clientX - start.clientX);
      const y = touch.clientY - start.clientY;
      if (!dragging && (y <= 0 || y <= x)) {
        if (Math.max(x, Math.abs(y)) > 6) cancel();
        return;
      }
      // Claim downward motion before Safari turns the drag into native scrolling.
      event.preventDefault();
      dragging = true;
      if (!reduced) progress.setValue(Math.max(0, y / height));
    }
    function touchEnd(event: TouchEvent) {
      if (!start || !dragging) return cancel();
      if (event.cancelable) event.preventDefault();
      const y = event.changedTouches[0].clientY - start.clientY;
      const velocity = y / Math.max(1, event.timeStamp - startedAt);
      if (y > Math.min(height * 0.2, 110) || (y > 34 && velocity > 0.55)) close();
      else settle();
      start = undefined;
      dragging = false;
    }
    scroll.addEventListener('touchstart', touchStart, { passive: true });
    scroll.addEventListener('touchmove', touchMove, { passive: false });
    scroll.addEventListener('touchend', touchEnd);
    scroll.addEventListener('touchcancel', cancel);
    return () => {
      scroll.removeEventListener('touchstart', touchStart);
      scroll.removeEventListener('touchmove', touchMove);
      scroll.removeEventListener('touchend', touchEnd);
      scroll.removeEventListener('touchcancel', cancel);
    };
  }, [close, closing, hasTrack, height, progress, reduced]);

  const translateY = useMemo(
    () =>
      web
        ? progress.interpolate({
            inputRange: [0, 1],
            outputRange: [0, height],
            extrapolate: 'clamp',
          })
        : 0,
    [height, progress],
  );
  return { close, scrollRef, translateY };
}
