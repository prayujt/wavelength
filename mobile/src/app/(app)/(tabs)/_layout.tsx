import { Tabs } from 'expo-router';
import { House, Library, ListMusic, Search } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { View } from 'react-native';
import { color, font, TAB_BAR_HEIGHT } from '../../../lib/theme';
import { TabSwipe } from '../../../components/tab-swipe';
import { useReducedMotion } from '../../../components/ui';
export default function TabLayout() {
  const inset = useSafeAreaInsets();
  const reducedMotion = useReducedMotion();
  const screens = [
    { name: 'home', title: 'Home', Icon: House },
    { name: 'playlists', title: 'Playlists', Icon: ListMusic },
    { name: 'library', title: 'Library', Icon: Library },
    { name: 'search', title: 'Discover', Icon: Search },
  ];
  return (
    <TabSwipe>
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
            height: TAB_BAR_HEIGHT + inset.bottom,
            paddingBottom: inset.bottom + 4,
            paddingTop: 5,
          },
          tabBarLabelPosition: 'below-icon',
          tabBarLabelStyle: { fontFamily: font.medium, fontSize: 10, lineHeight: 12, marginTop: 1 },
          tabBarIconStyle: { width: 24, height: 24 },
          animation: reducedMotion ? 'none' : 'shift',
        }}
      >
        {screens.map(({ name, title, Icon }) => (
          <Tabs.Screen
            key={name}
            name={name}
            options={{
              title,
              tabBarIcon: ({ color: tint, focused }) => (
                <View style={{ alignItems: 'center' }}>
                  <Icon color={tint} size={21} strokeWidth={focused ? 2 : 1.6} />
                </View>
              ),
            }}
          />
        ))}
        <Tabs.Screen name="activity" options={{ href: null }} />
      </Tabs>
    </TabSwipe>
  );
}
