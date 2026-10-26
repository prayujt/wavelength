import { useEffect } from 'react';
import { Platform } from 'react-native';

export function useAppViewport() {
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const update = () => {
      const viewport = window.visualViewport;
      const editing = document.activeElement?.matches('input, textarea, [contenteditable="true"]');
      // CSS owns the full-screen height. In iOS standalone, innerHeight can
      // omit safe areas and leave a permanent strip below a fixed-size root.
      // Override it only for a software keyboard, never for edge overscroll.
      const keyboard =
        editing && viewport && viewport.height < document.documentElement.clientHeight - 120;
      if (keyboard)
        document.documentElement.style.setProperty(
          '--app-height',
          `${Math.round(viewport.height + viewport.offsetTop)}px`,
        );
      else document.documentElement.style.removeProperty('--app-height');
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
