export type RepeatMode = 'off' | 'all' | 'one';

export function moveItem<T>(items: readonly T[], from: number, to: number): T[] {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length) return [...items];
  const copy = [...items];
  copy.splice(to, 0, copy.splice(from, 1)[0]);
  return copy;
}

export function shuffled<T>(items: readonly T[], random = Math.random): T[] {
  const copy = [...items];
  for (let i = copy.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [copy[i], copy[j]] = [copy[j], copy[i]];
  }
  return copy;
}

export function nextIndex(
  length: number,
  index: number,
  repeat: RepeatMode,
  ended = false,
): number | null {
  if (!length) return null;
  if (ended && repeat === 'one') return index;
  if (index + 1 < length) return index + 1;
  return repeat === 'all' ? 0 : null;
}

export function clock(seconds: number): string {
  const value = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`;
}

export function duration(seconds: number): string {
  const minutes = Math.round(Math.max(0, seconds) / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)} hr ${minutes % 60} min` : `${minutes} min`;
}

export function refKey(ref?: { provider: string; id: string }): string {
  return ref ? `${ref.provider}:${ref.id}` : '';
}

export function releaseAvailability(
  refs: { provider: string; id: string }[],
  imported: Map<string, string>,
) {
  const ids = refs.map((ref) => imported.get(refKey(ref)));
  return {
    available: ids.filter(Boolean).length,
    complete: refs.length > 0 && ids.every(Boolean),
    ids,
  };
}
