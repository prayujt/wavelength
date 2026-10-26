import { useEffect, useRef } from 'react';
import { router } from 'expo-router';
import { usePlayer } from '../../lib/player';

// Keep existing bookmarks useful while the mode itself stays on the current UI.
export default function LegacyFeed() {
  const player = usePlayer();
  const start = useRef(player.startFeed);
  useEffect(() => {
    const timer = setTimeout(() => {
      void start.current();
      router.replace('/home');
    }, 0);
    return () => clearTimeout(timer);
  }, []);
  return null;
}
