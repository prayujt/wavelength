import { Stack } from 'expo-router';
import { color } from '../../../../lib/theme';

export default function LibraryLayout() {
  return (
    <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: color.bg } }} />
  );
}
