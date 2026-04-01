package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/notbbg/notbbg/tui/internal/client"
)

// NewHistoryCmd creates the history command.
func NewHistoryCmd() *cobra.Command {
	var (
		socket    string
		exchange  string
		format    string
		limit     int
		bucket    string
	)

	cmd := &cobra.Command{
		Use:   "history [instrument]",
		Short: "Query historical data from server cache",
		Long: `Query cached data from the server's BBolt store.

Examples:
  notbbg history BTCUSDT                    # OHLC from binance
  notbbg history BTCUSDT -e yahoo           # OHLC from Yahoo Finance
  notbbg history BTCUSDT -b trades          # Trade history
  notbbg history --bucket news --limit 20   # Recent news items`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instrument := ""
			if len(args) > 0 {
				instrument = args[0]
			}

			conn, err := client.ConnectUnix(socket)
			if err != nil {
				return fmt.Errorf("connect to server: %w (is notbbg-server running?)", err)
			}
			defer conn.Close()

			// Build query: "bucket/exchange/instrument" or "bucket/"
			query := bucket + "/"
			if exchange != "" {
				query += exchange + "/"
			}
			if instrument != "" {
				query += instrument
			}

			queryMsg, _ := json.Marshal(map[string]any{
				"type":  "query",
				"query": query,
				"limit": limit,
			})
			if err := conn.WriteFrame(queryMsg); err != nil {
				return fmt.Errorf("query: %w", err)
			}

			// Read response.
			frame, err := conn.ReadFrame()
			if err != nil {
				return fmt.Errorf("read response: %w", err)
			}

			var resp struct {
				Type    string          `json:"type"`
				Topic   string          `json:"topic"`
				Payload json.RawMessage `json:"payload"`
				Error   string          `json:"error"`
			}
			if err := json.Unmarshal(frame, &resp); err != nil {
				return fmt.Errorf("parse response: %w", err)
			}
			if resp.Error != "" {
				return fmt.Errorf("server error: %s", resp.Error)
			}

			var results []json.RawMessage
			if err := json.Unmarshal(resp.Payload, &results); err != nil {
				return fmt.Errorf("parse results: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintf(os.Stderr, "No data found for query: %s\n", query)
				return nil
			}

			fmt.Fprintf(os.Stderr, "%d records found\n", len(results))

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			case "jsonl":
				enc := json.NewEncoder(os.Stdout)
				for _, r := range results {
					enc.Encode(r)
				}
				return nil
			default:
				return fmt.Errorf("unknown format: %s", format)
			}
		},
	}

	cmd.Flags().StringVarP(&socket, "socket", "s", "", "server socket path")
	cmd.Flags().StringVarP(&exchange, "exchange", "e", "binance", "exchange name")
	cmd.Flags().StringVarP(&format, "format", "f", "jsonl", "output format: json, jsonl")
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "max records")
	cmd.Flags().StringVarP(&bucket, "bucket", "b", "ohlc", "data bucket: ohlc, trades, lob_snapshots, news, alerts")

	return cmd
}
