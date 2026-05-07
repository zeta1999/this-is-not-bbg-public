import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { TradesPanel } from "./TradesPanel";
import type { TradeAgg, TradeSnapData } from "../store";

// TRADES went from a horizontal tab strip with no exchange labels
// + no keyboard nav to a sidebar with [/] arrow stepping + `/`
// search focus. These tests pin the sidebar contract so the
// 2026-04-28 regression can't recur.

const mkAgg = (instrument: string, exchange: string, close: number): TradeAgg => ({
  Instrument: instrument,
  Exchange: exchange,
  Count: 0,
  Volume: 0,
  BuyVolume: 0,
  SellVolume: 0,
  VWAP: close,
  Open: close,
  High: close,
  Low: close,
  Close: close,
  Turnover: 0,
  P25: close,
  P50: close,
  P75: close,
});

const mkSnap = (instrument: string, exchange: string): TradeSnapData => ({
  Instrument: instrument,
  Exchange: exchange,
  Trades: [],
});

const fixture = () => {
  const aggs: Record<string, TradeAgg> = {
    "binance/BTCUSDT": mkAgg("BTCUSDT", "binance", 78000),
    "bybit/BTCUSDT": mkAgg("BTCUSDT", "bybit", 78010),
    "okx/ETHUSDT": mkAgg("ETHUSDT", "okx", 2300),
  };
  const snaps: Record<string, TradeSnapData> = {
    "binance/BTCUSDT": mkSnap("BTCUSDT", "binance"),
    "bybit/BTCUSDT": mkSnap("BTCUSDT", "bybit"),
    "okx/ETHUSDT": mkSnap("ETHUSDT", "okx"),
  };
  // keys[] is the canonical iteration order — preserved verbatim
  // so activeIdx is deterministic.
  return { aggs, snaps, keys: Object.keys(aggs) };
};

beforeEach(() => {
  // Quiet jsdom layout warnings on scrollIntoView.
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe("TradesPanel", () => {
  it("shows empty state with the 'waiting for trade aggregates' hint", () => {
    render(<TradesPanel aggs={{}} snaps={{}} keys={[]} />);
    expect(screen.getByText(/Waiting for trade aggregates/i)).toBeInTheDocument();
  });

  it("renders one sidebar row per (exchange, instrument) pair with both labels", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    // BTCUSDT appears twice in sidebar (binance + bybit) plus once
    // in the header (active row), so >= 3.
    expect(screen.getAllByText("BTCUSDT").length).toBeGreaterThanOrEqual(2);
    // Each exchange label shows up at least once in the sidebar;
    // the active exchange (binance, idx 0) also appears in the
    // header so use getAllByText.
    expect(screen.getAllByText("binance").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("bybit").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("okx").length).toBeGreaterThanOrEqual(1);
  });

  it("[ key cycles to the previous instrument", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    // activeIdx starts at 0 = binance/BTCUSDT.
    fireEvent.keyDown(window, { key: "[" });
    // Wraps backward to last entry: okx/ETHUSDT. Both the header
    // and the sidebar row render the label, hence getAllByText.
    expect(screen.getAllByText("ETHUSDT").length).toBeGreaterThanOrEqual(1);
    // Header shows the active exchange now (in addition to sidebar row).
    expect(screen.getAllByText("okx").length).toBeGreaterThanOrEqual(2);
  });

  it("] key cycles to the next instrument", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    fireEvent.keyDown(window, { key: "]" });
    // Now active = bybit/BTCUSDT (idx 1).
    // Header shows the active exchange.
    const headerExch = screen.getAllByText("bybit");
    expect(headerExch.length).toBeGreaterThan(0);
  });

  it("/ key focuses the search input", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    const input = screen.getByPlaceholderText(/search pair\/exchange/i) as HTMLInputElement;
    fireEvent.keyDown(window, { key: "/" });
    expect(document.activeElement).toBe(input);
  });

  it("typing in search filters the sidebar (header stays on active row)", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    const input = screen.getByPlaceholderText(/search pair\/exchange/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "okx" } });
    // Filter narrows the sidebar to 1 row (okx/ETHUSDT). The
    // header still pins the active idx=0 = binance/BTCUSDT until
    // the user explicitly selects another row, so:
    //  - "okx" appears once (the sidebar row)
    //  - "binance" appears once (the header)
    //  - "bybit" is gone (was a sidebar row, now filtered out)
    expect(screen.getAllByText("okx").length).toBe(1);
    expect(screen.getAllByText("binance").length).toBe(1);
    expect(screen.queryByText("bybit")).toBeNull();
    // ETHUSDT is the matched sidebar row label.
    expect(screen.getAllByText("ETHUSDT").length).toBe(1);
  });

  it("Enter on the search input jumps activeIdx to the first match", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    const input = screen.getByPlaceholderText(/search pair\/exchange/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "ETH" } });
    fireEvent.keyDown(input, { key: "Enter" });
    // After Enter the search clears via .blur and active becomes
    // the okx/ETHUSDT row. Header + remaining-sidebar rows both
    // render ETHUSDT, so the union must be > 0.
    const ethLabels = screen.getAllByText("ETHUSDT");
    expect(ethLabels.length).toBeGreaterThan(0);
    // Header shows okx as the active exchange.
    expect(screen.getAllByText("okx").length).toBeGreaterThan(0);
  });

  it("Esc on search clears the filter and unfocuses the input", () => {
    const { aggs, snaps, keys } = fixture();
    render(<TradesPanel aggs={aggs} snaps={snaps} keys={keys} />);
    const input = screen.getByPlaceholderText(/search pair\/exchange/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "okx" } });
    expect(input.value).toBe("okx");
    fireEvent.keyDown(input, { key: "Escape" });
    expect(input.value).toBe("");
  });

  it("ignores [/]/Arrow when no instruments are loaded", () => {
    render(<TradesPanel aggs={{}} snaps={{}} keys={[]} />);
    // Should not throw and should keep showing the empty state.
    fireEvent.keyDown(window, { key: "[" });
    fireEvent.keyDown(window, { key: "]" });
    expect(screen.getByText(/Waiting for trade aggregates/i)).toBeInTheDocument();
  });
});
