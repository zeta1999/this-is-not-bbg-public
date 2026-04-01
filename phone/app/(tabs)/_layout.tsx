import React from "react";
import { Tabs } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { colors, fonts } from "../../src/theme";

type TabIcon = React.ComponentProps<typeof Ionicons>["name"];

const TAB_CONFIG: {
  name: string;
  title: string;
  icon: TabIcon;
  iconFocused: TabIcon;
}[] = [
  {
    name: "watchlist",
    title: "Watchlist",
    icon: "trending-up-outline",
    iconFocused: "trending-up",
  },
  {
    name: "orderbook",
    title: "LOB",
    icon: "bar-chart-outline",
    iconFocused: "bar-chart",
  },
  {
    name: "news",
    title: "News",
    icon: "newspaper-outline",
    iconFocused: "newspaper",
  },
  {
    name: "alerts",
    title: "Alerts",
    icon: "notifications-outline",
    iconFocused: "notifications",
  },
  {
    name: "settings",
    title: "Settings",
    icon: "settings-outline",
    iconFocused: "settings",
  },
];

export default function TabLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: true,
        headerStyle: {
          backgroundColor: colors.bg,
        },
        headerTitleStyle: {
          fontFamily: fonts.mono,
          fontWeight: "700",
          fontSize: 16,
          color: colors.amber,
          textTransform: "uppercase",
          letterSpacing: 1,
        },
        headerTintColor: colors.amber,
        headerShadowVisible: false,
        tabBarStyle: {
          backgroundColor: colors.bg,
          borderTopColor: colors.border,
          borderTopWidth: 1,
          height: 88,
          paddingBottom: 28,
          paddingTop: 8,
        },
        tabBarActiveTintColor: colors.amber,
        tabBarInactiveTintColor: colors.muted,
        tabBarLabelStyle: {
          fontFamily: fonts.mono,
          fontWeight: "700",
          fontSize: 10,
          letterSpacing: 0.5,
          textTransform: "uppercase",
        },
      }}
    >
      {TAB_CONFIG.map((tab) => (
        <Tabs.Screen
          key={tab.name}
          name={tab.name}
          options={{
            title: tab.title,
            tabBarIcon: ({ focused, color, size }) => (
              <Ionicons
                name={focused ? tab.iconFocused : tab.icon}
                size={size}
                color={color}
              />
            ),
          }}
        />
      ))}
    </Tabs>
  );
}
