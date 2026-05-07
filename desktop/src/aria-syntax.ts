// Aria DSL tokenizer for the script-editor cell.
//
// Pure module so the highlighter is testable in isolation. The
// editor (CellGridPanel) imports both `tokenizeAria` and
// `ARIA_COLORS`. Lossless: concatenating every token's `text`
// rebuilds the input string byte-for-byte; this is what keeps
// the transparent-textarea-over-styled-pre overlay aligned.

import { colors } from "./styles/theme";

export const ARIA_KEYWORDS = new Set([
  "let", "contract", "one", "scale", "max", "min", "when", "spot",
  "call", "put", "if", "then", "else", "in", "out", "and", "or",
  "not", "barrier", "up", "down", "knockin", "knockout", "true", "false",
  "rebate", "rate", "vol", "carry", "strike", "maturity",
]);

export type AriaTokKind =
  | "kw"      // keyword: amber
  | "num"     // numeric literal: cyan
  | "date"    // ISO date YYYY-MM-DD: yellow
  | "str"     // double-quoted string: green
  | "comment" // line comment (//, --, #): dim
  | "pipe"    // |> operator: amber
  | "op"      // arithmetic / comparison: white
  | "punct"   // brackets / commas: dim
  | "ident"   // identifier: white
  | "ws";     // whitespace: white

export interface AriaTok {
  kind: AriaTokKind;
  text: string;
}

// Strict-but-tolerant: every input character ends up in some
// token. Unrecognised single characters fall through as `ident`
// so concat(tokens.map(t => t.text)) === input is invariant.
export function tokenizeAria(src: string): AriaTok[] {
  const out: AriaTok[] = [];
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];

    // Whitespace run.
    if (c === " " || c === "\t" || c === "\n" || c === "\r") {
      let j = i;
      while (j < n && /\s/.test(src[j])) j++;
      out.push({ kind: "ws", text: src.slice(i, j) });
      i = j;
      continue;
    }

    // Line comment — accept //, --, #. Aria docs disagree on the
    // canonical lead-in so accept all three.
    if (
      (c === "/" && src[i + 1] === "/") ||
      (c === "-" && src[i + 1] === "-") ||
      c === "#"
    ) {
      let j = i;
      while (j < n && src[j] !== "\n") j++;
      out.push({ kind: "comment", text: src.slice(i, j) });
      i = j;
      continue;
    }

    // String literal.
    if (c === '"') {
      let j = i + 1;
      while (j < n && src[j] !== '"') {
        if (src[j] === "\\" && j + 1 < n) j++;
        j++;
      }
      if (j < n) j++;
      out.push({ kind: "str", text: src.slice(i, j) });
      i = j;
      continue;
    }

    // Date YYYY-MM-DD before the number/op fork so 2025-12-31 isn't
    // parsed as `2025` `-` `12` ...
    const dateMatch = /^\d{4}-\d{2}-\d{2}/.exec(src.slice(i));
    if (dateMatch) {
      out.push({ kind: "date", text: dateMatch[0] });
      i += dateMatch[0].length;
      continue;
    }

    // Number (integer / decimal / exponent).
    if ((c >= "0" && c <= "9") || (c === "." && src[i + 1] >= "0" && src[i + 1] <= "9")) {
      const numMatch = /^\d+(\.\d+)?([eE][+-]?\d+)?/.exec(src.slice(i));
      if (numMatch) {
        out.push({ kind: "num", text: numMatch[0] });
        i += numMatch[0].length;
        continue;
      }
    }

    // Pipe operator |>.
    if (c === "|" && src[i + 1] === ">") {
      out.push({ kind: "pipe", text: "|>" });
      i += 2;
      continue;
    }

    // Identifier / keyword.
    if ((c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || c === "_") {
      let j = i;
      while (j < n && /[A-Za-z0-9_]/.test(src[j])) j++;
      const text = src.slice(i, j);
      out.push({ kind: ARIA_KEYWORDS.has(text.toLowerCase()) ? "kw" : "ident", text });
      i = j;
      continue;
    }

    if ("+-*/<>=!^".includes(c)) {
      out.push({ kind: "op", text: c });
      i++;
      continue;
    }
    if ("(),[]{}".includes(c)) {
      out.push({ kind: "punct", text: c });
      i++;
      continue;
    }

    out.push({ kind: "ident", text: c });
    i++;
  }
  return out;
}

export const ARIA_COLORS: Record<AriaTokKind, string> = {
  kw:      colors.amber,
  num:     colors.cyan,
  date:    colors.yellow,
  str:     colors.green,
  comment: colors.dimText,
  pipe:    colors.amber,
  op:      colors.white,
  punct:   colors.dimText,
  ident:   colors.white,
  ws:      colors.white,
};
