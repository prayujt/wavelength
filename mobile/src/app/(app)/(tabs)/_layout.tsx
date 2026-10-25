import { Tabs } from 'expo-router';
import { AudioLines, House, Library, Search } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { View } from 'react-native';
import { color, font } from '../../../lib/theme';
export default function TabLayout() {
  const inset = useSafeAreaInsets();
  const screens = [
    { name: 'home', title: 'Home', Icon: House },
    { name: 'search', title: 'Discover', Icon: Search },
    { name: 'library', title: 'Library', Icon: Library },
    { name: 'activity', title: 'Activity', Icon: AudioLines },
  ];
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        sceneStyle: { backgroundColor: color.bg },
        tabBarActiveTintColor: color.accent,
        tabBarInactiveTintColor: color.faint,
        tabBarStyle: {
          backgroundColor: '#0C1513',
          borderTopColor: '#23302A',
          borderTopWidth: 1,
          height: 72 + inset.bottom,
          paddingBottom: Math.max(inset.bottom, 10),
          paddingTop: 9,
        },
        tabBarLabelStyle: { fontFamily: font.medium, fontSize: 10, marginTop: 4 },
        animation: 'fade',
      }}
    >
      {screens.map(({ name, title, Icon }) => (
        <Tabs.Screen
          key={name}
          name={name}
          options={{
            title,
            tabBarIcon: ({ color: tint, focused }) => (
              <View style={{ alignItems: 'center', gap: 4 }}>
                <Icon color={tint} size={23} strokeWidth={focused ? 2 : 1.6} />
                <View
                  style={{
                    width: 4,
                    height: 4,
                    borderRadius: 2,
                    backgroundColor: focused ? color.accent : 'transparent',
                  }}
                />
              </View>
            ),
          }}
        />
      ))}
    </Tabs>
  );
}
