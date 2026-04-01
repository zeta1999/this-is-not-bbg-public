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

// FeedStatus represents a feed's status for CLI output.
type FeedStatus struct {
	Name       string  `json:"name"`
	State      string  `json:"state"`
	LatencyMs  float64 `json:"latency_ms"`
	ErrorCount uint64  `json:"error_count"`
	LastUpdate string  `json:"last_update"`
}

// NewFeedsCmd creates the feeds command.
func NewFeedsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feeds",
		Short: "Manage and monitor data feeds",
	}

	cmd.AddCommand(newFeedsListCmd())
	return cmd
}

func newFeedsListCmd() *cobra.Command {
	var (
		socket string
		format string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active feeds with status",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := client.ConnectUnix(socket)
			if err != nil {
				return fmt.Errorf("connect to server: %w (is notbbg-server running?)", err)
			}
			defer conn.Close()

			// Subscribe to feed status.
			subMsg, _ := json.Marshal(map[string]any{
				"type":     "subscribe",
				"patterns": []string{"feed.status"},
			})
			if err := conn.WriteFrame(subMsg); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			// Collect unique feed statuses.
			feeds := make(map[string]FeedStatus)
			deadline := time.After(5 * time.Second)

			for {
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
				if json.Unmarshal(frame, &msg) != nil || msg.Type != "update" || msg.Topic != "feed.status" {
					continue
				}

				var p struct {
					Name       string  `json:"Name"`
					State      string  `json:"State"`
					LastUpdate string  `json:"LastUpdate"`
					LatencyMs  float64 `json:"LatencyMs"`
					ErrorCount uint64  `json:"ErrorCount"`
				}
				if json.Unmarshal(msg.Payload, &p) != nil {
					continue
				}

				ts := p.LastUpdate
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					ts = t.Format("2006-01-02 15:04:05")
				}

				feeds[p.Name] = FeedStatus{
					Name:       p.Name,
					State:      p.State,
					LatencyMs:  p.LatencyMs,
					ErrorCount: p.ErrorCount,
					LastUpdate: ts,
				}
			}

		output:
			if len(feeds) == 0 {
				fmt.Fprintln(os.Stderr, "No feed status received within 5s.")
				return nil
			}

			var feedList []FeedStatus
			for _, f := range feeds {
				feedList = append(feedList, f)
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(feedList)
			case "text":
				fmt.Printf("%-20s %-12s %10s %8s  %s\n", "NAME", "STATE", "LATENCY", "ERRORS", "LAST UPDATE")
				fmt.Println(strings.Repeat("-", 75))
				for _, f := range feedList {
					state := f.State
					latency := fmt.Sprintf("%.1fms", f.LatencyMs)
					errors := fmt.Sprintf("%d", f.ErrorCount)
					fmt.Printf("%-20s %-12s %10s %8s  %s\n", f.Name, state, latency, errors, f.LastUpdate)
				}
				return nil
			default:
				return fmt.Errorf("unknown format: %s", format)
			}
		},
	}

	cmd.Flags().StringVarP(&socket, "socket", "s", "", "server socket path")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text, json")

	return cmd
}
