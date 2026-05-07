import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { NewsPanel } from "./NewsPanel";
import type { NewsItem } from "../store";

// NewsPanel runs an effect that hits /api/v1/news/search; stub fetch
// so the test isn't dependent on a live server or timers.
beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve({ json: () => Promise.resolve([]) } as any)));
});
afterEach(() => vi.unstubAllGlobals());

const mkItem = (over: Partial<NewsItem>): NewsItem => ({
  title: "headline",
  source: "src",
  body: "body text",
  url: "",
  tickers: [],
  timestamp: Math.floor(Date.now() / 1000),
  ...over,
});

describe("NewsPanel", () => {
  it("renders empty state when no items", () => {
    render(<NewsPanel items={[]} />);
    expect(screen.getByText(/Waiting for news/i)).toBeInTheDocument();
  });

  it("renders one row per item", () => {
    const items = [
      mkItem({ title: "Bitcoin hits ATH" }),
      mkItem({ title: "ETH merge anniversary" }),
    ];
    render(<NewsPanel items={items} />);
    expect(screen.getByText("Bitcoin hits ATH")).toBeInTheDocument();
    expect(screen.getByText("ETH merge anniversary")).toBeInTheDocument();
    expect(screen.getByText("2 items")).toBeInTheDocument();
  });

  it("clicking a row opens article detail; Esc closes it", () => {
    const items = [
      mkItem({ title: "first", body: "first body" }),
      mkItem({ title: "second", body: "second body" }),
    ];
    render(<NewsPanel items={items} />);

    // Click the second row → detail view shows its body and a Back
    // button; the detail view is the only render path the user
    // sees on Enter / click, so test that path directly.
    fireEvent.click(screen.getByText("second"));
    expect(screen.getByText("second body")).toBeInTheDocument();
    expect(screen.getByText(/Back \(Esc\)/)).toBeInTheDocument();

    // Esc returns to the list view.
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByText(/Back \(Esc\)/)).toBeNull();
    expect(screen.getByText("2 items")).toBeInTheDocument();
  });

  it("j moves the cursor into the list (selectedIdx 0 = first item)", () => {
    const items = [
      mkItem({ title: "alpha", body: "alpha body" }),
      mkItem({ title: "beta", body: "beta body" }),
    ];
    render(<NewsPanel items={items} />);
    // Initial selectedIdx = -1 (list view). Pressing j advances to 0,
    // which puts the panel into detail view for the first item.
    fireEvent.keyDown(window, { key: "j" });
    expect(screen.getByText("alpha body")).toBeInTheDocument();
  });

  it("renders clickable URL in detail when present", () => {
    const items = [mkItem({ title: "linked", url: "https://example.com" })];
    render(<NewsPanel items={items} />);
    fireEvent.click(screen.getByText("linked"));
    const link = screen.getByRole("link") as HTMLAnchorElement;
    expect(link.href).toBe("https://example.com/");
    expect(link.target).toBe("_blank");
  });
});
