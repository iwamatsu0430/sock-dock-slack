package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/service"
)

func client(g *globals) *control.Client { return control.NewClient(g.dirs.SocketPath()) }

func initCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a starter config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := g.dirs.ConfigFile
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("%s already exists", p)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(assets.ConfigExample), 0o600); err != nil {
				return err
			}
			fmt.Println("created", p)
			fmt.Println("next: edit it, then run `sockdock check`")
			return nil
		},
	}
}

func checkCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate the config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(g.dirs.ConfigFile)
			if err != nil {
				return err
			}
			fmt.Printf("%s: OK (%d routes)\n", cfg.Path, len(cfg.Routes))
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ROUTE\tTYPE\tCHANNEL\tMATCH")
			for _, r := range cfg.Routes {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Handler.Type, r.Channel, r.Match)
			}
			w.Flush()
			if _, _, err := secret.Tokens(cfg.Settings.SecretsFile); err != nil {
				fmt.Println("warning:", err)
			}
			return nil
		},
	}
}

func matchCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "match <channel-id> <text>",
		Short: "Show which route a top-level message would start",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(g.dirs.ConfigFile)
			if err != nil {
				return err
			}
			res := control.MatchConfig(cfg, control.MatchRequest{Channel: args[0], Text: strings.Join(args[1:], " ")})
			if !res.Matched {
				return &exitError{code: 1, msg: "no route matches"}
			}
			fmt.Println("route:", res.Route)
			keys := make([]string, 0, len(res.Groups))
			for k := range res.Groups {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("  %s = %q\n", k, res.Groups[k])
			}
			return nil
		},
	}
}

func statusCmd(g *globals) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show gateway status (exit code 1 if not running or not connected)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, _ := service.Status()
			s, err := client(g).Status(cmd.Context())
			if err != nil {
				return &exitError{code: 1, msg: fmt.Sprintf("%v\nservice: %s", err, svc)}
			}
			if asJSON {
				return printJSON(s)
			}
			conn := "connected"
			if !s.Connected {
				conn = "DISCONNECTED"
				if s.LastError != "" {
					conn += " (" + s.LastError + ")"
				}
			}
			fmt.Printf("sockdock %s (pid %d), up %s\n", s.Version, s.PID, time.Since(s.StartedAt).Round(time.Second))
			fmt.Printf("slack:   %s as %s on %s\n", conn, s.Identity.UserID, s.Identity.Team)
			fmt.Printf("service: %s\n", svc)
			fmt.Printf("config:  %s\n", s.ConfigPath)
			if s.LastEventAt != nil {
				fmt.Printf("last event: %s ago\n", time.Since(*s.LastEventAt).Round(time.Second))
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "\nROUTE\tTYPE\tCHANNEL\tSTATE")
			for _, r := range s.Routes {
				state := "active"
				if r.Paused {
					state = "paused"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Type, r.Channel, state)
			}
			w.Flush()
			if len(s.Running) > 0 {
				fmt.Println("\nrunning:")
				for _, rt := range s.Running {
					fmt.Printf("  %s  %s  %s  (%s)\n", rt.Route, rt.Channel, rt.ThreadTS, time.Since(rt.StartedAt).Round(time.Second))
				}
			}
			if s.Pending > 0 {
				fmt.Printf("queued: %d\n", s.Pending)
			}
			if !s.Connected {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func reloadCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Ask the running gateway to re-read its config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := client(g).Reload(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("reloaded")
			return nil
		},
	}
}

func threadsCmd(g *globals) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "threads",
		Short: "List recent threads",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ts, err := client(g).Threads(cmd.Context(), limit)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "LAST ACTIVE\tROUTE\tTURNS\tCHANNEL\tTHREAD\tLINK")
			for _, t := range ts {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", t.LastActiveAt.Format("01-02 15:04"), t.Route, t.Turns, t.Channel, t.ThreadTS, t.Permalink)
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "number of threads")
	return cmd
}

func turnsCmd(g *globals) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "turns",
		Short: "List recent handler runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ts, err := client(g).Turns(cmd.Context(), limit)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTARTED\tDURATION\tROUTE\tSTATUS\tERROR")
			for _, t := range ts {
				dur := "-"
				if t.FinishedAt != nil {
					dur = t.FinishedAt.Sub(t.StartedAt).String()
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.StartedAt.Format("01-02 15:04:05"), dur, t.Route, t.Status, truncate(t.Error, 60))
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "number of turns")
	return cmd
}

