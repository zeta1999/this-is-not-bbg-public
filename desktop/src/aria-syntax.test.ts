import { describe, it, expect } from "vitest";
import { tokenizeAria, ARIA_KEYWORDS, ARIA_COLORS } from "./aria-syntax";

// The tokenizer underpins the script-editor highlight overlay.
// Two contracts matter to the editor:
//   1. Lossless: concat(tokens.text) === input — keeps the
//      transparent textarea aligned with the styled <pre>.
//   2. Token kinds get the right colour from ARIA_COLORS — keeps
//      the "amber keyword / cyan number / yellow date" UX stable.

describe("tokenizeAria", () => {
  it("emits exactly one ws token for a whitespace run", () => {
    const toks = tokenizeAria("  \t \n");
    expect(toks).toHaveLength(1);
    expect(toks[0].kind).toBe("ws");
    expect(toks[0].text).toBe("  \t \n");
  });

  it("recognises every Aria keyword", () => {
    for (const kw of ARIA_KEYWORDS) {
      const toks = tokenizeAria(kw);
      expect(toks, kw).toHaveLength(1);
      expect(toks[0].kind, kw).toBe("kw");
      expect(toks[0].text, kw).toBe(kw);
    }
  });

  it("keywords are case-insensitive", () => {
    const upper = tokenizeAria("LET").map((t) => t.kind);
    const lower = tokenizeAria("let").map((t) => t.kind);
    expect(upper).toEqual(["kw"]);
    expect(lower).toEqual(["kw"]);
  });

  it("identifiers that aren't keywords stay as ident", () => {
    const toks = tokenizeAria("S K myVar BTC_USD");
    const idents = toks.filter((t) => t.kind === "ident");
    expect(idents.map((t) => t.text)).toEqual(["S", "K", "myVar", "BTC_USD"]);
  });

  it("parses ISO dates as date tokens", () => {
    const toks = tokenizeAria("2025-12-31");
    expect(toks).toHaveLength(1);
    expect(toks[0].kind).toBe("date");
    expect(toks[0].text).toBe("2025-12-31");
  });

  it("does NOT split a date into number-minus-number", () => {
    const toks = tokenizeAria("2025-12-31");
    expect(toks.filter((t) => t.kind === "op")).toHaveLength(0);
  });

  it("parses integer / decimal / exponent literals", () => {
    const cases: Array<[string, string]> = [
      ["100", "100"],
      ["100.0", "100.0"],
      ["1e-4", "1e-4"],
      ["1.5E10", "1.5E10"],
    ];
    for (const [src, want] of cases) {
      const toks = tokenizeAria(src);
      expect(toks, src).toHaveLength(1);
      expect(toks[0].kind, src).toBe("num");
      expect(toks[0].text, src).toBe(want);
    }
  });

  it('parses "double-quoted strings" including escape sequences', () => {
    const toks = tokenizeAria('"BTC-USD"');
    expect(toks).toHaveLength(1);
    expect(toks[0].kind).toBe("str");
    expect(toks[0].text).toBe('"BTC-USD"');

    const escaped = tokenizeAria('"a\\"b"');
    expect(escaped).toHaveLength(1);
    expect(escaped[0].text).toBe('"a\\"b"');
  });

  it("recognises all three comment lead-ins (//, --, #)", () => {
    for (const lead of ["//", "--", "#"]) {
      const src = `${lead} comment text`;
      const toks = tokenizeAria(src);
      expect(toks, lead).toHaveLength(1);
      expect(toks[0].kind, lead).toBe("comment");
      expect(toks[0].text, lead).toBe(src);
    }
  });

  it("|> emits a single pipe token, not | + >", () => {
    const toks = tokenizeAria("|>");
    expect(toks).toHaveLength(1);
    expect(toks[0].kind).toBe("pipe");
  });

  it("operators and punctuation get distinct kinds", () => {
    const toks = tokenizeAria("(a + b)");
    const kinds = toks.map((t) => `${t.kind}:${t.text}`);
    expect(kinds).toContain("punct:(");
    expect(kinds).toContain("op:+");
    expect(kinds).toContain("punct:)");
  });

  it("is LOSSLESS — concat(tokens.text) === input", () => {
    const sample = `let K = 100.0
let S = spot("BTC-USD")
contract
  one(USD)
  |> scale(max(S - K, 0.0))
  |> when(2025-12-31)
// trailing comment
`;
    const reconstructed = tokenizeAria(sample).map((t) => t.text).join("");
    expect(reconstructed).toBe(sample);
  });

  it("ARIA_COLORS has an entry for every emitted kind", () => {
    const seen = new Set<string>();
    const sample = `let K = 100 + 1.5e-2 * "x" |> 2025-01-01 // c
foo`;
    for (const tok of tokenizeAria(sample)) seen.add(tok.kind);
    for (const k of seen) {
      expect(ARIA_COLORS[k as keyof typeof ARIA_COLORS], k).toBeDefined();
    }
  });
});
