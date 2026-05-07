package app

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	ohlcLogMu   sync.Mutex
	ohlcLogFile *os.File
)

// ohlcDebugInstruments is the allowlist of "exchange/instrument"
// pairs the realtime + history traces emit lines for. Empty/zero =
// log everything. Set narrow during an active debug session so the
// log stays readable; alignToNow logs every render anyway since
// it's only called for whichever instrument is on the active
// panel.
var ohlcDebugInstruments = map[string]struct{}{
	"binance/BTCUSDT": {},
	"binance/ETHUSDT": {},
}

// ohlcDebugMatch returns true if a given exchange/instrument pair
// passes the trace filter. Used by the realtime + history paths to
// skip the per-tick formatting cost on non-watched instruments.
func ohlcDebugMatch(exchange, instrument string) bool {
	if len(ohlcDebugInstruments) == 0 {
		return true
	}
	_, ok := ohlcDebugInstruments[exchange+"/"+instrument]
	return ok
}

// ohlcDebugMatchTopic is the same filter for topics like
// "ohlc.binance.BTCUSDT" (3-segment) and instrument-key forms like
// "BTCUSDT/binance" used by the history-event applier.
func ohlcDebugMatchTopic(topicOrKey string) bool {
	if len(ohlcDebugInstruments) == 0 {
		return true
	}
	if strings.HasPrefix(topicOrKey, "ohlc.") {
		// ohlc.binance.BTCUSDT → binance/BTCUSDT
		parts := strings.SplitN(topicOrKey, ".", 3)
		if len(parts) == 3 {
			return ohlcDebugMatch(parts[1], parts[2])
		}
		return false
	}
	// instrumentKey form: BTCUSDT/binance → swap to binance/BTCUSDT
	if i := strings.IndexByte(topicOrKey, '/'); i >= 0 {
		inst := topicOrKey[:i]
		ex := topicOrKey[i+1:]
		return ohlcDebugMatch(ex, inst)
	}
	return false
}

// ohlcDebug writes one line to /tmp/notbbg-ohlc.log. Cheap and
// crash-safe (open-once, append). Used to trace timestamps across
// the realtime + backfill + alignment pipeline so the operator can
// see WHERE candles disappear when the chart shows fewer bars than
// the array claims to contain.
func ohlcDebug(format string, args ...any) {
	ohlcLogMu.Lock()
	defer ohlcLogMu.Unlock()
	if ohlcLogFile == nil {
		f, err := os.OpenFile("/tmp/notbbg-ohlc.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		ohlcLogFile = f
	}
	fmt.Fprintf(ohlcLogFile, "%s "+format+"\n",
		append([]any{time.Now().UTC().Format("01-02 15:04:05.000")}, args...)...)
}
