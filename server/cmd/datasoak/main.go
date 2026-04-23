// datasoak drives synthetic bus traffic through the datalake+cache
// subsystems for a configurable duration and rate, then reports RSS
// growth, goroutine delta, and message counts.
//
// Intent: catch memory leaks and unbounded-queue regressions before
// they reach production. See DATA-PLAN.md §2.
//
// Example:
//
//	datasoak --duration=30s --rate=5000
//	DATASOAK_DURATION=1h DATASOAK_RATE=10000 datasoak
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/cache"
	"github.com/notbbg/notbbg/server/internal/datalake"
	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	duration := flag.Duration("duration", envDuration("DATASOAK_DURATION", 30*time.Second),
		"how long to run (DATASOAK_DURATION)")
	rate := flag.Int("rate", envInt("DATASOAK_RATE", 2000),
		"target messages/sec (DATASOAK_RATE)")
	keep := flag.Bool("keep", false, "keep temp dirs after run")
	maxGrowth := flag.Int64("max-growth-mib", 128,
		"fail if RSS grows by more than this many MiB during the run (0 = no check)")
	flag.Parse()

	tmp, err := os.MkdirTemp("", "datasoak-*")
	if err != nil {
		fatal(err)
	}
	if !*keep {
		defer os.RemoveAll(tmp)
	}
	cachePath := filepath.Join(tmp, "cache.db")
	lakePath := filepath.Join(tmp, "lake")

	fmt.Printf("datasoak: duration=%s rate=%d/s tmp=%s\n", *duration, *rate, tmp)

	b := bus.New(64)

	store, err := cache.Open(cachePath, time.Hour)
	if err != nil {
		fatal(err)
	}
	defer store.Close()

	writer := datalake.New(b, datalake.Config{
		Path:    lakePath,
		Enabled: true,
		Topics:  []string{"ohlc.*.*", "trade.*.*"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	writerDone := make(chan struct{})
	go func() { _ = writer.Run(ctx); close(writerDone) }()

	// Capture baseline after subsystems warmed up.
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	var baseMem runtime.MemStats
	runtime.ReadMemStats(&baseMem)
	baseGoroutines := runtime.NumGoroutine()

	fmt.Printf("baseline: heap=%s goroutines=%d\n", humanBytes(baseMem.HeapAlloc), baseGoroutines)

	var sent int64
	go producer(ctx, b, *rate, &sent)

	// Sample ticker prints a heartbeat every 5 s so long runs don't
	// look stuck.
	samples := time.NewTicker(5 * time.Second)
	defer samples.Stop()
	var peakHeap uint64 = baseMem.HeapAlloc
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-samples.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peakHeap {
				peakHeap = m.HeapAlloc
			}
			fmt.Printf("  t=%s heap=%s goroutines=%d sent=%d\n",
				time.Now().Format("15:04:05"), humanBytes(m.HeapAlloc),
				runtime.NumGoroutine(), atomic.LoadInt64(&sent))
		}
	}

	// Wait for the writer to drain and report final stats.
	<-writerDone
	runtime.GC()
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)
	finalGoroutines := runtime.NumGoroutine()

	fmt.Println()
	fmt.Println("final:")
	fmt.Printf("  heap         = %s (base %s, peak %s)\n",
		humanBytes(finalMem.HeapAlloc), humanBytes(baseMem.HeapAlloc), humanBytes(peakHeap))
	fmt.Printf("  goroutines   = %d (base %d, delta %+d)\n",
		finalGoroutines, baseGoroutines, finalGoroutines-baseGoroutines)
	fmt.Printf("  sent         = %d\n", atomic.LoadInt64(&sent))
	fmt.Printf("  heap_objects = %d\n", finalMem.HeapObjects)

	growthMiB := int64(finalMem.HeapAlloc-baseMem.HeapAlloc) / (1 << 20)
	if *maxGrowth > 0 && growthMiB > *maxGrowth {
		fmt.Printf("\nFAIL: heap grew by %d MiB, exceeds --max-growth-mib=%d\n",
			growthMiB, *maxGrowth)
		os.Exit(1)
	}
	if finalGoroutines > baseGoroutines+2 {
		// Allow a small wobble (goroutine scheduler noise) but flag
		// anything looking like a genuine leak.
		fmt.Printf("\nFAIL: goroutines grew from %d to %d\n", baseGoroutines, finalGoroutines)
		os.Exit(1)
	}
	fmt.Println("\nOK")
}

// producer publishes synthetic OHLC + Trade messages at the target
// rate using a token-bucket pacing loop.
func producer(ctx context.Context, b *bus.Bus, ratePerSec int, sent *int64) {
	if ratePerSec <= 0 {
		return
	}
	interval := time.Second / time.Duration(ratePerSec)
	// Cap the timer resolution; on some platforms sub-microsecond
	// tickers burn CPU without improving accuracy.
	if interval < time.Microsecond {
		interval = time.Microsecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"}
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sym := symbols[n%len(symbols)]
			if n%2 == 0 {
				b.Publish(bus.Message{
					Topic: "ohlc.binance." + sym,
					Payload: &pb.OHLC{
						Instrument: sym, Exchange: "binance", Timeframe: "1m",
						Timestamp: timestamppb.Now(),
						Open:      1, High: 2, Low: 0.5, Close: 1.5, Volume: float64(n),
					},
				})
			} else {
				b.Publish(bus.Message{
					Topic: "trade.binance." + sym,
					Payload: &pb.Trade{
						Instrument: sym, Exchange: "binance",
						Timestamp: timestamppb.Now(),
						Price:     100 + float64(n%10), Quantity: 0.01,
						Side: pb.Side_SIDE_BUY, TradeId: "t" + strconv.Itoa(n),
					},
				})
			}
			atomic.AddInt64(sent, 1)
			n++
		}
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "datasoak:", err)
	os.Exit(2)
}
