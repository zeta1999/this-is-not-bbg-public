import { useEffect, useRef, useCallback, useState } from "react";

// ---------------------------------------------------------------------------
// Tauri event listener hook
// ---------------------------------------------------------------------------

/**
 * Hook that subscribes to Tauri events emitted from the Rust backend.
 * Returns the latest payload and a manual refresh trigger.
 *
 * When running outside the Tauri webview (e.g. plain browser during dev),
 * the import will fail gracefully and the hook becomes a no-op.
 */
export function useTauriEvent<T = unknown>(eventName: string): {
  data: T | null;
  error: string | null;
} {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const unlistenRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function setup() {
      try {
        // Dynamic import so the app doesn't crash in a regular browser
        const { listen } = await import("@tauri-apps/api/event");
        const unlisten = await listen<T>(eventName, (event) => {
          if (!cancelled) {
            setData(event.payload);
            setError(null);
          }
        });
        unlistenRef.current = unlisten;
      } catch (err) {
        if (!cancelled) {
          setError(
            `Failed to listen to event "${eventName}": ${String(err)}`
          );
        }
      }
    }

    setup();

    return () => {
      cancelled = true;
      unlistenRef.current?.();
    };
  }, [eventName]);

  return { data, error };
}

/**
 * Hook that invokes a Tauri command and returns a refresh function.
 */
export function useTauriCommand<T = unknown>(
  command: string,
  args?: Record<string, unknown>
): {
  data: T | null;
  loading: boolean;
  error: string | null;
  refresh: () => void;
} {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  const refresh = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    async function invoke() {
      try {
        const { invoke: tauriInvoke } = await import(
          "@tauri-apps/api/core"
        );
        const result = (await tauriInvoke(command, args ?? {})) as T;
        if (!cancelled) {
          setData(result);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(String(err));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    invoke();

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [command, tick]);

  return { data, loading, error, refresh };
}
