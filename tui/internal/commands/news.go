package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/notbbg/notbbg/tui/internal/client"
)

// NewsRecord represents a news item for CLI output.
type NewsRecord struct {
	Title     string   `json:"title"`
	Source    string   `json:"source"`
	URL       string   `json:"url,omitempty"`
	Published string   `json:"published"`
	Tickers   []string `json:"tickers,omitempty"`
}

// NewNewsCmd creates the news command with search subcommand.
func NewNewsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "news",
		Short: "Search and browse news",
	}

	cmd.AddCommand(newNewsSearchCmd())
	return cmd
}

func newNewsSearchCmd() *cobra.Command {
	var (
		socket string
		limit  int
		format string
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search news by keyword, ticker, or source",
		Long:  "Search cached news items. Examples: notbbg news search BTC, notbbg news search --limit 20 ethereum",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")

			conn, err := client.ConnectUnix(socket)
			if err != nil {
				return fmt.Errorf("connect to server: %w (is notbbg-server running?)", err)
			}
			defer conn.Close()

			// Subscribe to news topic.
			subMsg, _ := json.Marshal(map[string]any{
				"type":     "subscribe",
				"patterns": []string{"news"},
			})
			if err := conn.WriteFrame(subMsg); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			// Collect news items matching query.
			queryUpper := strings.ToUpper(query)
			var results []NewsRecord
			deadline := time.After(5 * time.Second)

			for len(results) < limit {
				select {
				case <-deadline:
					goto output
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
				if json.Unmarshal(frame, &msg) != nil || msg.Type != "update" || msg.Topic != "news" {
					continue
				}

				var p struct {
					Title     string   `json:"Title"`
					Source    string   `json:"Source"`
					Body      string   `json:"Body"`
					URL       string   `json:"URL"`
					Published string   `json:"Published"`
					Tickers   []string `json:"Tickers"`
				}
				if json.Unmarshal(msg.Payload, &p) != nil {
					continue
				}

				// Match query against title, source, body, tickers.
				searchable := strings.ToUpper(p.Title + " " + p.Source + " " + p.Body + " " + strings.Join(p.Tickers, " "))
				if !strings.Contains(searchable, queryUpper) {
					continue
				}

				ts := p.Published
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					ts = t.Format("2006-01-02 15:04")
				}

				results = append(results, NewsRecord{
					Title:     p.Title,
					Source:    p.Source,
					URL:       p.URL,
					Published: ts,
					Tickers:   p.Tickers,
				})
			}

		output:
			if len(results) == 0 {
				fmt.Fprintf(os.Stderr, "No news matching \"%s\" within 5s.\n", query)
				return nil
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			case "text":
				for _, r := range results {
					tickers := ""
					if len(r.Tickers) > 0 {
						tickers = " [" + strings.Join(r.Tickers, ",") + "]"
					}
					fmt.Printf("%-14s %-16s %s%s\n", r.Source, r.Published, r.Title, tickers)
				}
				return nil
			default:
				return fmt.Errorf("unknown format: %s", format)
			}
		},
	}

	cmd.Flags().StringVarP(&socket, "socket", "s", "", "server socket path")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "max results")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text, json")

	return cmd
}
