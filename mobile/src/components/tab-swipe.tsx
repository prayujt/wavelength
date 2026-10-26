import React, { useRef } from 'react';
import { Platform, View, useWindowDimensions, type GestureResponderEvent } from 'react-native';
import { router, useSegments } from 'expo-router';

const routes = ['/home', '/playlists', '/library', '/search'] as const;

// Keep OS edge-back, sliders, inputs and horizontal album shelves in control of
// their own gestures. A deliberate horizontal swipe on a page changes tabs.
export function TabSwipe({ children }: { children: React.ReactNode }) {
  const segments = useSegments(),
    { width } = useWindowDimensions();
  const start = useRef<{ x: number; y: number; at: number } | null>(null);
  function begin(event: GestureResponderEvent) {
    start.current = null;
    const { touches } = event.nativeEvent;
    const { pageX: x, pageY: y } = touches[0] || event.nativeEvent;
    if (touches.length !== 1 || x < 24 || x > width - 24) return;
    if (Platform.OS === 'web') {
      let element = event.target as unknown as HTMLElement | null;
      if (element?.closest('input, textarea, [role="slider"], [role="tablist"]')) return;
      while (element) {
        const style = getComputedStyle(element);
        if (element.scrollWidth > element.clientWidth + 2 && /auto|scroll/.test(style.overflowX))
          return;
        element = element.parentElement;
      }
    }
    start.current = { x, y, at: Date.now() };
  }
  function end(event: GestureResponderEvent) {
    const from = start.current;
    start.current = null;
    if (!from) return;
    const touch = event.nativeEvent.changedTouches[0] || event.nativeEvent;
    const dx = touch.pageX - from.x,
      dy = touch.pageY - from.y;
    if (
      Date.now() - from.at > 650 ||
      Math.abs(dx) < 72 ||
      Math.abs(dy) > 40 ||
      Math.abs(dx) < Math.abs(dy) * 2
    )
      return;
    const screen = segments.slice(segments.indexOf('(tabs)') + 1);
    if (screen.length > 1 && screen[1] !== 'index') return;
    const at = routes.indexOf(('/' + screen[0]) as (typeof routes)[number]);
    if (at < 0) return;
    const next = routes[at + (dx < 0 ? 1 : -1)];
    if (next) router.navigate(next);
  }
  return (
    <View
      testID="tab-swipe-surface"
      style={{ flex: 1 }}
      onTouchStart={begin}
      onTouchEnd={end}
      onTouchCancel={() => {
        start.current = null;
      }}
    >
      {children}
    </View>
  );
}
