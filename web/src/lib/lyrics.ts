export type LyricLine = { text: string; at?: number };

export function parseLyrics(text: string): LyricLine[] {
  const timed: LyricLine[] = [];
  for (const line of text.split(/\r?\n/)) {
    const match = line.match(/^((?:\[\d+:\d+(?:\.\d+)?\])+)(.*)$/);
    if (!match) continue;
    for (const stamp of match[1].matchAll(/\[(\d+):(\d+(?:\.\d+)?)\]/g)) {
      timed.push({
        at: Number(stamp[1]) * 60 + Number(stamp[2]),
        text: match[2].trim(),
      });
    }
  }
  if (timed.length) return timed.sort((a, b) => a.at! - b.at!);
  return text
    .trim()
    .split(/\r?\n/)
    .map((line) => ({ text: line }));
}
