import { useEffect } from 'react';
import { Platform } from 'react-native';

export function useAppViewport() {
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const update = () => {
      const viewport = window.visualViewport;
      const editing = document.activeElement?.matches('input, textarea, [contenteditable="true"]');
      const height =
        editing && viewport
          ? Math.min(window.innerHeight, viewport.height + viewport.offsetTop)
          : window.innerHeight;
      document.documentElement.style.setProperty('--app-height', `${Math.round(height)}px`);
    };
    update();
    window.addEventListener('resize', update);
    window.visualViewport?.addEventListener('resize', update);
    window.visualViewport?.addEventListener('scroll', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    return () => {
      window.removeEventListener('resize', update);
      window.visualViewport?.removeEventListener('resize', update);
      window.visualViewport?.removeEventListener('scroll', update);
      document.removeEventListener('focusin', update);
      document.removeEventListener('focusout', update);
    };
  }, []);
}
