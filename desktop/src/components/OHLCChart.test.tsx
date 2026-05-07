import { describe, it, expect, vi, beforeAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { OHLCChart } from "./OHLCChart";
import type { InstrumentData } from "../store";

// lightweight-charts touches WebGL/Canvas APIs jsdom doesn't ship.
// Stub it to a minimal shape so the component renders without
// blowing up; the chart-render branch is exercised in browser
// builds, not unit tests.
vi.mock("lightweight-charts", () => {
  const noop = () => {};
  const series = { setData: noop };
  const chart = {
    addCandlestickSeries: () => series,
    applyOptions: noop,
    timeScale: () => ({ fitContent: noop }),
    remove: noop,
  };
  return {
    createChart: () => chart,
    ColorType: { Solid: "solid" },
  };
});

beforeAll(() => {
  // ResizeObserver is referenced in the chart effect; jsdom doesn't
  // implement it.
  (global as any).ResizeObserver = class { observe(){} disconnect(){} unobserve(){} };
});

const mkInst = (
  instrument: string,
  exchange: string,
  tfs: string[] = ["1m"],
): InstrumentData => ({
  instrument,
  exchange,
  timeframes: new Map(tfs.map((tf) => [tf, []])),
  activeTF: tfs[0],
  lastUpdate: Date.now(),
});

describe("OHLCChart sidebar search", () => {
  it("filters the sidebar by substring on instrument or exchange", () => {
    const data = new Map<string, InstrumentData>([
      ["binance|BTCUSDT", mkInst("BTCUSDT", "binance")],
      ["binance|ETHUSDT", mkInst("ETHUSDT", "binance")],
      ["okx|SOLUSDT", mkInst("SOLUSDT", "okx")],
    ]);
    const keys = Array.from(data.keys());
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={keys}
        activeIdx={0}
        setActiveIdx={() => {}}
        cycleTF={() => {}}
      />
    );

    // All three rows visible initially.
    expect(screen.getAllByText(/USDT/).length).toBeGreaterThanOrEqual(3);

    // Type "okx" → only SOLUSDT row should remain. The header
    // still shows the activeIdx instrument (BTCUSDT), so assert
    // on the sidebar's filtered count, not on absence of BTCUSDT
    // anywhere.
    const input = screen.getByPlaceholderText(/search pair/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "okx" } });
    // SOLUSDT survives; ETHUSDT is filtered out.
    expect(screen.getByText("SOLUSDT")).toBeInTheDocument();
    expect(screen.queryByText("ETHUSDT")).toBeNull();
  });

  it("Enter on the search input jumps activeIdx to the first match", () => {
    const data = new Map<string, InstrumentData>([
      ["binance|BTCUSDT", mkInst("BTCUSDT", "binance")],
      ["binance|ETHUSDT", mkInst("ETHUSDT", "binance")],
      ["okx|SOLUSDT", mkInst("SOLUSDT", "okx")],
    ]);
    const keys = Array.from(data.keys());
    const setIdx = vi.fn();
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={keys}
        activeIdx={0}
        setActiveIdx={setIdx}
        cycleTF={() => {}}
      />
    );
    const input = screen.getByPlaceholderText(/search pair/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "ETH" } });
    fireEvent.keyDown(input, { key: "Enter" });
    // The first filtered match is binance|ETHUSDT (index 1 in keys).
    expect(setIdx).toHaveBeenCalledWith(1);
  });

  // Pin the 2026-04-28 fix: yahoo ^VIX-style instruments (only 1d
  // published natively) no longer paint greyed-out 1m/5m/15m/1h/4h
  // pills that silently fail when clicked. We render only TFs the
  // source actually emits + a "(1d only)" italic hint when there
  // is exactly one native TF.
  it("renders only TFs the source actually publishes", () => {
    const data = new Map<string, InstrumentData>([
      ["yahoo|^VIX", mkInst("^VIX", "yahoo", ["1d"])],
    ]);
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={Array.from(data.keys())}
        activeIdx={0}
        setActiveIdx={() => {}}
        cycleTF={() => {}}
      />
    );
    // Active TF button is present.
    expect(screen.getByRole("button", { name: "1d" })).toBeInTheDocument();
    // None of the unavailable TFs render — even greyed.
    for (const tf of ["1m", "5m", "15m", "1h", "4h"]) {
      expect(screen.queryByRole("button", { name: tf })).toBeNull();
    }
  });

  it("shows the '(1d only)' hint when an instrument has a single native TF", () => {
    const data = new Map<string, InstrumentData>([
      ["yahoo|^VIX", mkInst("^VIX", "yahoo", ["1d"])],
    ]);
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={Array.from(data.keys())}
        activeIdx={0}
        setActiveIdx={() => {}}
        cycleTF={() => {}}
      />
    );
    expect(screen.getByText(/\(1d only\)/i)).toBeInTheDocument();
  });

  it("does NOT show the single-TF hint for instruments with multiple TFs", () => {
    const data = new Map<string, InstrumentData>([
      ["binance|BTCUSDT", mkInst("BTCUSDT", "binance", ["1m", "5m", "1h", "1d"])],
    ]);
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={Array.from(data.keys())}
        activeIdx={0}
        setActiveIdx={() => {}}
        cycleTF={() => {}}
      />
    );
    expect(screen.queryByText(/only\)/i)).toBeNull();
    // All four available TFs show as buttons.
    for (const tf of ["1m", "5m", "1h", "1d"]) {
      expect(screen.getByRole("button", { name: tf })).toBeInTheDocument();
    }
    // 4h / 15m are NOT in availTFs for this fixture, so no button.
    expect(screen.queryByRole("button", { name: "4h" })).toBeNull();
    expect(screen.queryByRole("button", { name: "15m" })).toBeNull();
  });

  it("Esc clears the search field", () => {
    const data = new Map<string, InstrumentData>([
      ["binance|BTCUSDT", mkInst("BTCUSDT", "binance")],
    ]);
    const keys = Array.from(data.keys());
    render(
      <OHLCChart
        ohlcData={data}
        ohlcKeys={keys}
        activeIdx={0}
        setActiveIdx={() => {}}
        cycleTF={() => {}}
      />
    );
    const input = screen.getByPlaceholderText(/search pair/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "abc" } });
    expect(input.value).toBe("abc");
    fireEvent.keyDown(input, { key: "Escape" });
    expect(input.value).toBe("");
  });
});
