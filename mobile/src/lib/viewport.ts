import { useEffect } from 'react';
import { Platform } from 'react-native';
import { appViewportHeight } from '../core/viewport';
import { installed } from './device-store';

export function useAppViewport() {
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    // Measure viewport units independently of the corrected document height.
    // Otherwise keyboard/resize events can repeatedly undo their own correction.
    const probe = document.createElement('div');
    probe.dataset.appViewportProbe = '';
    probe.style.cssText =
      'position:absolute;top:0;left:0;width:1px;height:100vh;height:100dvh;box-sizing:border-box;visibility:hidden;pointer-events:none;padding-top:env(safe-area-inset-top);padding-bottom:env(safe-area-inset-bottom)';
    document.body.appendChild(probe);
    const update = () => {
      const viewport = window.visualViewport;
      const editing = document.activeElement?.matches('input, textarea, [contenteditable="true"]');
      const cssHeight = probe.getBoundingClientRect().height;
      const style = getComputedStyle(probe);
      const height = appViewportHeight({
        iphoneStandalone: /iPhone/.test(navigator.userAgent) && installed(),
        width: document.documentElement.clientWidth,
        height: cssHeight,
        screenWidth: window.screen.width,
        screenHeight: window.screen.height,
        top: parseFloat(style.paddingTop) || 0,
        bottom: parseFloat(style.paddingBottom) || 0,
      });
      const keyboard =
        editing && viewport && viewport.scale <= 1.01 && viewport.height < height - 120;
      const target =
        keyboard || height > cssHeight + 1
          ? `${Math.round(keyboard ? viewport.height + viewport.offsetTop : height)}px`
          : '';
      const root = document.documentElement.style;
      if (root.getPropertyValue('--app-height') !== target) {
        if (target) root.setProperty('--app-height', target);
        else root.removeProperty('--app-height');
      }
    };
    update();
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(update);
    observer?.observe(probe);
    document.addEventListener('visibilitychange', update);
    window.addEventListener('resize', update);
    window.visualViewport?.addEventListener('resize', update);
    window.visualViewport?.addEventListener('scroll', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    return () => {
      observer?.disconnect();
      probe.remove();
      document.documentElement.style.removeProperty('--app-height');
      document.removeEventListener('visibilitychange', update);
      window.removeEventListener('resize', update);
      window.visualViewport?.removeEventListener('resize', update);
      window.visualViewport?.removeEventListener('scroll', update);
      document.removeEventListener('focusin', update);
      document.removeEventListener('focusout', update);
    };
  }, []);
}
