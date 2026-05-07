package views

import "strings"

// MatchesTokenQuery returns true when every whitespace-separated
// token in `query` is a case-insensitive substring of at least one of
// the supplied `fields`. So `"BTC bin"` matches an instrument with
// fields=("BTCUSDT", "binance") because "BTC"⊂"BTCUSDT" and
// "bin"⊂"binance". Empty / whitespace-only queries return false so
// callers can special-case "no filter" without an extra branch.
func MatchesTokenQuery(query string, fields ...string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	tokens := strings.Fields(strings.ToUpper(q))
	if len(tokens) == 0 {
		return false
	}
	hays := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		hays = append(hays, strings.ToUpper(f))
	}
	if len(hays) == 0 {
		return false
	}
	for _, tok := range tokens {
		ok := false
		for _, h := range hays {
			if strings.Contains(h, tok) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
