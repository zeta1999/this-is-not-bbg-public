package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewPluginCmd creates the plugin management command.
func NewPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage plugins",
	}

	cmd.AddCommand(pluginListCmd())
	cmd.AddCommand(pluginInitCmd())

	return cmd
}

func pluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".config", "notbbg", "plugins")

			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No plugins installed.")
					fmt.Printf("Plugin directory: %s\n", dir)
					return nil
				}
				return err
			}

			if len(entries) == 0 {
				fmt.Println("No plugins installed.")
				return nil
			}

			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				manifest := filepath.Join(dir, e.Name(), "manifest.yaml")
				if _, err := os.Stat(manifest); err != nil {
					continue
				}
				data, _ := os.ReadFile(manifest)
				var m struct {
					Name    string   `yaml:"name"`
					Command string   `yaml:"command"`
					Input   []string `yaml:"input_topics"`
					Output  []string `yaml:"output_topics"`
				}
				yaml.Unmarshal(data, &m)
				name := m.Name
				if name == "" {
					name = e.Name()
				}
				fmt.Printf("  %-20s  cmd=%s  in=%v  out=%v\n", name, m.Command, m.Input, m.Output)
			}
			return nil
		},
	}
}

func pluginInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Create a new plugin scaffold",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".config", "notbbg", "plugins", name)

			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}

			manifest := map[string]any{
				"name":          name,
				"command":       fmt.Sprintf("./%s", name),
				"args":          []string{},
				"input_topics":  []string{"news"},
				"output_topics": []string{fmt.Sprintf("news.%s", name)},
			}

			data, _ := yaml.Marshal(manifest)
			path := filepath.Join(dir, "manifest.yaml")
			if err := os.WriteFile(path, data, 0644); err != nil {
				return err
			}

			fmt.Printf("Plugin scaffold created: %s\n", dir)
			fmt.Printf("Edit %s to configure.\n", path)
			return nil
		},
	}
}
