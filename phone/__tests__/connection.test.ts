// Connection module tests. AsyncStorage is mocked by jest-expo via
// react-native-async-storage's official mock.
jest.mock("@react-native-async-storage/async-storage", () =>
  require("@react-native-async-storage/async-storage/jest/async-storage-mock")
);

import { getServerUrl, getToken, setConnection, onConnectionChange } from "../src/connection";

describe("connection state", () => {
  it("starts with a default URL and empty token", () => {
    const url = getServerUrl();
    expect(url.startsWith("http://")).toBe(true);
    // Token is initially empty until pairing.
    expect(getToken()).toBe("");
  });

  it("setConnection updates getters and notifies listeners", () => {
    const seen: string[] = [];
    const off = onConnectionChange(() => seen.push(getToken()));
    setConnection("http://example.test:9474", "tok-123");
    expect(getServerUrl()).toBe("http://example.test:9474");
    expect(getToken()).toBe("tok-123");
    expect(seen).toEqual(["tok-123"]);
    off();
  });

  it("listener unsubscribe stops firing", () => {
    let calls = 0;
    const off = onConnectionChange(() => calls++);
    off();
    setConnection("http://x:9474", "y");
    expect(calls).toBe(0);
  });
});
