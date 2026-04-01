package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewAgentCmd creates the agent command group.
func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent skills",
	}

	cmd.AddCommand(newAgentListCmd())
	return cmd
}

func newAgentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available agent skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			skills := []struct {
				Name string
				Desc string
			}{
				{"news-scan", "Scan news for watchlist relevance"},
				{"anomaly-detect", "Monitor price/volume anomalies"},
				{"summarize", "Generate market summary for ticker"},
				{"correlation-watch", "Monitor cross-asset correlation breaks"},
			}

			fmt.Printf("%-20s %s\n", "SKILL", "DESCRIPTION")
			fmt.Println("---")
			for _, s := range skills {
				fmt.Printf("%-20s %s\n", s.Name, s.Desc)
			}
			fmt.Println()
			fmt.Println("Run a skill: set NOTBBG_SOCKET and NOTBBG_SKILL env vars, then execute your agent binary.")
			return nil
		},
	}
}
