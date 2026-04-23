package bus

import (
	"testing"
)

// FuzzTopicRouter exercises the glob-pattern matcher under arbitrary
// input. Invariants:
//   - Subscribe with any pattern must not panic (path.Match handles
//     malformed patterns by returning an error, which the router
//     swallows — we assert it stays quiet).
//   - Publish with any topic must not panic and must not leak to a
//     subscriber whose pattern does not match.
func FuzzTopicRouter(f *testing.F) {
	f.Add("ohlc.binance.BTCUSDT", "ohlc.*.*")
	f.Add("news", "news")
	f.Add("x.y.z", "x.y.*")
	f.Add("weird", "[") // malformed glob — path.Match returns ErrBadPattern
	f.Add("", "")
	f.Add("a/b", "a/b") // path separator — path.Match treats `/` specially

	f.Fuzz(func(t *testing.T, topic, pattern string) {
		b := New(8)
		sub := b.Subscribe(4, pattern)
		defer b.Unsubscribe(sub)

		// Publish should never panic for any topic string.
		b.Publish(Message{Topic: topic, Payload: "x"})

		// Drain channel non-blockingly and assert any delivered message
		// has a matching pattern (self-consistency).
		select {
		case m := <-sub.C:
			if ok := matchesAny(m.Topic, []string{pattern}); !ok {
				t.Fatalf("delivered topic %q does not match pattern %q", m.Topic, pattern)
			}
		default:
			// No delivery is fine: either pattern doesn't match or the
			// pattern was malformed.
		}
	})
}
