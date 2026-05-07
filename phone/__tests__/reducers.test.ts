// Pure-reducer tests for phone/src/store.ts. No SSE / fetch /
// AsyncStorage / react-native runtime needed — every helper is
// a transformation we can call with synthetic inputs and assert
// on the output. Captures the bug class behind the live-test
// fixes from 2026-04-28.

import {
  applyPriceTick,
  dedupCellsByAddr,
  parseNewsBackfill,
  parseSanitySnapshot,
  shallowEqualArrays,
  trimTradeSnap,
  upsertAlert,
  upsertFeedStatus,
  upsertLiveNews,
  MAX_ALERT_ENTRIES,
  MAX_NEWS_ITEMS,
  MAX_VISIBLE_TRADES,
} from "../src/reducers";

describe("parseNewsBackfill", () => {
  it("returns empty list for non-array inputs", () => {
    expect(parseNewsBackfill(null)).toEqual([]);
    expect(parseNewsBackfill("not an array")).toEqual([]);
    expect(parseNewsBackfill({})).toEqual([]);
  });

  it("handles mixed casing of payload field names", () => {
    const got = parseNewsBackfill([
      { Title: "uppercase", Source: "X", Published: "2024-01-01T00:00:00Z" },
      { title: "lowercase", source: "Y" },
    ]);
    expect(got).toHaveLength(2);
    expect(got.map((n) => n.title).sort()).toEqual(["lowercase", "uppercase"]);
  });

  it("dedupes by title — only the first occurrence survives", () => {
    const got = parseNewsBackfill([
      { Title: "same headline", Source: "A", Published: "2024-01-01T00:00:00Z" },
      { Title: "same headline", Source: "B", Published: "2024-02-01T00:00:00Z" },
      { Title: "different",     Source: "C" },
    ]);
    expect(got).toHaveLength(2);
  });

  it("sorts newest first by timestamp", () => {
    const got = parseNewsBackfill([
      { Title: "old",   Published: "2020-01-01T00:00:00Z" },
      { Title: "new",   Published: "2024-01-01T00:00:00Z" },
      { Title: "older", Published: "2019-01-01T00:00:00Z" },
    ]);
    expect(got.map((n) => n.title)).toEqual(["new", "old", "older"]);
  });

  it("skips records with empty title", () => {
    const got = parseNewsBackfill([
      { Title: "" }, { title: "" }, { Title: "valid" },
    ]);
    expect(got).toHaveLength(1);
    expect(got[0].title).toBe("valid");
  });

  it("caps the result at MAX_NEWS_ITEMS", () => {
    const big = Array.from({ length: MAX_NEWS_ITEMS + 50 }, (_, i) => ({
      Title: `t${i}`,
    }));
    expect(parseNewsBackfill(big)).toHaveLength(MAX_NEWS_ITEMS);
  });
});

describe("upsertLiveNews", () => {
  it("prepends a new item", () => {
    const got = upsertLiveNews(
      [{ title: "old", source: "", body: "", url: "", tickers: [], timestamp: 1 }],
      { Title: "new", Source: "X" },
      2,
    );
    expect(got[0].title).toBe("new");
    expect(got).toHaveLength(2);
  });

  it("returns prev unchanged when title already present", () => {
    const prev = [{ title: "dup", source: "", body: "", url: "", tickers: [], timestamp: 1 }];
    const got = upsertLiveNews(prev, { Title: "dup" }, 2);
    expect(got).toBe(prev);
  });

  it("ignores empty-title payloads", () => {
    const prev: any[] = [];
    expect(upsertLiveNews(prev, {}, 1)).toBe(prev);
    expect(upsertLiveNews(prev, { Title: "" }, 1)).toBe(prev);
  });

  it("sorts by timestamp DESC when published is older than existing items", () => {
    // A backfill replay races the live arrival — even if the new
    // item carries an older Published date, the list must stay
    // newest-first.
    const prev = [
      { title: "newer", source: "", body: "", url: "", tickers: [], timestamp: 1000 },
    ];
    const got = upsertLiveNews(
      prev,
      { Title: "older", Published: new Date(500_000).toISOString() },
      9999,
    );
    expect(got.map((n) => n.title)).toEqual(["newer", "older"]);
    expect(got[1].timestamp).toBe(500); // 500_000ms / 1000 = 500s
  });

  it("uses payload.Published when present, falls back to nowSec", () => {
    const got = upsertLiveNews(
      [],
      { Title: "live", Published: new Date(2_000_000).toISOString() },
      9999,
    );
    expect(got[0].timestamp).toBe(2000);
  });
});