func pauseCmd(g *globals, paused bool) *cobra.Command {
	use, short := "resume <route>", "Resume a paused route"
	if paused {
		use, short = "pause <route>", "Pause a route (new threads are ignored, replies get a notice)"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return client(g).SetPaused(cmd.Context(), args[0], paused)
		},
	}
}

func logsCmd(g *globals) *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the last lines of the log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			lines, err := client(g).Logs(cmd.Context(), n)
			if errors.Is(err, control.ErrNotRunning) {
				// Fall back to reading the file directly.
				b, ferr := os.ReadFile(g.dirs.LogFile())
				if ferr != nil {
					return err
				}
				all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
				lines = all[max(len(all)-n, 0):]
			} else if err != nil {
				return err
			}
			for _, l := range lines {
				fmt.Println(l)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "lines", "n", 50, "number of lines")
	return cmd
}

func authCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage Slack tokens"}
	set := &cobra.Command{
		Use:   "set [app|bot]",
		Short: "Store tokens in the macOS Keychain (prompts for the value)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			names := []string{secret.AppToken, secret.BotToken}
			if len(args) == 1 {
				switch args[0] {
				case "app":
					names = names[:1]
				case "bot":
					names = names[1:]
				default:
					return fmt.Errorf("unknown token %q (app or bot)", args[0])
				}
			}
			for _, n := range names {
				hint := "xapp-…, from Basic Information > App-Level Tokens"
				if n == secret.BotToken {
					hint = "xoxb-…, from OAuth & Permissions"
				}
				fmt.Printf("%s (%s)\n", n, hint)
				if err := secret.KeychainSetInteractive(n); err != nil {
					return err
				}
			}
			fmt.Println("saved to the Keychain (service: sockdock)")
			return nil
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show where each token is read from",
		RunE: func(cmd *cobra.Command, _ []string) error {
			secretsFile := ""
			if cfg, err := config.Load(g.dirs.ConfigFile); err == nil {
				secretsFile = cfg.Settings.SecretsFile
			}
			for _, n := range []string{secret.AppToken, secret.BotToken} {
				v, src, err := secret.Lookup(n, secretsFile)
				switch {
				case err != nil:
					fmt.Printf("%s: error: %v\n", n, err)
				case v == "":
					fmt.Printf("%s: not set\n", n)
				default:
					fmt.Printf("%s: %s…%s (from %s)\n", n, v[:min(5, len(v))], v[max(len(v)-4, 0):], src)
				}
			}
			return nil
		},
	}
	del := &cobra.Command{
		Use:   "delete",
		Short: "Remove the tokens from the Keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, n := range []string{secret.AppToken, secret.BotToken} {
				_ = secret.KeychainDelete(n)
			}
			fmt.Println("removed")
			return nil
		},
	}
	cmd.AddCommand(set, status, del)
	return cmd
}

func serviceCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Run sockdock in the background at login (launchd / systemd)"}
	var binary string
	install := &cobra.Command{
		Use:   "install",
		Short: "Install and start the background service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := config.Load(g.dirs.ConfigFile); err != nil {
				return fmt.Errorf("fix the config first: %w", err)
			}
			if binary == "" {
				var err error
				if binary, err = service.DefaultBinary(); err != nil {
					return err
				}
			}
			cfgPath, err := filepath.Abs(g.dirs.ConfigFile)
			if err != nil {
				return err
			}
			p, err := service.Install(service.Spec{
				Binary: binary, ConfigFile: cfgPath, LogDir: g.dirs.LogDir, Path: os.Getenv("PATH"),
			})
			if err != nil {
				return err
			}
			fmt.Println("installed", p)
			fmt.Println("the service uses your current PATH; re-run `sockdock service install` if handlers need a different one")
			return nil
		},
	}
	install.Flags().StringVar(&binary, "binary", "", "path to the sockdock binary the service runs")
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the background service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := service.Uninstall(); err != nil {
				return err
			}
			fmt.Println("uninstalled")
			return nil
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show whether the service is installed and loaded",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, ok := service.Status()
			fmt.Println(s)
			if !ok {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	cmd.AddCommand(install, uninstall, status)
	return cmd
}

func manifestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "manifest",
		Short: "Print the Slack app manifest",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Print(assets.SlackAppManifest)
		},
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
