import React, { useEffect, useState } from "react";
import { View, ActivityIndicator } from "react-native";
import { Redirect } from "expo-router";
import { getToken, isLoaded, onConnectionChange } from "../src/connection";
import { colors } from "../src/theme";

// First-launch routing: send unpaired users straight to Settings
// so the app doesn't open on a confusing empty Watchlist with a
// "DISCONNECTED" banner. Once a token is present we drop them on
// the main tab. We have to wait for AsyncStorage hydration to
// finish before deciding — `isLoaded()` flips after the .then()
// in connection.ts.
export default function Index() {
  const [ready, setReady] = useState(isLoaded());

  useEffect(() => {
    if (ready) return;
    let timer: any;
    const check = () => {
      if (isLoaded()) {
        setReady(true);
        return;
      }
      timer = setTimeout(check, 50);
    };
    check();
    const unsub = onConnectionChange(() => setReady(true));
    return () => {
      if (timer) clearTimeout(timer);
      unsub();
    };
  }, [ready]);

  if (!ready) {
    return (
      <View style={{ flex: 1, backgroundColor: colors.bg, alignItems: "center", justifyContent: "center" }}>
        <ActivityIndicator color={colors.amber} />
      </View>
    );
  }

  if (!getToken()) {
    return <Redirect href="/(tabs)/settings" />;
  }
  return <Redirect href="/(tabs)/watchlist" />;
}