describe("dedupCellsByAddr", () => {
  it("collapses overlapping (row,col) — last write wins", () => {
    const got = dedupCellsByAddr([
      { address: { row: 2, col: 0 }, value: "old" },
      { address: { row: 2, col: 0 }, value: "new" },
      { address: { row: 2, col: 1 }, value: "other" },
    ]);
    expect(got).toHaveLength(2);
    const r2c0 = got.find((c) => c.address.row === 2 && c.address.col === 0);
    expect(r2c0!.value).toBe("new");
  });

  it("preserves uniquely-addressed cells", () => {
    const cells = [
      { address: { row: 0, col: 0 }, x: 1 },
      { address: { row: 1, col: 0 }, x: 2 },
      { address: { row: 1, col: 1 }, x: 3 },
    ];
    expect(dedupCellsByAddr(cells)).toHaveLength(3);
  });

  it("handles empty input", () => {
    expect(dedupCellsByAddr([])).toEqual([]);
  });
});

describe("upsertAlert", () => {
  it("prepends and caps at MAX_ALERT_ENTRIES", () => {
    const prev = Array.from({ length: MAX_ALERT_ENTRIES }, (_, i) => ({
      id: String(i), message: "", severity: "info", timestamp: i,
    }));
    const got = upsertAlert(prev, { ID: "new", Message: "boom", Severity: "critical" }, 999);
    expect(got).toHaveLength(MAX_ALERT_ENTRIES);
    expect(got[0].id).toBe("new");
    expect(got[0].severity).toBe("critical");
  });

  it("falls back to nowMs when payload has no id", () => {
    const got = upsertAlert([], { Message: "x" }, 12345);
    expect(got[0].id).toBe("12345");
  });
});

describe("upsertFeedStatus", () => {
  it("replaces existing entry by name", () => {
    const prev = [
      { name: "binance", state: "connected", latencyMs: 100, errorCount: 0 },
      { name: "kraken",  state: "connected", latencyMs: 50,  errorCount: 0 },
    ];
    const got = upsertFeedStatus(prev, { Name: "binance", State: "stale", LatencyMs: 999 });
    expect(got).toHaveLength(2);
    const bn = got.find((f) => f.name === "binance")!;
    expect(bn.state).toBe("stale");
    expect(bn.latencyMs).toBe(999);
  });

  it("inserts new entries sorted alphabetically by name", () => {
    const got = upsertFeedStatus(
      [{ name: "kraken", state: "connected", latencyMs: 0, errorCount: 0 }],
      { Name: "binance", State: "connected" },
    );
    expect(got.map((f) => f.name)).toEqual(["binance", "kraken"]);
  });
});

