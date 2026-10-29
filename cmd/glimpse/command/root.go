// Package command defines Glimpse's Cobra command tree and Viper configuration.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"glimpse/internal/app"
	"glimpse/internal/pager"
	"glimpse/internal/ui"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// NewRoot creates a root command with an isolated Viper instance, which keeps
// command construction predictable in tests and future embedded use.
func NewRoot() *cobra.Command {
	return newRoot(func(ctx context.Context, application *app.App, input io.ReadCloser, name string) error {
		return application.Run(ctx, input, name)
	}, requireInteractiveTerminal)
}

type runner func(context.Context, *app.App, io.ReadCloser, string) error

func newRoot(run runner, checkTerminal func(io.Writer) error) *cobra.Command {
	config := viper.New()
	var configFile string

	root := &cobra.Command{
		Use:           "glimpse [FILE]",
		Short:         "A terminal pager",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell, _ := cmd.Flags().GetString("completion"); shell != "" {
				return writeCompletion(cmd.Root(), cmd.OutOrStdout(), shell)
			}
			if check, _ := cmd.Flags().GetBool("check-config"); check {
				return checkConfig(config, cmd)
			}
			if checkTerminal != nil {
				if err := checkTerminal(cmd.OutOrStdout()); errors.Is(err, errStdoutNotTerminal) {
					return passThrough(cmd, args)
				} else if err != nil {
					return err
				}
			}
			options, err := readOptions(config, cmd)
			if err != nil {
				return err
			}
			if options.Label == "" && (len(args) == 0 || args[0] == "-") {
				// man(1) names the page it pipes, e.g. "ls(1)".
				options.Label = os.Getenv("MAN_PN")
			}
			application, err := app.New(options)
			if err != nil {
				return err
			}
			input, name, err := openInput(cmd.InOrStdin(), args)
			if err != nil {
				return err
			}
			return run(cmd.Context(), application, input, name)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Cobra's hidden completion command has none of the root's flags
			// and needs no configuration.
			if cmd != cmd.Root() {
				return nil
			}
			config.SetEnvPrefix("GLIMPSE")
			config.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
			config.AutomaticEnv()
			if err := config.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			for _, name := range []string{"smart-case", "ignore-case", "case-sensitive"} {
				if err := config.BindPFlag("search."+name, cmd.Flags().Lookup(name)); err != nil {
					return err
				}
			}
			for flag, key := range map[string]string{"incremental-search": "search.incremental", "highlight-search": "search.highlight"} {
				if err := config.BindPFlag(key, cmd.Flags().Lookup(flag)); err != nil {
					return err
				}
			}
			for _, name := range []string{"memory-budget", "spool-quota", "metadata-budget", "metadata-quota", "retention", "spool-dir"} {
				if err := config.BindPFlag("storage."+name, cmd.Flags().Lookup(name)); err != nil {
					return err
				}
			}
			for _, name := range []string{"ambiguous-width", "wrap", "line-numbers", "hex-colors"} {
				if err := config.BindPFlag("display."+name, cmd.Flags().Lookup(name)); err != nil {
					return err
				}
			}

			if configFile != "" {
				config.SetConfigFile(configFile)
			} else {
				config.SetConfigName("glimpse")
				config.SetConfigType("toml")
				if dir, err := os.UserConfigDir(); err == nil {
					config.AddConfigPath(dir + string(os.PathSeparator) + "glimpse")
				}
			}

			if err := config.ReadInConfig(); err != nil {
				var notFound viper.ConfigFileNotFoundError
				if !errors.As(err, &notFound) {
					return fmt.Errorf("read config: %w", err)
				}
			}
			return nil
		},
	}

	root.SetVersionTemplate("glimpse {{.Version}}\n")
	root.PersistentFlags().StringVar(&configFile, "config", "", "TOML config file (default: $XDG_CONFIG_HOME/glimpse/glimpse.toml)")
	root.Flags().Bool("check-config", false, "validate the configuration (including unknown keys) and exit")
	root.Flags().String("completion", "", "print a completion script for bash, zsh, fish, or powershell and exit")
	root.Flags().String("label", "", "name shown for the input in the statusline (stdin defaults to $MAN_PN)")
	root.Flags().Int("chunk-size", 64*1024, "input storage chunk size in bytes")
	root.Flags().Bool("follow", false, "follow the end of incoming input (toggle with F)")
	root.Flags().Bool("follow-name", false, "watch a file by name, reopening it after rotation or replacement, instead of by descriptor")
	root.Flags().Bool("smart-case", false, "ignore search case unless the query contains an uppercase letter")
	root.Flags().Bool("ignore-case", false, "ignore search case, including uppercase queries")
	root.Flags().Bool("case-sensitive", false, "match search case exactly")
	root.Flags().Bool("incremental-search", false, "preview matches while editing a search")
	root.Flags().Bool("highlight-search", true, "highlight visible search matches")
	storage := pager.DefaultStorageOptions()
	root.Flags().Int64("memory-budget", storage.MemoryBudget, "resident input byte budget")
	root.Flags().Int64("spool-quota", storage.SpoolQuota, "lossless stream quota in bytes; pause when full")
	root.Flags().Int64("metadata-budget", 8<<20, "resident row/line index byte budget")
	root.Flags().Int64("metadata-quota", 256<<20, "maximum row/line index bytes")
	root.Flags().String("retention", "lossless", "stream retention: lossless or rolling (explicit history loss)")
	root.Flags().String("spool-dir", "", "directory for private temporary input spools")
	root.Flags().Int("ambiguous-width", 1, "East Asian ambiguous character width: 1 or 2 cells")
	root.Flags().BoolP("line-numbers", "N", false, "show a line number gutter (toggle with #)")
	root.Flags().Bool("hex-colors", false, "color #RRGGBB codes with their own color in truecolor terminals (toggle with R)")
	root.Flags().Bool("wrap", true, "wrap long lines (toggle with w); otherwise scroll horizontally with h/l")
	root.Flags().StringP("jump-target", "j", "1", "screen line for search matches and line jumps: 1 is the top, -1 the bottom, .5 the middle")
	root.Flags().Int("cmdheight", int(ui.DefaultCmdHeight), "footer layout: -2 like -1 with the statusline initially hidden, -1 toggleable status/prompt row, 0 status with on-demand search row, 1 permanent status and search rows")
	return root
}

