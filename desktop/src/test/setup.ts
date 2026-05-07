import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// React Testing Library doesn't auto-cleanup between tests under
// Vitest the way it does under Jest's default preset; do it here
// so DOM nodes don't leak across cases.
afterEach(() => cleanup());

// Stub window.matchMedia for libraries that probe for it (none used
// today, but cheap insurance against jsdom landmines).
if (!window.matchMedia) {
  (window as any).matchMedia = (q: string) => ({
    matches: false,
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    onchange: null,
    dispatchEvent: () => false,
  });
}
