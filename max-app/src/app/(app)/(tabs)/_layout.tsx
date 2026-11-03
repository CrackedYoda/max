import { Tabs } from 'expo-router';

import { Icon, type IconName } from '@/components/ui/Icon';
import { colors, fonts } from '@/theme';

function tabIcon(name: IconName) {
  return function TabIcon({ focused }: { focused: boolean }) {
    return <Icon name={name} color={focused ? colors.clayText : colors.muted} size={22} />;
  };
}

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.clayText,
        tabBarInactiveTintColor: colors.muted,
        tabBarLabelStyle: { fontFamily: fonts.semibold, fontSize: 11 },
        tabBarStyle: { borderTopColor: colors.line, backgroundColor: colors.surface },
        sceneStyle: { backgroundColor: colors.ivory },
      }}>
      <Tabs.Screen name="home" options={{ title: 'Home', tabBarIcon: tabIcon('home') }} />
      <Tabs.Screen name="send" options={{ title: 'Send', tabBarIcon: tabIcon('send') }} />
      <Tabs.Screen name="receive" options={{ title: 'Receive', tabBarIcon: tabIcon('receive') }} />
      <Tabs.Screen name="ids" options={{ title: 'IDs', tabBarIcon: tabIcon('ids') }} />
    </Tabs>
  );
}
