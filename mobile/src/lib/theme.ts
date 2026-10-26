export const color = {
  bg: '#0A1111',
  surface: '#121D1C',
  raised: '#1D2B28',
  line: '#283733',
  text: '#F5F3EA',
  muted: '#99AAA2',
  faint: '#63796F',
  accent: '#C7F3CB',
  accentInk: '#173A26',
  gold: '#E0BC92',
  danger: '#F0A49B',
  overlay: '#030B09CC',
};
export const font = {
  regular: 'DMSans_400Regular',
  medium: 'DMSans_500Medium',
  bold: 'DMSans_700Bold',
  display: 'Manrope_700Bold',
  heavy: 'Manrope_800ExtraBold',
};
export const palette = [
  ['#472B22', '#E99464', '#F1D7AB'],
  ['#162B39', '#487A88', '#C2D8C6'],
  ['#26372A', '#A2B08D', '#E6D8B9'],
  ['#3D283B', '#BA7D9A', '#E8CDB6'],
  ['#232743', '#727DB5', '#D0C2DD'],
  ['#3B3522', '#C7B577', '#E8DDC3'],
];
export function seed(value: string) {
  let n = 0;
  for (const c of value) n = ((n << 5) - n + c.charCodeAt(0)) | 0;
  return Math.abs(n);
}
export function colors(value: string) {
  return palette[seed(value) % palette.length];
}
// The home-indicator inset belongs inside the bar's painted background.
export const TAB_BAR_HEIGHT = 52;
export const PLAYER_DOCK_HEIGHT = 64;
export const PLAYER_DOCK_GAP = 6;
