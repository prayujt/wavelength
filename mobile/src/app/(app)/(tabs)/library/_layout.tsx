import { Stack } from 'expo-router';
import { color } from '../../../../lib/theme';

export default function LibraryLayout() {
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        animation: 'none',
        contentStyle: { backgroundColor: color.bg },
      }}
    />
  );
}