func readOptions(config *viper.Viper, cmd *cobra.Command) (app.Options, error) {
	height, err := strconv.Atoi(config.GetString("cmdheight"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse cmdheight: %w", err)
	}
	chunkSize, err := strconv.Atoi(config.GetString("chunk-size"))
	if err != nil || chunkSize <= 0 {
		return app.Options{}, fmt.Errorf("chunk-size must be a positive integer")
	}
	follow, err := strconv.ParseBool(config.GetString("follow"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse follow: %w", err)
	}
	followName, err := strconv.ParseBool(config.GetString("follow-name"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse follow-name: %w", err)
	}
	options := app.Options{CmdHeight: ui.CmdHeight(height), ChunkSize: chunkSize, Follow: follow, FollowName: followName, Label: config.GetString("label")}
	if options.JumpTarget, err = app.ParseJumpTarget(config.GetString("jump-target")); err != nil {
		return app.Options{}, err
	}
	options.AmbiguousWidth, err = strconv.Atoi(config.GetString("display.ambiguous-width"))
	if err != nil || options.AmbiguousWidth != 1 && options.AmbiguousWidth != 2 {
		return app.Options{}, fmt.Errorf("ambiguous-width must be 1 or 2")
	}
	for name, destination := range map[string]*int64{
		"memory-budget": &options.Storage.MemoryBudget, "spool-quota": &options.Storage.SpoolQuota,
		"metadata-budget": &options.MetadataBudget, "metadata-quota": &options.MetadataQuota,
	} {
		value, err := strconv.ParseInt(config.GetString("storage."+name), 10, 64)
		if err != nil || value <= 0 {
			return app.Options{}, fmt.Errorf("storage.%s must be a positive integer byte count", name)
		}
		*destination = value
	}
	options.Storage.Retention = config.GetString("storage.retention")
	options.Storage.SpoolDir = config.GetString("storage.spool-dir")
	options.IncrementalSearch, err = strconv.ParseBool(config.GetString("search.incremental"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse search.incremental: %w", err)
	}
	highlight, err := strconv.ParseBool(config.GetString("search.highlight"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse search.highlight: %w", err)
	}
	options.NoSearchHighlight = !highlight
	wrap, err := strconv.ParseBool(config.GetString("display.wrap"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse display.wrap: %w", err)
	}
	options.NoWrap = !wrap
	options.LineNumbers, err = strconv.ParseBool(config.GetString("display.line-numbers"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse display.line-numbers: %w", err)
	}
	options.HexColors, err = strconv.ParseBool(config.GetString("display.hex-colors"))
	if err != nil {
		return app.Options{}, fmt.Errorf("parse display.hex-colors: %w", err)
	}
	if err := readAppearance(config, &options); err != nil {
		return app.Options{}, err
	}
	// Treat the case settings as one mode: an enabled flag outranks environment
	// modes, which outrank TOML modes. At the same tier, explicit sensitivity wins
	// over smart case, which wins over unconditional case folding.
	mode, tier := "", 0
	for _, name := range []string{"ignore-case", "smart-case", "case-sensitive"} {
		enabled, err := strconv.ParseBool(config.GetString("search." + name))
		if err != nil {
			return app.Options{}, fmt.Errorf("parse search.%s: %w", name, err)
		}
		if !enabled {
			continue
		}
		priority := 1
		if env := os.Getenv("GLIMPSE_SEARCH_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))); env != "" {
			priority = 2
		}
		if cmd.Flags().Changed(name) {
			priority = 3
		}
		if priority >= tier {
			mode, tier = name, priority
		}
	}
	options.SmartCase = mode == "smart-case"
	options.IgnoreCase = mode == "ignore-case"
	options.CaseSensitive = mode == "case-sensitive"
	if raw := config.Get("keybinds"); raw != nil {
		table, ok := raw.(map[string]any)
		if !ok {
			return app.Options{}, fmt.Errorf("keybinds must be a table of key arrays")
		}
		bindings := make(map[string][]string, len(table))
		for action, rawKeys := range table {
			keys, ok := rawKeys.([]any)
			if !ok {
				return app.Options{}, fmt.Errorf("keybind %s must be an array of keys", action)
			}
			sequence := make([]string, 0, len(keys))
			for _, rawKey := range keys {
				key, ok := rawKey.(string)
				if !ok {
					return app.Options{}, fmt.Errorf("keybind %s contains a non-string key", action)
				}
				sequence = append(sequence, key)
			}
			bindings[action] = sequence
		}
		options.Keybinds = bindings
	}
	if options.Run, err = readRun(config.Get("run")); err != nil {
		return app.Options{}, err
	}
	return options, nil
}

// readRun reads [[run]] tables: keys (one sequence), command, and optional
// background.
func readRun(raw any) ([]app.RunCommand, error) {
	if raw == nil {
		return nil, nil
	}
	var tables []map[string]any
	switch raw := raw.(type) {
	case []map[string]any:
		tables = raw
	case []any:
		for _, item := range raw {
			table, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("run must be an array of tables ([[run]])")
			}
			tables = append(tables, table)
		}
	default:
		return nil, fmt.Errorf("run must be an array of tables ([[run]])")
	}
	runs := make([]app.RunCommand, 0, len(tables))
	for i, table := range tables {
		name := fmt.Sprintf("run-%d", i+1)
		var run app.RunCommand
		for key, value := range table {
			switch key {
			case "keys":
				keys, ok := value.([]any)
				if !ok {
					return nil, fmt.Errorf("%s: keys must be an array of keys", name)
				}
				for _, rawKey := range keys {
					key, ok := rawKey.(string)
					if !ok {
						return nil, fmt.Errorf("%s: keys contains a non-string key", name)
					}
					run.Keys = append(run.Keys, key)
				}
			case "command":
				command, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("%s: command must be a string", name)
				}
				run.Command = command
			case "background":
				background, ok := value.(bool)
				if !ok {
					return nil, fmt.Errorf("%s: background must be true or false", name)
				}
				run.Background = background
			default:
				return nil, fmt.Errorf("%s: unknown option %q", name, key)
			}
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func openInput(stdin io.Reader, args []string) (io.ReadCloser, string, error) {
	if len(args) == 1 && args[0] != "-" {
		file, err := os.Open(args[0])
		if err != nil {
			return nil, "", err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, "", err
		}
		if info.IsDir() {
			file.Close()
			return nil, "", fmt.Errorf("%s is a directory", args[0])
		}
		return file, args[0], nil
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return nil, "", fmt.Errorf("provide a file or pipe input into glimpse")
	}
	// Stdin has no name; the statusline omits its filename section.
	if input, ok := stdin.(io.ReadCloser); ok {
		return input, "", nil
	}
	return io.NopCloser(stdin), "", nil
}
