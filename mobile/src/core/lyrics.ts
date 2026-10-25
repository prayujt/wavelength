export function parseLyrics(value: string): { text: string; time?: number }[] {
  const result: { text: string; time?: number }[] = [];
  for (const line of value.split(/\r?\n/)) {
    const stamps = [...line.matchAll(/\[(\d+):(\d{2})(?:\.(\d{1,3}))?\]/g)];
    const text = line.replace(/\[[^\]]*\]/g, '').trim();
    if (!text) continue;
    if (!stamps.length) result.push({ text });
    else
      for (const stamp of stamps)
        result.push({
          text,
          time: Number(stamp[1]) * 60 + Number(stamp[2]) + Number('0.' + (stamp[3] || '0')),
        });
  }
  if (result.every((line) => line.time !== undefined)) result.sort((a, b) => a.time! - b.time!);
  return result;
}
