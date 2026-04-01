// Package commands provides CLI command implementations for non-interactive use.
package commands

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/notbbg/notbbg/tui/internal/client"
)

// OHLCRecord represents a single OHLC record for export.
type OHLCRecord struct {
	Instrument string    `json:"instrument"`
	Exchange   string    `json:"exchange"`
	Timeframe  string    `json:"timeframe"`
	Timestamp  time.Time `json:"timestamp"`
	Open       float64   `json:"open"`
	High       float64   `json:"high"`
	Low        float64   `json:"low"`
	Close      float64   `json:"close"`
	Volume     float64   `json:"volume"`
}

// NewExportCmd creates the export command.
func NewExportCmd() *cobra.Command {
	var (
		format    string
		timeframe string
		exchange  string
		socket    string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "export [type] [instrument]",
		Short: "Export market data (JSON, JSONL, CSV)",
		Long:  "Export cached market data. Types: ohlc, trades. Formats: json, jsonl, csv.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dataType := args[0]
			instrument := args[1]

			// Connect to server.
			conn, err := client.ConnectUnix(socket)
			if err != nil {
				return fmt.Errorf("connect to server: %w (is notbbg-server running?)", err)
			}
			defer conn.Close()

			// Subscribe to the relevant topic to capture live data.
			var topic string
			switch dataType {
			case "ohlc":
				topic = fmt.Sprintf("ohlc.%s.%s", exchange, instrument)
			case "trades":
				topic = fmt.Sprintf("trade.%s.%s", exchange, instrument)
			default:
				return fmt.Errorf("unknown type: %s (use ohlc or trades)", dataType)
			}

			subMsg := map[string]any{
				"type":     "subscribe",
				"patterns": []string{topic},
			}
			data, _ := json.Marshal(subMsg)
			if err := conn.WriteFrame(data); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			// Collect records.
			var records []OHLCRecord
			deadline := time.After(5 * time.Second)

			for len(records) < limit {
				select {
				case <-deadline:
					goto export
				default:
				}

				frame, err := conn.ReadFrame()
				if err != nil {
					break
				}

				var msg struct {
					Type    string          `json:"type"`
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				}
				if json.Unmarshal(frame, &msg) != nil || msg.Type != "update" {
					continue
				}

				var p struct {
					Instrument string  `json:"Instrument"`
					Exchange   string  `json:"Exchange"`
					Timeframe  string  `json:"Timeframe"`
					Timestamp  string  `json:"Timestamp"`
					Open       float64 `json:"Open"`
					High       float64 `json:"High"`
					Low        float64 `json:"Low"`
					Close      float64 `json:"Close"`
					Volume     float64 `json:"Volume"`
				}
				if json.Unmarshal(msg.Payload, &p) != nil {
					continue
				}

				ts, _ := time.Parse(time.RFC3339Nano, p.Timestamp)
				records = append(records, OHLCRecord{
					Instrument: p.Instrument,
					Exchange:   p.Exchange,
					Timeframe:  p.Timeframe,
					Timestamp:  ts,
					Open:       p.Open,
					High:       p.High,
					Low:        p.Low,
					Close:      p.Close,
					Volume:     p.Volume,
				})
			}

		export:
			if len(records) == 0 {
				slog.Warn("no data received", "topic", topic, "timeout", "5s")
				fmt.Fprintf(os.Stderr, "No data received for %s on %s within 5s.\n", instrument, exchange)
				fmt.Fprintf(os.Stderr, "Make sure the server is running and the symbol/exchange are correct.\n")
				return nil
			}

			return writeExport(os.Stdout, format, records)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "json", "output format: json, jsonl, csv")
	cmd.Flags().StringVarP(&timeframe, "tf", "t", "1m", "timeframe: 1m, 5m, 15m, 1h, 4h, 1d")
	cmd.Flags().StringVarP(&exchange, "exchange", "e", "binance", "exchange name")
	cmd.Flags().StringVarP(&socket, "socket", "s", "", "server socket path")
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "max records to collect")

	return cmd
}

func writeExport(w io.Writer, format string, records []OHLCRecord) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(records)

	case "jsonl":
		enc := json.NewEncoder(w)
		for _, r := range records {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil

	case "csv":
		cw := csv.NewWriter(w)
		defer cw.Flush()

		if err := cw.Write([]string{"instrument", "exchange", "timeframe", "timestamp", "open", "high", "low", "close", "volume"}); err != nil {
			return err
		}

		for _, r := range records {
			row := []string{
				r.Instrument,
				r.Exchange,
				r.Timeframe,
				r.Timestamp.Format(time.RFC3339),
				fmt.Sprintf("%.2f", r.Open),
				fmt.Sprintf("%.2f", r.High),
				fmt.Sprintf("%.2f", r.Low),
				fmt.Sprintf("%.2f", r.Close),
				fmt.Sprintf("%.6f", r.Volume),
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
