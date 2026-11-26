export const color = {
  bg: '#101216',
  surface: '#181B21',
  raised: '#222630',
  line: '#303642',
  text: '#F2F4F8',
  muted: '#ACB3C1',
  faint: '#828C9D',
  accent: '#B7C8FF',
  accentInk: '#19264C',
  gold: '#E0BC92',
  danger: '#F0A49B',
  overlay: '#090B10CC',
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
export const PLAYER_DOCK_HEIGHT = 60;
export const PLAYER_DOCK_GAP = 6;