describe("applyPriceTick", () => {
  it("first tick has change=0 (no baseline)", () => {
    const got = applyPriceTick(undefined, {
      Instrument: "BTCUSDT", Exchange: "binance", Close: 78000,
    }, 1000);
    expect(got).not.toBeNull();
    expect(got!.change).toBe(0);
    expect(got!.price).toBe(78000);
  });

  it("computes change% relative to prior price", () => {
    const prev = {
      instrument: "BTCUSDT", exchange: "binance",
      price: 100, change: 0, timeframe: "1m", lastUpdate: 0,
    };
    const got = applyPriceTick(prev, {
      Instrument: "BTCUSDT", Exchange: "binance", Close: 110,
    }, 1000)!;
    expect(got.change).toBeCloseTo(10, 6);
  });

  it("returns null when payload is missing fields", () => {
    expect(applyPriceTick(undefined, {}, 0)).toBeNull();
    expect(applyPriceTick(undefined, { Close: 100 }, 0)).toBeNull();
    expect(applyPriceTick(undefined, { Instrument: "X", Exchange: "Y" }, 0)).toBeNull();
    expect(applyPriceTick(undefined, { Instrument: "X", Exchange: "Y", Close: NaN }, 0)).toBeNull();
  });

  it("preserves timeframe + carries timestamp", () => {
    const got = applyPriceTick(undefined, {
      Instrument: "BTCUSDT", Exchange: "binance", Close: 78000, Timeframe: "5m",
    }, 12345)!;
    expect(got.timeframe).toBe("5m");
    expect(got.lastUpdate).toBe(12345);
  });
});

describe("trimTradeSnap", () => {
  it("returns the input when it's at or below MAX_VISIBLE_TRADES", () => {
    const trades = Array.from({ length: 10 }, (_, i) => ({
      Price: i, Quantity: 1, Side: "buy", Timestamp: "",
    }));
    expect(trimTradeSnap(trades)).toBe(trades);
  });

  it("keeps the LAST MAX_VISIBLE_TRADES (newest), not the first", () => {
    const trades = Array.from({ length: 100 }, (_, i) => ({
      Price: i, Quantity: 1, Side: "buy", Timestamp: "",
    }));
    const got = trimTradeSnap(trades);
    expect(got).toHaveLength(MAX_VISIBLE_TRADES);
    expect(got[0].Price).toBe(100 - MAX_VISIBLE_TRADES);
    expect(got[got.length - 1].Price).toBe(99);
  });

  it("handles undefined / non-array gracefully", () => {
    expect(trimTradeSnap(undefined)).toEqual([]);
    expect(trimTradeSnap("nope" as any)).toEqual([]);
  });
});

describe("parseSanitySnapshot", () => {
  it("returns null for malformed payloads", () => {
    expect(parseSanitySnapshot({}, 0)).toBeNull();
    expect(parseSanitySnapshot({ instrument: "BTC" }, 0)).toBeNull();
    expect(parseSanitySnapshot({ median: 100 }, 0)).toBeNull();
  });

  it("copies typed fields with safe defaults", () => {
    const got = parseSanitySnapshot(
      { instrument: "BTCUSDT", median: 78000, threshold_pct: 0.5 },
      999,
    )!;
    expect(got.instrument).toBe("BTCUSDT");
    expect(got.median).toBe(78000);
    expect(got.threshold_pct).toBe(0.5);
    expect(got.venue_count).toBe(0);
    expect(got.outlier_count).toBe(0);
    expect(got.venues).toEqual([]);
    expect(got.lastUpdate).toBe(999);
  });

  it("preserves venues array when present", () => {
    const venues = [{ exchange: "binance", mid: 100, delta_pct: 0, age_seconds: 0, outlier: false }];
    const got = parseSanitySnapshot(
      { instrument: "X", median: 100, venues },
      0,
    )!;
    expect(got.venues).toBe(venues);
  });
});

describe("shallowEqualArrays", () => {
  it("identical reference is equal", () => {
    const a = ["x"];
    expect(shallowEqualArrays(a, a)).toBe(true);
  });

  it("same content is equal", () => {
    expect(shallowEqualArrays(["a", "b"], ["a", "b"])).toBe(true);
  });

  it("different length is unequal", () => {
    expect(shallowEqualArrays(["a"], ["a", "b"])).toBe(false);
  });

  it("different content is unequal", () => {
    expect(shallowEqualArrays(["a", "b"], ["a", "c"])).toBe(false);
  });

  it("empty arrays are equal", () => {
    expect(shallowEqualArrays([], [])).toBe(true);
  });
});
