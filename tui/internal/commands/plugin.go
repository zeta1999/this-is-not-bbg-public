package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"
)

// NewPluginCmd creates the plugin management command.
func NewPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage plugins",
	}

	cmd.AddCommand(pluginListCmd())
	cmd.AddCommand(pluginInitCmd())
	cmd.AddCommand(pluginInstallCmd())
	cmd.AddCommand(pluginRemoveCmd())
	cmd.AddCommand(pluginLogsCmd())

	return cmd
}

// pluginLogsCmd prints (or follows) the plugin's stderr log. The
// server tees plugin stderr to <plugin-dir>/notbbg.log with a simple
// 10 MB size-based rotation; this command is a lightweight tail(1).
func pluginLogsCmd() *cobra.Command {
	var follow bool
	var tailN int
	c := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show the plugin's stderr log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := filepath.Join(tuiconfig.Plugins(), name, "notbbg.log")
			f, err := os.Open(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Printf("No log file for plugin %q yet (%s).\n", name, path)
					return nil
				}
				return err
			}
			defer f.Close()

			// Seek to last tailN lines. Cheap approximation: read the
			// last 64 KiB and keep the tail lines. Works fine for
			// typical plugin logs; precise line counting is not worth
			// the complexity here.
			if tailN > 0 {
				fi, _ := f.Stat()
				start := int64(0)
				const window = 64 * 1024
				if fi != nil && fi.Size() > window {
					start = fi.Size() - window
				}
				if _, err := f.Seek(start, io.SeekStart); err != nil {
					return err
				}
				buf, err := io.ReadAll(f)
				if err != nil {
					return err
				}
				lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
				if len(lines) > tailN {
					lines = lines[len(lines)-tailN:]
				}
				fmt.Println(strings.Join(lines, "\n"))
			} else {
				if _, err := io.Copy(cmd.OutOrStdout(), f); err != nil {
					return err
				}
			}

			if !follow {
				return nil
			}

			// Follow mode: poll for new bytes, write them out. Simple
			// 250 ms poll is fine — this is a CLI tool, not a hot path.
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				default:
				}
				n, err := io.Copy(cmd.OutOrStdout(), f)
				if err != nil {
					return err
				}
				if n == 0 {
					time.Sleep(250 * time.Millisecond)
				}
			}
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "follow appended output (like tail -f)")
	c.Flags().IntVarP(&tailN, "tail", "n", 0, "show last N lines only (approximation over last 64 KiB)")
	return c
}

// pluginInstallCmd copies a local plugin source directory into the
// plugin home. The server's Reconcile loop picks it up within 5 s —
// no restart required.
func pluginInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <source-path> [name]",
		Short: "Install a plugin from a local directory",
		Long: `Copy a plugin source directory into ~/.config/notbbg/plugins/.

If [name] is omitted, the target dir uses the source dir's basename.
The server's plugin hot-reload will detect the new plugin within 5s.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			name := filepath.Base(filepath.Clean(src))
			if len(args) == 2 {
				name = args[1]
			}
			srcInfo, err := os.Stat(src)
			if err != nil {
				return fmt.Errorf("source %q: %w", src, err)
			}
			if !srcInfo.IsDir() {
				return fmt.Errorf("source %q is not a directory", src)
			}
			if _, err := os.Stat(filepath.Join(src, "manifest.yaml")); err != nil {
				return fmt.Errorf("source %q has no manifest.yaml: %w", src, err)
			}
			dest := filepath.Join(tuiconfig.Plugins(), name)
			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("plugin %q already installed at %s — remove it first", name, dest)
			}
			if err := copyDir(src, dest); err != nil {
				return fmt.Errorf("copy: %w", err)
			}
			fmt.Printf("Installed plugin %q to %s\n", name, dest)
			return nil
		},
	}
}

// pluginRemoveCmd deletes a plugin's directory. The server Reconcile
// loop sees the deletion on its next tick and stops the process.
func pluginRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			dir := filepath.Join(tuiconfig.Plugins(), name)
			if _, err := os.Stat(dir); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("plugin %q is not installed", name)
				}
				return err
			}
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			fmt.Printf("Removed plugin %q (%s)\n", name, dir)
			return nil
		},
	}
}

// copyDir performs a simple recursive copy of src into dst. Enough
// for local plugin installs (no symlinks, no perm-preservation
// beyond owner bits). Overwrites existing dest files.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func pluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := tuiconfig.Plugins()

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
					Screens []struct {
						ID    string `yaml:"id"`
						Label string `yaml:"label"`
					} `yaml:"screens"`
				}
				_ = yaml.Unmarshal(data, &m)
				name := m.Name
				if name == "" {
					name = e.Name()
				}
				screenIDs := ""
				for _, s := range m.Screens {
					if screenIDs != "" {
						screenIDs += ","
					}
					screenIDs += s.ID
				}
				if screenIDs == "" {
					screenIDs = "(none)"
				}
				fmt.Printf("  %-20s  cmd=%s  screens=%s  in=%v  out=%v\n", name, m.Command, screenIDs, m.Input, m.Output)
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
			dir := filepath.Join(tuiconfig.Plugins(), name)

			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}

			manifest := map[string]any{
				"name":          name,
				"command":       fmt.Sprintf("./%s", name),
				"args":          []string{},
				"input_topics":  []string{"ohlc.binance.*"},
				"output_topics": []string{fmt.Sprintf("plugin.%s.screen", name)},
				"screens": []map[string]string{
					{
						"id":    strings.ToUpper(name),
						"label": name,
						"icon":  "flash-outline",
					},
				},
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
