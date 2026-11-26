import { Tabs } from 'expo-router';
import { House, Library, ListMusic, Search } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Pressable, View } from 'react-native';
import { color, TAB_BAR_HEIGHT } from '../../../lib/theme';
import { Txt } from '../../../components/ui';

const screens = [
  { name: 'home', title: 'Home', Icon: House },
  { name: 'search', title: 'Search', Icon: Search },
  { name: 'library', title: 'Library', Icon: Library },
  { name: 'playlists', title: 'Playlists', Icon: ListMusic },
];

export default function TabLayout() {
  const inset = useSafeAreaInsets();
  return (
    <Tabs
      tabBar={({ state, navigation }) => (
        <View
          testID="bottom-navigation"
          style={{
            backgroundColor: color.bg,
            borderTopColor: color.line,
            borderTopWidth: 1,
            paddingBottom: inset.bottom,
            paddingLeft: inset.left,
            paddingRight: inset.right,
          }}
        >
          <View role="tablist" style={{ height: TAB_BAR_HEIGHT - 1, flexDirection: 'row' }}>
            {screens.map(({ name, title, Icon }) => {
              const route = state.routes.find((route) => route.name === name)!;
              const focused = state.routes[state.index].key === route.key;
              return (
                <Pressable
                  key={name}
                  accessibilityRole="tab"
                  accessibilityLabel={title}
                  accessibilityState={{ selected: focused }}
                  onPress={() => {
                    const event = navigation.emit({
                      type: 'tabPress',
                      target: route.key,
                      canPreventDefault: true,
                    });
                    if (!focused && !event.defaultPrevented)
                      navigation.navigate(route.name, route.params);
                  }}
                  onLongPress={() => navigation.emit({ type: 'tabLongPress', target: route.key })}
                  style={{
                    flex: 1,
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: 2,
                    paddingHorizontal: 4,
                    opacity: focused ? 1 : 0.88,
                  }}
                >
                  <Icon
                    color={focused ? color.accent : color.muted}
                    size={22}
                    strokeWidth={focused ? 2 : 1.6}
                  />
                  <Txt
                    size={13}
                    weight={focused ? 'bold' : 'medium'}
                    numberOfLines={1}
                    maxFontSizeMultiplier={1.4}
                    style={{ lineHeight: 18, color: focused ? color.accent : color.muted }}
                  >
                    {title}
                  </Txt>
                </Pressable>
              );
            })}
          </View>
        </View>
      )}
      screenOptions={{
        headerShown: false,
        sceneStyle: { backgroundColor: color.bg },
        tabBarActiveTintColor: color.accent,
        tabBarInactiveTintColor: color.faint,
        tabBarStyle: { height: TAB_BAR_HEIGHT + inset.bottom },
        animation: 'none',
      }}
    >
      {screens.map(({ name, title }) => (
        <Tabs.Screen key={name} name={name} options={{ title }} />
      ))}
      <Tabs.Screen name="activity" options={{ href: null }} />
    </Tabs>
  );
}
