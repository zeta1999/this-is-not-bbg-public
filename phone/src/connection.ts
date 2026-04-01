// Shared connection state between phone screens.
// Persisted to AsyncStorage so it survives app reloads.
import { Platform } from "react-native";
import AsyncStorage from "@react-native-async-storage/async-storage";

const DEFAULT_URL = Platform.OS === "android" ? "http://10.0.2.2:9474" : "http://localhost:9474";
const STORAGE_KEY = "notbbg_connection";

let _serverUrl = DEFAULT_URL;
let _token = "";
let _listeners: (() => void)[] = [];
let _loaded = false;

// Load persisted state on startup.
AsyncStorage.getItem(STORAGE_KEY).then((raw) => {
  if (raw) {
    try {
      const { url, token } = JSON.parse(raw);
      if (url) _serverUrl = url;
      if (token) _token = token;
      _listeners.forEach((fn) => fn());
    } catch {}
  }
  _loaded = true;
}).catch(() => { _loaded = true; });

export function getServerUrl() { return _serverUrl; }
export function getToken() { return _token; }
export function isLoaded() { return _loaded; }

export function setConnection(url: string, token: string) {
  _serverUrl = url;
  _token = token;
  _listeners.forEach((fn) => fn());
  // Persist.
  AsyncStorage.setItem(STORAGE_KEY, JSON.stringify({ url, token })).catch(() => {});
}

export function onConnectionChange(fn: () => void) {
  _listeners.push(fn);
  return () => { _listeners = _listeners.filter((l) => l !== fn); };
}
