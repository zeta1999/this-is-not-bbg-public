import * as SecureStore from "expo-secure-store";
import { ServerConfig } from "../types";

const KEYS = {
  SERVER_CONFIG: "notbbg_server_config",
  SESSION_TOKEN: "notbbg_session_token",
  NOTIFICATION_PREFS: "notbbg_notification_prefs",
} as const;

// ---------------------------------------------------------------------------
// Server configuration
// ---------------------------------------------------------------------------

export async function saveServerConfig(config: ServerConfig): Promise<void> {
  await SecureStore.setItemAsync(
    KEYS.SERVER_CONFIG,
    JSON.stringify(config),
  );
}

export async function getServerConfig(): Promise<ServerConfig | null> {
  const raw = await SecureStore.getItemAsync(KEYS.SERVER_CONFIG);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as ServerConfig;
  } catch {
    return null;
  }
}

export async function clearServerConfig(): Promise<void> {
  await SecureStore.deleteItemAsync(KEYS.SERVER_CONFIG);
  await SecureStore.deleteItemAsync(KEYS.SESSION_TOKEN);
}

// ---------------------------------------------------------------------------
// Session token (may be refreshed independently)
// ---------------------------------------------------------------------------

export async function saveSessionToken(token: string): Promise<void> {
  await SecureStore.setItemAsync(KEYS.SESSION_TOKEN, token);
}

export async function getSessionToken(): Promise<string | null> {
  return SecureStore.getItemAsync(KEYS.SESSION_TOKEN);
}

// ---------------------------------------------------------------------------
// Notification preferences
// ---------------------------------------------------------------------------

export interface NotificationPrefs {
  alertsEnabled: boolean;
  newsEnabled: boolean;
  priceAlertsEnabled: boolean;
}

const DEFAULT_PREFS: NotificationPrefs = {
  alertsEnabled: true,
  newsEnabled: true,
  priceAlertsEnabled: true,
};

export async function getNotificationPrefs(): Promise<NotificationPrefs> {
  const raw = await SecureStore.getItemAsync(KEYS.NOTIFICATION_PREFS);
  if (!raw) return DEFAULT_PREFS;
  try {
    return { ...DEFAULT_PREFS, ...JSON.parse(raw) };
  } catch {
    return DEFAULT_PREFS;
  }
}

export async function saveNotificationPrefs(
  prefs: NotificationPrefs,
): Promise<void> {
  await SecureStore.setItemAsync(
    KEYS.NOTIFICATION_PREFS,
    JSON.stringify(prefs),
  );
}
