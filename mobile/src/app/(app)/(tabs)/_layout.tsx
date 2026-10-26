import { Tabs } from 'expo-router';
import { House, Library, ListMusic, Search } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Pressable, View } from 'react-native';
import { color, TAB_BAR_HEIGHT } from '../../../lib/theme';
import { TabSwipe } from '../../../components/tab-swipe';
import { Txt, useReducedMotion } from '../../../components/ui';
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
        tabBar={({ state, navigation }) => (
          <View
            testID="bottom-navigation"
            style={{
              backgroundColor: '#0C1513',
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
                    }}
                  >
                    <Icon
                      color={focused ? color.accent : color.muted}
                      size={22}
                      strokeWidth={focused ? 2 : 1.6}
                    />
                    <Txt
                      size={13}
                      weight="medium"
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
          animation: reducedMotion ? 'none' : 'shift',
        }}
      >
        {screens.map(({ name, title }) => (
          <Tabs.Screen key={name} name={name} options={{ title }} />
        ))}
        <Tabs.Screen name="activity" options={{ href: null }} />
      </Tabs>
    </TabSwipe>
  );
}
