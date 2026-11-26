export function observeAudioActivity(
  audio: HTMLAudioElement,
  setActive: (active: boolean) => void,
) {
  const events = [
    'playing',
    'pause',
    'waiting',
    'seeking',
    'seeked',
    'ended',
    'emptied',
    'error',
  ];
  const update = (event: Event) => {
    setActive(
      (event.type === 'playing' || event.type === 'seeked') &&
        !audio.paused &&
        !audio.ended &&
        !audio.seeking &&
        audio.readyState >= audio.HAVE_FUTURE_DATA,
    );
  };
  for (const event of events) audio.addEventListener(event, update);
  return {
    destroy() {
      for (const event of events) audio.removeEventListener(event, update);
    },
  };
}
