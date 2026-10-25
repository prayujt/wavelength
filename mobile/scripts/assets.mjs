import sharp from 'sharp';
import { mkdirSync, writeFileSync } from 'node:fs';
mkdirSync('assets', { recursive: true });
const bars = [120, 280, 430, 250, 360, 160];
const symbol = (fill) =>
  bars
    .map(
      (height, i) =>
        `<rect x="${267 + i * 82}" y="${512 - height / 2}" width="32" height="${height}" rx="16" fill="${fill}"/>`,
    )
    .join('');
const svg = (background, fill) =>
  `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">${background ? `<rect width="1024" height="1024" fill="${background}"/>` : ''}${symbol(fill)}</svg>`;
writeFileSync('assets/wavelength.svg', svg('#0A1111', '#C7F3CB'));
for (const [file, background, tint, size] of [
  ['icon.png', '#0A1111', '#C7F3CB', 1024],
  ['android-icon-foreground.png', '', '#C7F3CB', 1024],
  ['android-icon-monochrome.png', '', '#FFFFFF', 1024],
  ['splash-icon.png', '', '#C7F3CB', 256],
  ['favicon.png', '#0A1111', '#C7F3CB', 64],
])
  await sharp(Buffer.from(svg(background, tint)))
    .resize(size, size)
    .png()
    .toFile('assets/' + file);
// An original, short ambient chord for the opt-in preview. No third-party recordings.
const rate = 16000,
  seconds = 16,
  samples = rate * seconds,
  bytes = Buffer.alloc(44 + samples * 2);
bytes.write('RIFF');
bytes.writeUInt32LE(bytes.length - 8, 4);
bytes.write('WAVEfmt ', 8);
bytes.writeUInt32LE(16, 16);
bytes.writeUInt16LE(1, 20);
bytes.writeUInt16LE(1, 22);
bytes.writeUInt32LE(rate, 24);
bytes.writeUInt32LE(rate * 2, 28);
bytes.writeUInt16LE(2, 32);
bytes.writeUInt16LE(16, 34);
bytes.write('data', 36);
bytes.writeUInt32LE(samples * 2, 40);
for (let i = 0; i < samples; i++) {
  const t = i / rate,
    envelope = Math.min(1, t / 2, (seconds - t) / 3);
  const notes = [130.8128, 195.9977, 261.6256, 329.6276];
  const wave = notes.reduce(
    (sum, hz, j) =>
      sum + Math.sin(2 * Math.PI * hz * t + 0.05 * Math.sin(t)) * (0.045 / (1 + j * 0.4)),
    0,
  );
  bytes.writeInt16LE(Math.round(wave * envelope * 32767), 44 + i * 2);
}
writeFileSync('assets/preview.wav', bytes);
console.log('Wavelength icon family and original preview audio generated.');
